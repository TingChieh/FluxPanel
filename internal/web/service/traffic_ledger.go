package service

import (
	"maps"
	"sync"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	trafficHourlyRetentionDays = 90
	trafficDailyRetentionDays  = 365
	trafficLedgerBatchSize     = 500
)

// TrafficLedgerKey identifies one (UTC hour, node, client email) bucket in memory.
type TrafficLedgerKey struct {
	HourBucket int64
	NodeId     int
	Email      string
}

// TrafficDelta holds accumulated upload and download byte increments.
type TrafficDelta struct {
	Up   int64
	Down int64
}

var (
	trafficLedgerMu      sync.Mutex
	trafficLedgerPending = make(map[TrafficLedgerKey]TrafficDelta)
)

// RecordTrafficDelta accumulates positive traffic increments in memory for periodic flushing.
func RecordTrafficDelta(at time.Time, nodeID int, email string, up, down int64) {
	if up < 0 {
		up = 0
	}
	if down < 0 {
		down = 0
	}
	if up == 0 && down == 0 {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	key := TrafficLedgerKey{
		HourBucket: at.UTC().Truncate(time.Hour).Unix(),
		NodeId:     nodeID,
		Email:      email,
	}
	trafficLedgerMu.Lock()
	cur := trafficLedgerPending[key]
	cur.Up += up
	cur.Down += down
	trafficLedgerPending[key] = cur
	trafficLedgerMu.Unlock()
}

// SnapshotPendingTraffic returns a copy of all unflushed hourly increments.
func SnapshotPendingTraffic() map[TrafficLedgerKey]TrafficDelta {
	trafficLedgerMu.Lock()
	defer trafficLedgerMu.Unlock()
	if len(trafficLedgerPending) == 0 {
		return map[TrafficLedgerKey]TrafficDelta{}
	}
	out := make(map[TrafficLedgerKey]TrafficDelta, len(trafficLedgerPending))
	maps.Copy(out, trafficLedgerPending)
	return out
}

// ResetTrafficLedgerForTest clears the in-memory traffic ledger between tests.
func ResetTrafficLedgerForTest() {
	trafficLedgerMu.Lock()
	trafficLedgerPending = make(map[TrafficLedgerKey]TrafficDelta)
	trafficLedgerMu.Unlock()
}

func requeueTrafficDeltas(batch map[TrafficLedgerKey]TrafficDelta) {
	if len(batch) == 0 {
		return
	}
	trafficLedgerMu.Lock()
	for k, d := range batch {
		cur := trafficLedgerPending[k]
		cur.Up += d.Up
		cur.Down += d.Down
		trafficLedgerPending[k] = cur
	}
	trafficLedgerMu.Unlock()
}

// FlushTrafficLedger writes all buffered hourly deltas to SQLite in a single serialized transaction.
func FlushTrafficLedger() error {
	trafficLedgerMu.Lock()
	if len(trafficLedgerPending) == 0 {
		trafficLedgerMu.Unlock()
		return nil
	}
	batch := trafficLedgerPending
	trafficLedgerPending = make(map[TrafficLedgerKey]TrafficDelta)
	trafficLedgerMu.Unlock()

	rows := make([]model.TrafficHourly, 0, len(batch))
	for k, d := range batch {
		if d.Up == 0 && d.Down == 0 {
			continue
		}
		rows = append(rows, model.TrafficHourly{
			HourBucket: k.HourBucket,
			NodeId:     k.NodeId,
			Email:      k.Email,
			Up:         d.Up,
			Down:       d.Down,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	err := submitTrafficWriteBackground(func() error {
		db := database.GetDB()
		if db == nil {
			return nil
		}
		return db.Transaction(func(tx *gorm.DB) error {
			for i := 0; i < len(rows); i += trafficLedgerBatchSize {
				end := min(i+trafficLedgerBatchSize, len(rows))
				if err := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{
						{Name: "hour_bucket"},
						{Name: "node_id"},
						{Name: "email"},
					},
					DoUpdates: clause.Assignments(map[string]any{
						"up":   gorm.Expr("traffic_hourlies.up + excluded.up"),
						"down": gorm.Expr("traffic_hourlies.down + excluded.down"),
					}),
				}).Create(rows[i:end]).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		requeueTrafficDeltas(batch)
		return err
	}
	return nil
}

// RenameTrafficHistoryTx merges oldEmail into newEmail across the in-memory ledger and history tables.
func RenameTrafficHistoryTx(tx *gorm.DB, oldEmail, newEmail string) error {
	if oldEmail == "" || newEmail == "" || oldEmail == newEmail {
		return nil
	}

	trafficLedgerMu.Lock()
	for k, d := range trafficLedgerPending {
		if k.Email != oldEmail {
			continue
		}
		dst := TrafficLedgerKey{HourBucket: k.HourBucket, NodeId: k.NodeId, Email: newEmail}
		cur := trafficLedgerPending[dst]
		cur.Up += d.Up
		cur.Down += d.Down
		trafficLedgerPending[dst] = cur
		delete(trafficLedgerPending, k)
	}
	trafficLedgerMu.Unlock()

	if err := tx.Exec(
		`INSERT INTO traffic_hourlies (hour_bucket, node_id, email, up, down)
		 SELECT hour_bucket, node_id, ?, up, down FROM traffic_hourlies WHERE email = ?
		 ON CONFLICT(hour_bucket, node_id, email) DO UPDATE SET
		   up = traffic_hourlies.up + excluded.up,
		   down = traffic_hourlies.down + excluded.down`,
		newEmail, oldEmail,
	).Error; err != nil {
		return err
	}
	if err := tx.Where("email = ?", oldEmail).Delete(&model.TrafficHourly{}).Error; err != nil {
		return err
	}

	if err := tx.Exec(
		`INSERT INTO traffic_dailies (date, node_id, email, up, down)
		 SELECT date, node_id, ?, up, down FROM traffic_dailies WHERE email = ?
		 ON CONFLICT(date, node_id, email) DO UPDATE SET
		   up = traffic_dailies.up + excluded.up,
		   down = traffic_dailies.down + excluded.down`,
		newEmail, oldEmail,
	).Error; err != nil {
		return err
	}
	return tx.Where("email = ?", oldEmail).Delete(&model.TrafficDaily{}).Error
}

// TrafficHourlyCutoffDate returns the earliest YYYY-MM-DD (and its local midnight unix) kept in traffic_hourlies.
func TrafficHourlyCutoffDate(now time.Time, loc *time.Location) (string, int64) {
	if loc == nil {
		loc = time.Local
	}
	localNow := now.In(loc)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	cutoff := todayStart.AddDate(0, 0, -trafficHourlyRetentionDays)
	return cutoff.Format("2006-01-02"), cutoff.Unix()
}

// RollupAndPruneTrafficHistory rolls hourly rows older than 90 days into traffic_dailies and prunes >365d dailies.
func RollupAndPruneTrafficHistory(now time.Time, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	_, cutoffUnix := TrafficHourlyCutoffDate(now, loc)
	localNow := now.In(loc)
	pruneDailyDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).
		AddDate(0, 0, -trafficDailyRetentionDays).
		Format("2006-01-02")

	return submitTrafficWriteBackground(func() error {
		db := database.GetDB()
		if db == nil {
			return nil
		}
		return db.Transaction(func(tx *gorm.DB) error {
			var oldHours []model.TrafficHourly
			if err := tx.Where("hour_bucket < ?", cutoffUnix).Find(&oldHours).Error; err != nil {
				return err
			}
			if len(oldHours) > 0 {
				type dailyKey struct {
					date   string
					nodeID int
					email  string
				}
				agg := make(map[dailyKey]TrafficDelta, len(oldHours))
				for _, h := range oldHours {
					dk := dailyKey{
						date:   time.Unix(h.HourBucket, 0).In(loc).Format("2006-01-02"),
						nodeID: h.NodeId,
						email:  h.Email,
					}
					cur := agg[dk]
					cur.Up += h.Up
					cur.Down += h.Down
					agg[dk] = cur
				}
				dailies := make([]model.TrafficDaily, 0, len(agg))
				for k, d := range agg {
					dailies = append(dailies, model.TrafficDaily{
						Date:   k.date,
						NodeId: k.nodeID,
						Email:  k.email,
						Up:     d.Up,
						Down:   d.Down,
					})
				}
				for i := 0; i < len(dailies); i += trafficLedgerBatchSize {
					end := min(i+trafficLedgerBatchSize, len(dailies))
					if err := tx.Clauses(clause.OnConflict{
						Columns: []clause.Column{
							{Name: "date"},
							{Name: "node_id"},
							{Name: "email"},
						},
						DoUpdates: clause.Assignments(map[string]any{
							"up":   gorm.Expr("traffic_dailies.up + excluded.up"),
							"down": gorm.Expr("traffic_dailies.down + excluded.down"),
						}),
					}).Create(dailies[i:end]).Error; err != nil {
						return err
					}
				}
				if err := tx.Where("hour_bucket < ?", cutoffUnix).Delete(&model.TrafficHourly{}).Error; err != nil {
					return err
				}
			}
			return tx.Where("date < ?", pruneDailyDate).Delete(&model.TrafficDaily{}).Error
		})
	})
}
