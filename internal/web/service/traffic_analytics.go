package service

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"

	"gorm.io/gorm"
)

// ErrInvalidTrafficQuery indicates malformed period, anchor, or pagination parameters.
var ErrInvalidTrafficQuery = errors.New("invalid traffic query")

// TrafficSeriesPoint represents one time bucket (hour or day) in a traffic chart.
type TrafficSeriesPoint struct {
	Timestamp int64  `json:"timestamp" example:"1763625600"`
	Label     string `json:"label" example:"14:00"`
	Up        int64  `json:"up" example:"268435456"`
	Down      int64  `json:"down" example:"1073741824"`
	Total     int64  `json:"total" example:"1342177280"`
	Future    bool   `json:"future" example:"false"`
}

// TrafficSummaryMetrics holds period totals and period-over-period change.
type TrafficSummaryMetrics struct {
	TotalUp   int64    `json:"totalUp" example:"1073741824"`
	TotalDown int64    `json:"totalDown" example:"5368709120"`
	Total     int64    `json:"total" example:"6442450944"`
	PrevTotal int64    `json:"prevTotal" example:"5368709120"`
	ChangePct *float64 `json:"changePct" example:"20"`
}

// TrafficOverviewSummary summarizes global period usage, active counts, and live speed.
type TrafficOverviewSummary struct {
	TotalUp        int64    `json:"totalUp" example:"1073741824"`
	TotalDown      int64    `json:"totalDown" example:"5368709120"`
	Total          int64    `json:"total" example:"6442450944"`
	PrevTotal      int64    `json:"prevTotal" example:"5368709120"`
	ChangePct      *float64 `json:"changePct" example:"20"`
	ActiveClients  int      `json:"activeClients" example:"14"`
	TotalClients   int      `json:"totalClients" example:"38"`
	TopClientEmail string   `json:"topClientEmail" example:"alice@example.com"`
	TopClientTotal int64    `json:"topClientTotal" example:"2147483648"`
	OnlineNodes    int      `json:"onlineNodes" example:"4"`
	TotalNodes     int      `json:"totalNodes" example:"4"`
	TopNodeId      int      `json:"topNodeId" example:"1"`
	TopNodeName    string   `json:"topNodeName" example:"HK-01"`
	TopNodeTotal   int64    `json:"topNodeTotal" example:"3221225472"`
	LiveUpSpeed    int64    `json:"liveUpSpeed" example:"262144"`
	LiveDownSpeed  int64    `json:"liveDownSpeed" example:"2097152"`
	HasHistory     bool     `json:"hasHistory" example:"true"`
	HourlyExpired  bool     `json:"hourlyExpired" example:"false"`
}

// TrafficClientRankItem holds one client's period usage and live speed in the ranking list.
type TrafficClientRankItem struct {
	Email     string  `json:"email" example:"alice@example.com"`
	Up        int64   `json:"up" example:"268435456"`
	Down      int64   `json:"down" example:"1073741824"`
	Total     int64   `json:"total" example:"1342177280"`
	SharePct  float64 `json:"sharePct" example:"20.83"`
	Online    bool    `json:"online" example:"true"`
	UpSpeed   int64   `json:"upSpeed" example:"65536"`
	DownSpeed int64   `json:"downSpeed" example:"524288"`
	Deleted   bool    `json:"deleted" example:"false"`
}

// TrafficNodeRankItem holds one node's period usage and live speed in the ranking list.
type TrafficNodeRankItem struct {
	NodeId    int     `json:"nodeId" example:"1"`
	NodeName  string  `json:"nodeName" example:"HK-01"`
	Status    string  `json:"status" example:"online"`
	Up        int64   `json:"up" example:"536870912"`
	Down      int64   `json:"down" example:"2684354560"`
	Total     int64   `json:"total" example:"3221225472"`
	SharePct  float64 `json:"sharePct" example:"50"`
	UpSpeed   int64   `json:"upSpeed" example:"131072"`
	DownSpeed int64   `json:"downSpeed" example:"1048576"`
	Deleted   bool    `json:"deleted" example:"false"`
}

// TrafficOverviewResponse is returned by GET /panel/api/traffic/overview.
type TrafficOverviewResponse struct {
	Period                string                 `json:"period" example:"day"`
	Anchor                string                 `json:"anchor" example:"2026-11-20"`
	Today                 string                 `json:"today" example:"2026-11-20"`
	Timezone              string                 `json:"timezone" example:"Asia/Shanghai"`
	RangeStart            string                 `json:"rangeStart" example:"2026-11-20"`
	RangeEnd              string                 `json:"rangeEnd" example:"2026-11-20"`
	BucketUnit            string                 `json:"bucketUnit" example:"hour"`
	HourlyRetentionCutoff string                 `json:"hourlyRetentionCutoff" example:"2026-08-22"`
	Summary               TrafficOverviewSummary `json:"summary"`
	Series                []TrafficSeriesPoint   `json:"series"`
	Nodes                 []TrafficNodeRankItem  `json:"nodes"`
}

// TrafficClientsPageResponse is returned by GET /panel/api/traffic/clients.
type TrafficClientsPageResponse struct {
	Total       int                     `json:"total" example:"38"`
	Page        int                     `json:"page" example:"1"`
	PageSize    int                     `json:"pageSize" example:"25"`
	PeriodTotal int64                   `json:"periodTotal" example:"6442450944"`
	Items       []TrafficClientRankItem `json:"items"`
}

// TrafficNodeBreakdownItem holds one node's share inside a client's detail drawer.
type TrafficNodeBreakdownItem struct {
	NodeId   int     `json:"nodeId" example:"1"`
	NodeName string  `json:"nodeName" example:"HK-01"`
	Status   string  `json:"status" example:"online"`
	Up       int64   `json:"up" example:"268435456"`
	Down     int64   `json:"down" example:"1073741824"`
	Total    int64   `json:"total" example:"1342177280"`
	SharePct float64 `json:"sharePct" example:"60"`
	Deleted  bool    `json:"deleted" example:"false"`
}

// TrafficClientDetailResponse is returned by GET /panel/api/traffic/client/:email.
type TrafficClientDetailResponse struct {
	Email     string                     `json:"email" example:"alice@example.com"`
	Online    bool                       `json:"online" example:"true"`
	Deleted   bool                       `json:"deleted" example:"false"`
	UpSpeed   int64                      `json:"upSpeed" example:"65536"`
	DownSpeed int64                      `json:"downSpeed" example:"524288"`
	Summary   TrafficSummaryMetrics      `json:"summary"`
	Series    []TrafficSeriesPoint       `json:"series"`
	Nodes     []TrafficNodeBreakdownItem `json:"nodes"`
}

// TrafficClientBreakdownItem holds one client's (or unattributed) share inside a node's detail drawer.
type TrafficClientBreakdownItem struct {
	Email        string  `json:"email" example:"alice@example.com"`
	Online       bool    `json:"online" example:"true"`
	Up           int64   `json:"up" example:"268435456"`
	Down         int64   `json:"down" example:"1073741824"`
	Total        int64   `json:"total" example:"1342177280"`
	SharePct     float64 `json:"sharePct" example:"41.67"`
	Deleted      bool    `json:"deleted" example:"false"`
	Unattributed bool    `json:"unattributed" example:"false"`
}

// TrafficNodeDetailResponse is returned by GET /panel/api/traffic/node/:id.
type TrafficNodeDetailResponse struct {
	NodeId       int                          `json:"nodeId" example:"1"`
	NodeName     string                       `json:"nodeName" example:"HK-01"`
	Status       string                       `json:"status" example:"online"`
	Deleted      bool                         `json:"deleted" example:"false"`
	UpSpeed      int64                        `json:"upSpeed" example:"131072"`
	DownSpeed    int64                        `json:"downSpeed" example:"1048576"`
	Summary      TrafficSummaryMetrics        `json:"summary"`
	Series       []TrafficSeriesPoint         `json:"series"`
	ClientsTotal int                          `json:"clientsTotal" example:"12"`
	Clients      []TrafficClientBreakdownItem `json:"clients"`
}

// TrafficAnalyticsService queries historical traffic ledgers and merges live speed state.
type TrafficAnalyticsService struct {
	settingService SettingService
	inboundService InboundService
	xrayService    XrayService
	nowFn          func() time.Time
}

func (s *TrafficAnalyticsService) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

func (s *TrafficAnalyticsService) panelLocation() (*time.Location, string) {
	loc, err := s.settingService.GetTimeLocation()
	if err != nil || loc == nil {
		loc = time.Local
	}
	name := loc.String()
	if name == "" {
		name = "Local"
	}
	return loc, name
}

type trafficBucketSpec struct {
	key       string
	timestamp int64
	label     string
	future    bool
}

type trafficPeriodRange struct {
	period         string
	anchor         string
	today          string
	rangeStartDate string
	rangeEndDate   string
	bucketUnit     string
	startTime      time.Time
	endTime        time.Time
	prevStartTime  time.Time
	prevEndTime    time.Time
	prevStartDate  string
	prevEndDate    string
	includesToday  bool
	buckets        []trafficBucketSpec
	dates          []string
	prevDates      []string
}

func resolveTrafficRange(period, anchor string, now time.Time, loc *time.Location) (*trafficPeriodRange, error) {
	if period == "" {
		period = "day"
	}
	switch period {
	case "day", "week", "month":
	default:
		return nil, fmt.Errorf("%w: unsupported period %q", ErrInvalidTrafficQuery, period)
	}

	localNow := now.In(loc)
	today := localNow.Format("2006-01-02")
	var anchorTime time.Time
	if strings.TrimSpace(anchor) == "" {
		anchor = today
		anchorTime = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	} else {
		parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(anchor), loc)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid anchor %q", ErrInvalidTrafficQuery, anchor)
		}
		anchor = parsed.Format("2006-01-02")
		anchorTime = parsed
	}

	r := &trafficPeriodRange{
		period: period,
		anchor: anchor,
		today:  today,
	}

	switch period {
	case "day":
		r.bucketUnit = "hour"
		r.startTime = anchorTime
		r.endTime = anchorTime.AddDate(0, 0, 1)
		r.rangeStartDate = anchor
		r.rangeEndDate = anchor
		r.dates = []string{anchor}
		r.prevStartTime = anchorTime.AddDate(0, 0, -1)
		r.prevEndTime = anchorTime
		prevDate := r.prevStartTime.Format("2006-01-02")
		r.prevStartDate = prevDate
		r.prevEndDate = prevDate
		r.prevDates = []string{prevDate}
		r.includesToday = anchor == today

		// Step in real UTC hours so DST 23h/25h days emit every actual local hour once.
		for t := r.startTime; t.Before(r.endTime); t = t.Add(time.Hour) {
			localT := t.In(loc)
			ts := t.UTC().Truncate(time.Hour).Unix()
			r.buckets = append(r.buckets, trafficBucketSpec{
				key:       fmt.Sprintf("%d", ts),
				timestamp: ts,
				label:     localT.Format("15:04"),
				future:    t.After(now),
			})
		}

	case "week":
		r.bucketUnit = "day"
		weekday := int(anchorTime.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := anchorTime.AddDate(0, 0, -(weekday - 1))
		nextMonday := monday.AddDate(0, 0, 7)
		prevMonday := monday.AddDate(0, 0, -7)

		r.startTime = monday
		r.endTime = nextMonday
		r.rangeStartDate = monday.Format("2006-01-02")
		r.rangeEndDate = nextMonday.AddDate(0, 0, -1).Format("2006-01-02")
		r.prevStartTime = prevMonday
		r.prevEndTime = monday
		r.prevStartDate = prevMonday.Format("2006-01-02")
		r.prevEndDate = monday.AddDate(0, 0, -1).Format("2006-01-02")
		r.includesToday = today >= r.rangeStartDate && today <= r.rangeEndDate

		for d := monday; d.Before(nextMonday); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			r.dates = append(r.dates, ds)
			r.buckets = append(r.buckets, trafficBucketSpec{
				key:       ds,
				timestamp: d.Unix(),
				label:     d.Format("01-02"),
				future:    ds > today,
			})
		}
		for d := prevMonday; d.Before(monday); d = d.AddDate(0, 0, 1) {
			r.prevDates = append(r.prevDates, d.Format("2006-01-02"))
		}

	case "month":
		r.bucketUnit = "day"
		first := time.Date(anchorTime.Year(), anchorTime.Month(), 1, 0, 0, 0, 0, loc)
		nextMonth := first.AddDate(0, 1, 0)
		prevMonth := first.AddDate(0, -1, 0)

		r.startTime = first
		r.endTime = nextMonth
		r.rangeStartDate = first.Format("2006-01-02")
		r.rangeEndDate = nextMonth.AddDate(0, 0, -1).Format("2006-01-02")
		r.prevStartTime = prevMonth
		r.prevEndTime = first
		r.prevStartDate = prevMonth.Format("2006-01-02")
		r.prevEndDate = first.AddDate(0, 0, -1).Format("2006-01-02")
		r.includesToday = today >= r.rangeStartDate && today <= r.rangeEndDate

		for d := first; d.Before(nextMonth); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			r.dates = append(r.dates, ds)
			r.buckets = append(r.buckets, trafficBucketSpec{
				key:       ds,
				timestamp: d.Unix(),
				label:     d.Format("01-02"),
				future:    ds > today,
			})
		}
		for d := prevMonth; d.Before(first); d = d.AddDate(0, 0, 1) {
			r.prevDates = append(r.prevDates, d.Format("2006-01-02"))
		}
	}

	return r, nil
}

type trafficAggRow struct {
	bucketKey string
	nodeID    int
	email     string
	up        int64
	down      int64
}

type trafficFilter struct {
	nodeID *int
	email  *string
}

func loadTrafficRows(
	db *gorm.DB,
	bucketUnit string,
	startTime, endTime time.Time,
	dates []string,
	cutoffDate string,
	cutoffUnix int64,
	loc *time.Location,
	filter trafficFilter,
) ([]trafficAggRow, error) {
	pending := SnapshotPendingTraffic()
	var rows []trafficAggRow

	if bucketUnit == "hour" {
		startUnix := startTime.UTC().Truncate(time.Hour).Unix()
		endUnix := endTime.UTC().Truncate(time.Hour).Unix()
		q := db.Model(&model.TrafficHourly{}).Where("hour_bucket >= ? AND hour_bucket < ?", startUnix, endUnix)
		if filter.nodeID != nil {
			q = q.Where("node_id = ?", *filter.nodeID)
		}
		if filter.email != nil {
			q = q.Where("email = ?", *filter.email)
		}
		var dbRows []model.TrafficHourly
		if err := q.Find(&dbRows).Error; err != nil {
			return nil, err
		}
		rows = make([]trafficAggRow, 0, len(dbRows)+len(pending))
		for _, h := range dbRows {
			rows = append(rows, trafficAggRow{
				bucketKey: fmt.Sprintf("%d", h.HourBucket),
				nodeID:    h.NodeId,
				email:     h.Email,
				up:        h.Up,
				down:      h.Down,
			})
		}
		for k, d := range pending {
			if k.HourBucket < startUnix || k.HourBucket >= endUnix {
				continue
			}
			if filter.nodeID != nil && k.NodeId != *filter.nodeID {
				continue
			}
			if filter.email != nil && k.Email != *filter.email {
				continue
			}
			rows = append(rows, trafficAggRow{
				bucketKey: fmt.Sprintf("%d", k.HourBucket),
				nodeID:    k.NodeId,
				email:     k.Email,
				up:        d.Up,
				down:      d.Down,
			})
		}
		return rows, nil
	}

	// Daily bucket mode: split dates strictly by cutoffDate (< cutoff => dailies, >= cutoff => hourlies + pending).
	var dailyDates []string
	hourlyDateSet := make(map[string]struct{})
	for _, ds := range dates {
		if ds < cutoffDate {
			dailyDates = append(dailyDates, ds)
		} else {
			hourlyDateSet[ds] = struct{}{}
		}
	}

	if len(dailyDates) > 0 {
		q := db.Model(&model.TrafficDaily{}).Where("date IN ?", dailyDates)
		if filter.nodeID != nil {
			q = q.Where("node_id = ?", *filter.nodeID)
		}
		if filter.email != nil {
			q = q.Where("email = ?", *filter.email)
		}
		var dRows []model.TrafficDaily
		if err := q.Find(&dRows).Error; err != nil {
			return nil, err
		}
		for _, d := range dRows {
			rows = append(rows, trafficAggRow{
				bucketKey: d.Date,
				nodeID:    d.NodeId,
				email:     d.Email,
				up:        d.Up,
				down:      d.Down,
			})
		}
	}

	if len(hourlyDateSet) > 0 {
		startUnix := max(startTime.UTC().Truncate(time.Hour).Unix(), cutoffUnix)
		endUnix := endTime.UTC().Truncate(time.Hour).Unix()
		if startUnix < endUnix {
			q := db.Model(&model.TrafficHourly{}).Where("hour_bucket >= ? AND hour_bucket < ?", startUnix, endUnix)
			if filter.nodeID != nil {
				q = q.Where("node_id = ?", *filter.nodeID)
			}
			if filter.email != nil {
				q = q.Where("email = ?", *filter.email)
			}
			var hRows []model.TrafficHourly
			if err := q.Find(&hRows).Error; err != nil {
				return nil, err
			}
			for _, h := range hRows {
				ds := time.Unix(h.HourBucket, 0).In(loc).Format("2006-01-02")
				if _, ok := hourlyDateSet[ds]; !ok {
					continue
				}
				rows = append(rows, trafficAggRow{
					bucketKey: ds,
					nodeID:    h.NodeId,
					email:     h.Email,
					up:        h.Up,
					down:      h.Down,
				})
			}
			for k, d := range pending {
				if k.HourBucket < startUnix || k.HourBucket >= endUnix {
					continue
				}
				if filter.nodeID != nil && k.NodeId != *filter.nodeID {
					continue
				}
				if filter.email != nil && k.Email != *filter.email {
					continue
				}
				ds := time.Unix(k.HourBucket, 0).In(loc).Format("2006-01-02")
				if _, ok := hourlyDateSet[ds]; !ok {
					continue
				}
				rows = append(rows, trafficAggRow{
					bucketKey: ds,
					nodeID:    k.NodeId,
					email:     k.Email,
					up:        d.Up,
					down:      d.Down,
				})
			}
		}
	}

	return rows, nil
}

func sumTrafficRows(rows []trafficAggRow) (up, down int64) {
	for _, r := range rows {
		up += r.up
		down += r.down
	}
	return up, down
}

func buildSeriesPoints(specs []trafficBucketSpec, rows []trafficAggRow) []TrafficSeriesPoint {
	byKey := make(map[string]TrafficDelta, len(specs))
	for _, r := range rows {
		cur := byKey[r.bucketKey]
		cur.Up += r.up
		cur.Down += r.down
		byKey[r.bucketKey] = cur
	}
	series := make([]TrafficSeriesPoint, len(specs))
	for i, sp := range specs {
		d := byKey[sp.key]
		series[i] = TrafficSeriesPoint{
			Timestamp: sp.timestamp,
			Label:     sp.label,
			Up:        d.Up,
			Down:      d.Down,
			Total:     d.Up + d.Down,
			Future:    sp.future,
		}
	}
	return series
}

func calcChangePct(cur, prev int64) *float64 {
	if prev <= 0 {
		return nil
	}
	pct := math.Round((float64(cur-prev)/float64(prev))*10000) / 100
	return &pct
}

func calcSharePct(part, total int64) float64 {
	if total <= 0 || part <= 0 {
		return 0
	}
	return math.Round((float64(part)/float64(total))*10000) / 100
}

func hasAnyTrafficHistory(db *gorm.DB) bool {
	if len(SnapshotPendingTraffic()) > 0 {
		return true
	}
	var count int64
	if err := db.Model(&model.TrafficHourly{}).Limit(1).Count(&count).Error; err == nil && count > 0 {
		return true
	}
	if err := db.Model(&model.TrafficDaily{}).Limit(1).Count(&count).Error; err == nil && count > 0 {
		return true
	}
	return false
}

func (s *TrafficAnalyticsService) localNodeStatus() string {
	if s.xrayService.IsXrayRunning() {
		return "online"
	}
	return "offline"
}

func loadExistingClientsAndNodes(db *gorm.DB) (map[string]struct{}, map[int]model.Node, error) {
	var emails []string
	if err := db.Model(&model.ClientRecord{}).Pluck("email", &emails).Error; err != nil {
		return nil, nil, err
	}
	clientSet := make(map[string]struct{}, len(emails))
	for _, e := range emails {
		if e != "" {
			clientSet[e] = struct{}{}
		}
	}

	var nodes []model.Node
	if err := db.Model(&model.Node{}).Find(&nodes).Error; err != nil {
		return nil, nil, err
	}
	nodeMap := make(map[int]model.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.Id] = n
	}
	return clientSet, nodeMap, nil
}

func (s *TrafficAnalyticsService) onlineEmailSet() map[string]struct{} {
	list := s.inboundService.GetOnlineClients()
	out := make(map[string]struct{}, len(list))
	for _, e := range list {
		out[e] = struct{}{}
	}
	return out
}

// GetOverview returns global KPIs, period time series, and node rankings.
func (s *TrafficAnalyticsService) GetOverview(period, anchor string) (*TrafficOverviewResponse, error) {
	loc, tzName := s.panelLocation()
	now := s.now()
	rng, err := resolveTrafficRange(period, anchor, now, loc)
	if err != nil {
		return nil, err
	}
	cutoffDate, cutoffUnix := TrafficHourlyCutoffDate(now, loc)
	db := database.GetDB()

	curRows, err := loadTrafficRows(db, rng.bucketUnit, rng.startTime, rng.endTime, rng.dates, cutoffDate, cutoffUnix, loc, trafficFilter{})
	if err != nil {
		return nil, err
	}
	prevRows, err := loadTrafficRows(db, rng.bucketUnit, rng.prevStartTime, rng.prevEndTime, rng.prevDates, cutoffDate, cutoffUnix, loc, trafficFilter{})
	if err != nil {
		return nil, err
	}

	totalUp, totalDown := sumTrafficRows(curRows)
	total := totalUp + totalDown
	prevUp, prevDown := sumTrafficRows(prevRows)
	prevTotal := prevUp + prevDown

	clientSet, nodeMap, err := loadExistingClientsAndNodes(db)
	if err != nil {
		return nil, err
	}
	live := GetLiveSpeedSnapshot(now)

	clientTotals := make(map[string]TrafficDelta)
	nodeTotals := make(map[int]TrafficDelta)
	for _, r := range curRows {
		if r.email != "" {
			cd := clientTotals[r.email]
			cd.Up += r.up
			cd.Down += r.down
			clientTotals[r.email] = cd
		}
		nd := nodeTotals[r.nodeID]
		nd.Up += r.up
		nd.Down += r.down
		nodeTotals[r.nodeID] = nd
	}

	activeClients := 0
	var topClientEmail string
	var topClientTotal int64
	for email, d := range clientTotals {
		t := d.Up + d.Down
		if t > 0 {
			activeClients++
			if t > topClientTotal || (t == topClientTotal && email < topClientEmail) {
				topClientEmail = email
				topClientTotal = t
			}
		}
	}

	// Build node rank list: always include local (0) + all existing nodes + any deleted nodes with traffic.
	nodeIDSet := make(map[int]struct{}, len(nodeMap)+len(nodeTotals)+1)
	nodeIDSet[0] = struct{}{}
	for id := range nodeMap {
		nodeIDSet[id] = struct{}{}
	}
	for id, d := range nodeTotals {
		if d.Up+d.Down > 0 {
			nodeIDSet[id] = struct{}{}
		}
	}

	localStatus := s.localNodeStatus()
	onlineNodes := 0
	if localStatus == "online" {
		onlineNodes++
	}
	for _, n := range nodeMap {
		if n.Enable && n.Status == "online" {
			onlineNodes++
		}
	}

	nodeItems := make([]TrafficNodeRankItem, 0, len(nodeIDSet))
	for id := range nodeIDSet {
		d := nodeTotals[id]
		sp := live.Nodes[id]
		item := TrafficNodeRankItem{
			NodeId:    id,
			Up:        d.Up,
			Down:      d.Down,
			Total:     d.Up + d.Down,
			SharePct:  calcSharePct(d.Up+d.Down, total),
			UpSpeed:   sp.Up,
			DownSpeed: sp.Down,
		}
		if id == 0 {
			item.NodeName = "Local"
			item.Status = localStatus
		} else if n, ok := nodeMap[id]; ok {
			item.NodeName = n.Name
			if n.Enable && n.Status == "online" {
				item.Status = "online"
			} else {
				item.Status = "offline"
			}
		} else {
			item.NodeName = fmt.Sprintf("Node #%d", id)
			item.Status = "deleted"
			item.Deleted = true
		}
		nodeItems = append(nodeItems, item)
	}
	slices.SortFunc(nodeItems, func(a, b TrafficNodeRankItem) int {
		if c := cmp.Compare(b.Total, a.Total); c != 0 {
			return c
		}
		if c := cmp.Compare(b.UpSpeed+b.DownSpeed, a.UpSpeed+a.DownSpeed); c != 0 {
			return c
		}
		return cmp.Compare(a.NodeId, b.NodeId)
	})

	var topNodeId int
	var topNodeName string
	var topNodeTotal int64
	if len(nodeItems) > 0 && nodeItems[0].Total > 0 {
		topNodeId = nodeItems[0].NodeId
		topNodeName = nodeItems[0].NodeName
		topNodeTotal = nodeItems[0].Total
	}

	return &TrafficOverviewResponse{
		Period:                rng.period,
		Anchor:                rng.anchor,
		Today:                 rng.today,
		Timezone:              tzName,
		RangeStart:            rng.rangeStartDate,
		RangeEnd:              rng.rangeEndDate,
		BucketUnit:            rng.bucketUnit,
		HourlyRetentionCutoff: cutoffDate,
		Summary: TrafficOverviewSummary{
			TotalUp:        totalUp,
			TotalDown:      totalDown,
			Total:          total,
			PrevTotal:      prevTotal,
			ChangePct:      calcChangePct(total, prevTotal),
			ActiveClients:  activeClients,
			TotalClients:   len(clientSet),
			TopClientEmail: topClientEmail,
			TopClientTotal: topClientTotal,
			OnlineNodes:    onlineNodes,
			TotalNodes:     len(nodeMap) + 1,
			TopNodeId:      topNodeId,
			TopNodeName:    topNodeName,
			TopNodeTotal:   topNodeTotal,
			LiveUpSpeed:    live.Total.Up,
			LiveDownSpeed:  live.Total.Down,
			HasHistory:     hasAnyTrafficHistory(db),
			HourlyExpired:  rng.period == "day" && rng.anchor < cutoffDate,
		},
		Series: buildSeriesPoints(rng.buckets, curRows),
		Nodes:  nodeItems,
	}, nil
}

// GetClientsPage returns a server-paginated, searchable, sortable client ranking for the chosen period.
func (s *TrafficAnalyticsService) GetClientsPage(period, anchor, search, sortBy string, activeOnly bool, page, pageSize int) (*TrafficClientsPageResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if sortBy == "" {
		sortBy = "traffic"
	}
	if sortBy != "traffic" && sortBy != "speed" {
		return nil, fmt.Errorf("%w: unsupported sort %q", ErrInvalidTrafficQuery, sortBy)
	}

	loc, _ := s.panelLocation()
	now := s.now()
	rng, err := resolveTrafficRange(period, anchor, now, loc)
	if err != nil {
		return nil, err
	}
	cutoffDate, cutoffUnix := TrafficHourlyCutoffDate(now, loc)
	db := database.GetDB()

	curRows, err := loadTrafficRows(db, rng.bucketUnit, rng.startTime, rng.endTime, rng.dates, cutoffDate, cutoffUnix, loc, trafficFilter{})
	if err != nil {
		return nil, err
	}
	totalUp, totalDown := sumTrafficRows(curRows)
	periodTotal := totalUp + totalDown

	clientSet, _, err := loadExistingClientsAndNodes(db)
	if err != nil {
		return nil, err
	}
	onlineSet := s.onlineEmailSet()
	live := GetLiveSpeedSnapshot(now)

	clientTotals := make(map[string]TrafficDelta)
	for _, r := range curRows {
		if r.email == "" {
			continue
		}
		cd := clientTotals[r.email]
		cd.Up += r.up
		cd.Down += r.down
		clientTotals[r.email] = cd
	}

	candidates := make(map[string]struct{}, len(clientSet)+len(clientTotals))
	for email := range clientSet {
		candidates[email] = struct{}{}
	}
	for email, d := range clientTotals {
		if d.Up+d.Down > 0 {
			candidates[email] = struct{}{}
		}
	}

	qSearch := strings.ToLower(strings.TrimSpace(search))
	items := make([]TrafficClientRankItem, 0, len(candidates))
	for email := range candidates {
		if qSearch != "" && !strings.Contains(strings.ToLower(email), qSearch) {
			continue
		}
		d := clientTotals[email]
		tot := d.Up + d.Down
		_, exists := clientSet[email]
		_, online := onlineSet[email]
		sp := live.Users[email]
		hasLive := online || sp.Up > 0 || sp.Down > 0

		if activeOnly {
			if tot <= 0 && !(rng.includesToday && hasLive) {
				continue
			}
		}

		items = append(items, TrafficClientRankItem{
			Email:     email,
			Up:        d.Up,
			Down:      d.Down,
			Total:     tot,
			SharePct:  calcSharePct(tot, periodTotal),
			Online:    online,
			UpSpeed:   sp.Up,
			DownSpeed: sp.Down,
			Deleted:   !exists,
		})
	}

	slices.SortFunc(items, func(a, b TrafficClientRankItem) int {
		if sortBy == "speed" {
			if c := cmp.Compare(b.UpSpeed+b.DownSpeed, a.UpSpeed+a.DownSpeed); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(b.Total, a.Total); c != 0 {
			return c
		}
		return cmp.Compare(a.Email, b.Email)
	})

	totalCount := len(items)
	start := (page - 1) * pageSize
	if start > totalCount {
		start = totalCount
	}
	end := min(start+pageSize, totalCount)

	return &TrafficClientsPageResponse{
		Total:       totalCount,
		Page:        page,
		PageSize:    pageSize,
		PeriodTotal: periodTotal,
		Items:       items[start:end],
	}, nil
}

// GetClientDetail returns one client's period time series and per-node breakdown.
func (s *TrafficAnalyticsService) GetClientDetail(email, period, anchor string) (*TrafficClientDetailResponse, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("%w: email is required", ErrInvalidTrafficQuery)
	}
	loc, _ := s.panelLocation()
	now := s.now()
	rng, err := resolveTrafficRange(period, anchor, now, loc)
	if err != nil {
		return nil, err
	}
	cutoffDate, cutoffUnix := TrafficHourlyCutoffDate(now, loc)
	db := database.GetDB()

	filter := trafficFilter{email: &email}
	curRows, err := loadTrafficRows(db, rng.bucketUnit, rng.startTime, rng.endTime, rng.dates, cutoffDate, cutoffUnix, loc, filter)
	if err != nil {
		return nil, err
	}
	prevRows, err := loadTrafficRows(db, rng.bucketUnit, rng.prevStartTime, rng.prevEndTime, rng.prevDates, cutoffDate, cutoffUnix, loc, filter)
	if err != nil {
		return nil, err
	}

	totalUp, totalDown := sumTrafficRows(curRows)
	total := totalUp + totalDown
	prevUp, prevDown := sumTrafficRows(prevRows)
	prevTotal := prevUp + prevDown

	clientSet, nodeMap, err := loadExistingClientsAndNodes(db)
	if err != nil {
		return nil, err
	}
	onlineSet := s.onlineEmailSet()
	live := GetLiveSpeedSnapshot(now)

	nodeTotals := make(map[int]TrafficDelta)
	for _, r := range curRows {
		nd := nodeTotals[r.nodeID]
		nd.Up += r.up
		nd.Down += r.down
		nodeTotals[r.nodeID] = nd
	}

	localStatus := s.localNodeStatus()
	nodes := make([]TrafficNodeBreakdownItem, 0, len(nodeTotals))
	for id, d := range nodeTotals {
		tot := d.Up + d.Down
		if tot <= 0 {
			continue
		}
		item := TrafficNodeBreakdownItem{
			NodeId:   id,
			Up:       d.Up,
			Down:     d.Down,
			Total:    tot,
			SharePct: calcSharePct(tot, total),
		}
		if id == 0 {
			item.NodeName = "Local"
			item.Status = localStatus
		} else if n, ok := nodeMap[id]; ok {
			item.NodeName = n.Name
			if n.Enable && n.Status == "online" {
				item.Status = "online"
			} else {
				item.Status = "offline"
			}
		} else {
			item.NodeName = fmt.Sprintf("Node #%d", id)
			item.Status = "deleted"
			item.Deleted = true
		}
		nodes = append(nodes, item)
	}
	slices.SortFunc(nodes, func(a, b TrafficNodeBreakdownItem) int {
		if c := cmp.Compare(b.Total, a.Total); c != 0 {
			return c
		}
		return cmp.Compare(a.NodeId, b.NodeId)
	})

	_, exists := clientSet[email]
	_, online := onlineSet[email]
	sp := live.Users[email]

	return &TrafficClientDetailResponse{
		Email:     email,
		Online:    online,
		Deleted:   !exists,
		UpSpeed:   sp.Up,
		DownSpeed: sp.Down,
		Summary: TrafficSummaryMetrics{
			TotalUp:   totalUp,
			TotalDown: totalDown,
			Total:     total,
			PrevTotal: prevTotal,
			ChangePct: calcChangePct(total, prevTotal),
		},
		Series: buildSeriesPoints(rng.buckets, curRows),
		Nodes:  nodes,
	}, nil
}

// GetNodeDetail returns one node's period time series and paginated per-client breakdown.
func (s *TrafficAnalyticsService) GetNodeDetail(nodeID int, period, anchor, search string, page, pageSize int) (*TrafficNodeDetailResponse, error) {
	if nodeID < 0 {
		return nil, fmt.Errorf("%w: invalid node id", ErrInvalidTrafficQuery)
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}

	loc, _ := s.panelLocation()
	now := s.now()
	rng, err := resolveTrafficRange(period, anchor, now, loc)
	if err != nil {
		return nil, err
	}
	cutoffDate, cutoffUnix := TrafficHourlyCutoffDate(now, loc)
	db := database.GetDB()

	clientSet, nodeMap, err := loadExistingClientsAndNodes(db)
	if err != nil {
		return nil, err
	}

	filter := trafficFilter{nodeID: &nodeID}
	curRows, err := loadTrafficRows(db, rng.bucketUnit, rng.startTime, rng.endTime, rng.dates, cutoffDate, cutoffUnix, loc, filter)
	if err != nil {
		return nil, err
	}
	prevRows, err := loadTrafficRows(db, rng.bucketUnit, rng.prevStartTime, rng.prevEndTime, rng.prevDates, cutoffDate, cutoffUnix, loc, filter)
	if err != nil {
		return nil, err
	}

	var nodeName, status string
	var deleted bool
	if nodeID == 0 {
		nodeName = "Local"
		status = s.localNodeStatus()
	} else if n, ok := nodeMap[nodeID]; ok {
		nodeName = n.Name
		if n.Enable && n.Status == "online" {
			status = "online"
		} else {
			status = "offline"
		}
	} else {
		if len(curRows) == 0 && len(prevRows) == 0 {
			return nil, gorm.ErrRecordNotFound
		}
		nodeName = fmt.Sprintf("Node #%d", nodeID)
		status = "deleted"
		deleted = true
	}

	totalUp, totalDown := sumTrafficRows(curRows)
	total := totalUp + totalDown
	prevUp, prevDown := sumTrafficRows(prevRows)
	prevTotal := prevUp + prevDown

	onlineSet := s.onlineEmailSet()
	live := GetLiveSpeedSnapshot(now)

	byEmail := make(map[string]TrafficDelta)
	for _, r := range curRows {
		cd := byEmail[r.email]
		cd.Up += r.up
		cd.Down += r.down
		byEmail[r.email] = cd
	}

	qSearch := strings.ToLower(strings.TrimSpace(search))
	clients := make([]TrafficClientBreakdownItem, 0, len(byEmail))
	for email, d := range byEmail {
		tot := d.Up + d.Down
		if tot <= 0 {
			continue
		}
		unattr := email == ""
		if qSearch != "" && (unattr || !strings.Contains(strings.ToLower(email), qSearch)) {
			continue
		}
		_, exists := clientSet[email]
		_, online := onlineSet[email]
		clients = append(clients, TrafficClientBreakdownItem{
			Email:        email,
			Online:       !unattr && online,
			Up:           d.Up,
			Down:         d.Down,
			Total:        tot,
			SharePct:     calcSharePct(tot, total),
			Deleted:      !unattr && !exists,
			Unattributed: unattr,
		})
	}
	slices.SortFunc(clients, func(a, b TrafficClientBreakdownItem) int {
		if c := cmp.Compare(b.Total, a.Total); c != 0 {
			return c
		}
		return cmp.Compare(a.Email, b.Email)
	})

	clientsTotal := len(clients)
	start := (page - 1) * pageSize
	if start > clientsTotal {
		start = clientsTotal
	}
	end := min(start+pageSize, clientsTotal)

	sp := live.Nodes[nodeID]
	return &TrafficNodeDetailResponse{
		NodeId:    nodeID,
		NodeName:  nodeName,
		Status:    status,
		Deleted:   deleted,
		UpSpeed:   sp.Up,
		DownSpeed: sp.Down,
		Summary: TrafficSummaryMetrics{
			TotalUp:   totalUp,
			TotalDown: totalDown,
			Total:     total,
			PrevTotal: prevTotal,
			ChangePct: calcChangePct(total, prevTotal),
		},
		Series:       buildSeriesPoints(rng.buckets, curRows),
		ClientsTotal: clientsTotal,
		Clients:      clients[start:end],
	}, nil
}
