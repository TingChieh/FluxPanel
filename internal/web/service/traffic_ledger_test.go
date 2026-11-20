package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/dbtest"
	"github.com/TingChieh/FluxPanel/internal/database/model"

	"gorm.io/gorm"
)

func initTrafficLedgerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	ResetTrafficLedgerForTest()
	t.Cleanup(ResetTrafficLedgerForTest)
	return database.GetDB()
}

func TestTrafficLedger_RecordSnapshotAndFlushUpsert(t *testing.T) {
	db := initTrafficLedgerTestDB(t)

	ts1 := time.Date(2026, 11, 20, 10, 15, 0, 0, time.UTC)
	ts2 := time.Date(2026, 11, 20, 10, 45, 0, 0, time.UTC)
	hourBucket := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).Unix()

	RecordTrafficDelta(ts1, 0, "alice", 100, 200)
	RecordTrafficDelta(ts2, 0, "alice", 50, 75)
	RecordTrafficDelta(ts1, 2, "", 10, 20)
	RecordTrafficDelta(ts1, 0, "zero", 0, 0)

	snap := SnapshotPendingTraffic()
	keyAlice := TrafficLedgerKey{HourBucket: hourBucket, NodeId: 0, Email: "alice"}
	if got := snap[keyAlice]; got != (TrafficDelta{Up: 150, Down: 275}) {
		t.Fatalf("pending alice = %+v, want {150 275}", got)
	}
	if _, exists := snap[TrafficLedgerKey{HourBucket: hourBucket, NodeId: 0, Email: "zero"}]; exists {
		t.Fatalf("zero delta should not be buffered")
	}

	if err := FlushTrafficLedger(); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	if len(SnapshotPendingTraffic()) != 0 {
		t.Fatalf("pending buffer not cleared after flush")
	}

	RecordTrafficDelta(ts2, 0, "alice", 25, 35)
	if err := FlushTrafficLedger(); err != nil {
		t.Fatalf("second flush: %v", err)
	}

	var row model.TrafficHourly
	if err := db.Where("hour_bucket = ? AND node_id = ? AND email = ?", hourBucket, 0, "alice").First(&row).Error; err != nil {
		t.Fatalf("query alice hourly: %v", err)
	}
	if row.Up != 175 || row.Down != 310 {
		t.Fatalf("alice hourly = (%d, %d), want (175, 310)", row.Up, row.Down)
	}

	var unattr model.TrafficHourly
	if err := db.Where("hour_bucket = ? AND node_id = ? AND email = ?", hourBucket, 2, "").First(&unattr).Error; err != nil {
		t.Fatalf("query unattributed hourly: %v", err)
	}
	if unattr.Up != 10 || unattr.Down != 20 {
		t.Fatalf("unattributed hourly = (%d, %d), want (10, 20)", unattr.Up, unattr.Down)
	}
}

func TestTrafficLedger_FlushFailureRequeuesDeltas(t *testing.T) {
	db := initTrafficLedgerTestDB(t)

	ts := time.Date(2026, 11, 20, 11, 5, 0, 0, time.UTC)
	hourBucket := time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC).Unix()
	RecordTrafficDelta(ts, 1, "bob", 400, 900)

	if err := db.Exec("DROP TABLE traffic_hourlies").Error; err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if err := FlushTrafficLedger(); err == nil {
		t.Fatalf("expected flush failure after dropping table")
	}

	snap := SnapshotPendingTraffic()
	key := TrafficLedgerKey{HourBucket: hourBucket, NodeId: 1, Email: "bob"}
	if got := snap[key]; got != (TrafficDelta{Up: 400, Down: 900}) {
		t.Fatalf("requeued delta = %+v, want {400 900}", got)
	}
}

func TestTrafficLedger_RenameMergesBufferAndTablesOnConflict(t *testing.T) {
	db := initTrafficLedgerTestDB(t)

	ts := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	hourBucket := ts.Unix()
	seedHourly := []model.TrafficHourly{
		{HourBucket: hourBucket, NodeId: 0, Email: "old@a", Up: 100, Down: 200},
		{HourBucket: hourBucket, NodeId: 0, Email: "new@a", Up: 30, Down: 40},
		{HourBucket: hourBucket, NodeId: 1, Email: "old@a", Up: 50, Down: 60},
	}
	if err := db.Create(&seedHourly).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}
	seedDaily := []model.TrafficDaily{
		{Date: "2026-06-01", NodeId: 0, Email: "old@a", Up: 1000, Down: 2000},
		{Date: "2026-06-01", NodeId: 0, Email: "new@a", Up: 300, Down: 400},
	}
	if err := db.Create(&seedDaily).Error; err != nil {
		t.Fatalf("seed daily: %v", err)
	}
	RecordTrafficDelta(ts, 0, "old@a", 7, 8)
	RecordTrafficDelta(ts, 0, "new@a", 1, 2)

	if err := db.Transaction(func(tx *gorm.DB) error {
		return RenameTrafficHistoryTx(tx, "old@a", "new@a")
	}); err != nil {
		t.Fatalf("rename traffic history: %v", err)
	}

	snap := SnapshotPendingTraffic()
	if _, ok := snap[TrafficLedgerKey{HourBucket: hourBucket, NodeId: 0, Email: "old@a"}]; ok {
		t.Fatalf("old@a still in pending buffer")
	}
	if got := snap[TrafficLedgerKey{HourBucket: hourBucket, NodeId: 0, Email: "new@a"}]; got != (TrafficDelta{Up: 8, Down: 10}) {
		t.Fatalf("pending new@a = %+v, want {8 10}", got)
	}

	var oldHourlyCount int64
	db.Model(&model.TrafficHourly{}).Where("email = ?", "old@a").Count(&oldHourlyCount)
	if oldHourlyCount != 0 {
		t.Fatalf("old hourly rows remain: %d", oldHourlyCount)
	}

	var h0, h1 model.TrafficHourly
	if err := db.Where("hour_bucket = ? AND node_id = ? AND email = ?", hourBucket, 0, "new@a").First(&h0).Error; err != nil {
		t.Fatalf("query merged hourly node 0: %v", err)
	}
	if h0.Up != 130 || h0.Down != 240 {
		t.Fatalf("merged hourly node 0 = (%d, %d), want (130, 240)", h0.Up, h0.Down)
	}
	if err := db.Where("hour_bucket = ? AND node_id = ? AND email = ?", hourBucket, 1, "new@a").First(&h1).Error; err != nil {
		t.Fatalf("query moved hourly node 1: %v", err)
	}
	if h1.Up != 50 || h1.Down != 60 {
		t.Fatalf("moved hourly node 1 = (%d, %d), want (50, 60)", h1.Up, h1.Down)
	}

	var d0 model.TrafficDaily
	if err := db.Where("date = ? AND node_id = ? AND email = ?", "2026-06-01", 0, "new@a").First(&d0).Error; err != nil {
		t.Fatalf("query merged daily: %v", err)
	}
	if d0.Up != 1300 || d0.Down != 2400 {
		t.Fatalf("merged daily = (%d, %d), want (1300, 2400)", d0.Up, d0.Down)
	}
}

func TestTrafficLedger_RollupAndPruneStrictCutoff(t *testing.T) {
	db := initTrafficLedgerTestDB(t)

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	now := time.Date(2026, 11, 20, 14, 30, 0, 0, loc)
	cutoffDate, cutoffUnix := TrafficHourlyCutoffDate(now, loc)
	if cutoffDate != "2026-08-22" {
		t.Fatalf("cutoffDate = %q, want 2026-08-22", cutoffDate)
	}

	// 2026-08-21 23:00+08:00 (before cutoff) and 2026-08-21 02:00+08:00 should roll into 2026-08-21.
	before1 := time.Date(2026, 8, 21, 2, 0, 0, 0, loc).Unix()
	before2 := time.Date(2026, 8, 21, 23, 0, 0, 0, loc).Unix()
	// Exact cutoff boundary 2026-08-22 00:00+08:00 must stay in traffic_hourlies.
	onCutoff := cutoffUnix

	seedHourly := []model.TrafficHourly{
		{HourBucket: before1, NodeId: 0, Email: "alice", Up: 100, Down: 200},
		{HourBucket: before2, NodeId: 0, Email: "alice", Up: 300, Down: 400},
		{HourBucket: onCutoff, NodeId: 0, Email: "alice", Up: 50, Down: 60},
	}
	if err := db.Create(&seedHourly).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}
	// Existing daily older than 365 days must be pruned; 364 days ago kept.
	seedDaily := []model.TrafficDaily{
		{Date: "2025-11-19", NodeId: 0, Email: "alice", Up: 9, Down: 9},
		{Date: "2025-11-21", NodeId: 0, Email: "alice", Up: 8, Down: 8},
	}
	if err := db.Create(&seedDaily).Error; err != nil {
		t.Fatalf("seed daily: %v", err)
	}

	if err := RollupAndPruneTrafficHistory(now, loc); err != nil {
		t.Fatalf("rollup and prune: %v", err)
	}

	var remainingHourly []model.TrafficHourly
	if err := db.Order("hour_bucket asc").Find(&remainingHourly).Error; err != nil {
		t.Fatalf("query remaining hourly: %v", err)
	}
	if len(remainingHourly) != 1 || remainingHourly[0].HourBucket != onCutoff {
		t.Fatalf("remaining hourly = %+v, want single row at cutoff %d", remainingHourly, onCutoff)
	}

	var rolled model.TrafficDaily
	if err := db.Where("date = ? AND node_id = ? AND email = ?", "2026-08-21", 0, "alice").First(&rolled).Error; err != nil {
		t.Fatalf("query rolled daily: %v", err)
	}
	if rolled.Up != 400 || rolled.Down != 600 {
		t.Fatalf("rolled daily = (%d, %d), want (400, 600)", rolled.Up, rolled.Down)
	}

	var expiredCount int64
	db.Model(&model.TrafficDaily{}).Where("date = ?", "2025-11-19").Count(&expiredCount)
	if expiredCount != 0 {
		t.Fatalf("expected expired daily row to be pruned")
	}
	var keptCount int64
	db.Model(&model.TrafficDaily{}).Where("date = ?", "2025-11-21").Count(&keptCount)
	if keptCount != 1 {
		t.Fatalf("expected within-365d daily row to be kept")
	}
}

func TestTrafficLedger_RenameRollbackPreservesPendingBuffer(t *testing.T) {
	db := initTrafficLedgerTestDB(t)

	ts := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC)
	hourBucket := ts.Unix()
	RecordTrafficDelta(ts, 0, "old@rb", 55, 66)

	forcedErr := gorm.ErrInvalidTransaction
	err := runSerializedTx(func(tx *gorm.DB) error {
		if err := RenameTrafficHistoryTx(tx, "old@rb", "new@rb"); err != nil {
			return err
		}
		return forcedErr
	})
	if err == nil {
		t.Fatalf("expected transaction rollback error")
	}
	_ = db

	snap := SnapshotPendingTraffic()
	oldKey := TrafficLedgerKey{HourBucket: hourBucket, NodeId: 0, Email: "old@rb"}
	newKey := TrafficLedgerKey{HourBucket: hourBucket, NodeId: 0, Email: "new@rb"}
	if got := snap[oldKey]; got != (TrafficDelta{Up: 55, Down: 66}) {
		t.Fatalf("after rollback old pending = %+v, want {55 66}", got)
	}
	if _, leaked := snap[newKey]; leaked {
		t.Fatalf("after rollback new@rb should not exist in pending buffer")
	}
}

func TestTrafficLedger_BatchSizeStaysWithinSQLiteVarLimit(t *testing.T) {
	if trafficLedgerBatchSize*5 > 900 {
		t.Fatalf("trafficLedgerBatchSize=%d binds %d vars (> 900 SQLite limit)", trafficLedgerBatchSize, trafficLedgerBatchSize*5)
	}
}
