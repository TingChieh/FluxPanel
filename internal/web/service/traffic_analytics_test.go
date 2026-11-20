package service

import (
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database/model"
)

func setPanelTimeLocationForTest(t *testing.T, tz string) {
	t.Helper()
	var s SettingService
	if err := s.saveSetting("timeLocation", tz); err != nil {
		t.Fatalf("saveSetting(timeLocation, %q): %v", tz, err)
	}
}

func TestTrafficAnalytics_DayPeriodHandlesDST23hAnd25h(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)
	setPanelTimeLocationForTest(t, "America/New_York")

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}

	// 2026-03-08 is US spring-forward (23 hours); 2026-11-01 is fall-back (25 hours).
	springHour := time.Date(2026, 3, 8, 3, 0, 0, 0, loc).UTC().Unix()
	fallFirst1AM := time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC).Unix()  // 01:00 EDT
	fallSecond1AM := time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC).Unix() // 01:00 EST

	if err := db.Create(&[]model.TrafficHourly{
		{HourBucket: springHour, NodeId: 0, Email: "alice", Up: 100, Down: 200},
		{HourBucket: fallFirst1AM, NodeId: 0, Email: "alice", Up: 300, Down: 400},
		{HourBucket: fallSecond1AM, NodeId: 0, Email: "alice", Up: 500, Down: 600},
	}).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}

	svc := TrafficAnalyticsService{
		nowFn: func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, loc) },
	}
	springRes, err := svc.GetOverview("day", "2026-03-08")
	if err != nil {
		t.Fatalf("spring overview: %v", err)
	}
	if len(springRes.Series) != 23 {
		t.Fatalf("spring-forward day buckets = %d, want 23", len(springRes.Series))
	}
	if springRes.Summary.TotalUp != 100 || springRes.Summary.TotalDown != 200 {
		t.Fatalf("spring total = (%d, %d), want (100, 200)", springRes.Summary.TotalUp, springRes.Summary.TotalDown)
	}

	svc.nowFn = func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, loc) }
	fallRes, err := svc.GetOverview("day", "2026-11-01")
	if err != nil {
		t.Fatalf("fall overview: %v", err)
	}
	if len(fallRes.Series) != 25 {
		t.Fatalf("fall-back day buckets = %d, want 25", len(fallRes.Series))
	}
	if fallRes.Summary.TotalUp != 800 || fallRes.Summary.TotalDown != 1000 {
		t.Fatalf("fall total = (%d, %d), want (800, 1000)", fallRes.Summary.TotalUp, fallRes.Summary.TotalDown)
	}
}

func TestTrafficAnalytics_WeekAndMonthCross90DayCutoffNoDoubleCount(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)
	setPanelTimeLocationForTest(t, "Asia/Shanghai")

	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 20, 15, 0, 0, 0, loc)
	// cutoffDate = 2026-08-22 (Saturday). Natural week containing 2026-08-22 is 2026-08-17 (Mon) .. 2026-08-23 (Sun).
	cutoffDate, _ := TrafficHourlyCutoffDate(now, loc)
	if cutoffDate != "2026-08-22" {
		t.Fatalf("unexpected cutoff %s", cutoffDate)
	}

	// Put 2026-08-21 in traffic_dailies, and ALSO a stale overlapping row for 2026-08-22 in dailies
	// plus real hourly data for 2026-08-22 in traffic_hourlies — cutoff rule must strictly read 2026-08-22 from hourlies only.
	if err := db.Create(&[]model.TrafficDaily{
		{Date: "2026-08-21", NodeId: 0, Email: "alice", Up: 100, Down: 200},
		{Date: "2026-08-22", NodeId: 0, Email: "alice", Up: 9999, Down: 9999},
		// Previous week 2026-08-10..2026-08-16 for changePct check (prevTotal = 1000).
		{Date: "2026-08-12", NodeId: 0, Email: "alice", Up: 400, Down: 600},
	}).Error; err != nil {
		t.Fatalf("seed daily: %v", err)
	}
	hCutoff := time.Date(2026, 8, 22, 10, 0, 0, 0, loc).Unix()
	if err := db.Create(&model.TrafficHourly{
		HourBucket: hCutoff, NodeId: 0, Email: "alice", Up: 300, Down: 400,
	}).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}
	// Unflushed in-memory delta on 2026-08-23 (within week, >= cutoffDate).
	RecordTrafficDelta(time.Date(2026, 8, 23, 9, 15, 0, 0, loc), 0, "alice", 200, 300)

	svc := TrafficAnalyticsService{nowFn: func() time.Time { return now }}
	res, err := svc.GetOverview("week", "2026-08-22")
	if err != nil {
		t.Fatalf("week overview: %v", err)
	}
	if len(res.Series) != 7 {
		t.Fatalf("week series len = %d, want 7", len(res.Series))
	}
	// Total = (100+200) + (300+400) + (200+300) = 1500. Prev = 1000 => +50%.
	if res.Summary.Total != 1500 {
		t.Fatalf("week total = %d, want 1500", res.Summary.Total)
	}
	if res.Summary.PrevTotal != 1000 || res.Summary.ChangePct == nil || *res.Summary.ChangePct != 50 {
		t.Fatalf("prev/changePct = (%d, %v), want (1000, 50)", res.Summary.PrevTotal, res.Summary.ChangePct)
	}

	// Zero previous period yields nil ChangePct.
	zeroPrev, err := svc.GetOverview("week", "2026-08-12")
	if err != nil {
		t.Fatalf("prev week overview: %v", err)
	}
	if zeroPrev.Summary.ChangePct != nil {
		t.Fatalf("expected nil changePct when prevTotal == 0, got %v", *zeroPrev.Summary.ChangePct)
	}
}

func TestTrafficAnalytics_ClientsPageSearchSortActiveOnlyAndDeleted(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)
	setPanelTimeLocationForTest(t, "UTC")

	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&[]model.ClientRecord{
		{Email: "alice", UUID: "u1", SubID: "s1", Enable: true},
		{Email: "bob", UUID: "u2", SubID: "s2", Enable: true},
		{Email: "idle", UUID: "u3", SubID: "s3", Enable: true},
	}).Error; err != nil {
		t.Fatalf("seed clients: %v", err)
	}
	h := now.Truncate(time.Hour).Unix()
	if err := db.Create(&[]model.TrafficHourly{
		{HourBucket: h, NodeId: 0, Email: "alice", Up: 100, Down: 900},
		{HourBucket: h, NodeId: 0, Email: "ghost", Up: 50, Down: 150},
		{HourBucket: h, NodeId: 0, Email: "", Up: 5000, Down: 5000}, // unattributed: never in client rank
	}).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}
	UpdateNodeLiveSpeeds(
		now,
		map[int]LiveSpeed{1: {Up: 500, Down: 1500}},
		map[int]map[string]LiveSpeed{1: {"bob": {Up: 500, Down: 1500}}},
	)

	svc := TrafficAnalyticsService{nowFn: func() time.Time { return now }}

	// Default traffic sort, activeOnly=false: alice (1000), ghost (200, deleted), bob (0), idle (0).
	allPage, err := svc.GetClientsPage("day", "2026-11-20", "", "traffic", false, 1, 10)
	if err != nil {
		t.Fatalf("GetClientsPage all: %v", err)
	}
	if allPage.Total != 4 {
		t.Fatalf("all total = %d, want 4 (alice, ghost, bob, idle; no empty email)", allPage.Total)
	}
	if allPage.Items[0].Email != "alice" || allPage.Items[1].Email != "ghost" || !allPage.Items[1].Deleted {
		t.Fatalf("unexpected top items: %+v", allPage.Items[:2])
	}

	// Speed sort, activeOnly=true: bob (speed 2000), alice (1000 traffic), ghost (200 traffic); idle excluded.
	speedPage, err := svc.GetClientsPage("day", "2026-11-20", "", "speed", true, 1, 2)
	if err != nil {
		t.Fatalf("GetClientsPage speed: %v", err)
	}
	if speedPage.Total != 3 || len(speedPage.Items) != 2 {
		t.Fatalf("speed active total/len = (%d, %d), want (3, 2)", speedPage.Total, len(speedPage.Items))
	}
	if speedPage.Items[0].Email != "bob" || speedPage.Items[0].DownSpeed != 1500 {
		t.Fatalf("expected bob first by live speed, got %+v", speedPage.Items[0])
	}

	// Search finds zero-traffic existing client when activeOnly=false.
	searchPage, err := svc.GetClientsPage("day", "2026-11-20", "IDL", "traffic", false, 1, 10)
	if err != nil {
		t.Fatalf("GetClientsPage search: %v", err)
	}
	if searchPage.Total != 1 || searchPage.Items[0].Email != "idle" {
		t.Fatalf("search result = %+v, want [idle]", searchPage.Items)
	}
}

func TestTrafficAnalytics_ClientAndNodeDetailsAndRetentionExpiry(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)
	setPanelTimeLocationForTest(t, "UTC")

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&model.Node{
		Id: 1, Name: "HK-01", Address: "198.51.100.1", Port: 2053, ApiToken: "t", Guid: "g1", Enable: true, Status: "online",
	}).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: "alice", UUID: "u1", SubID: "s1", Enable: true}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	h := now.Truncate(time.Hour).Unix()
	if err := db.Create(&[]model.TrafficHourly{
		{HourBucket: h, NodeId: 0, Email: "alice", Up: 100, Down: 300},
		{HourBucket: h, NodeId: 0, Email: "", Up: 50, Down: 50},
		{HourBucket: h, NodeId: 1, Email: "alice", Up: 200, Down: 400},
		{HourBucket: h, NodeId: 99, Email: "alice", Up: 10, Down: 90}, // deleted node
	}).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}

	svc := TrafficAnalyticsService{nowFn: func() time.Time { return now }}
	clientDetail, err := svc.GetClientDetail("alice", "day", "2026-11-20")
	if err != nil {
		t.Fatalf("GetClientDetail: %v", err)
	}
	if clientDetail.Summary.Total != 1100 || len(clientDetail.Nodes) != 3 {
		t.Fatalf("clientDetail total/nodes = (%d, %d), want (1100, 3)", clientDetail.Summary.Total, len(clientDetail.Nodes))
	}
	var sawDeletedNode bool
	for _, n := range clientDetail.Nodes {
		if n.NodeId == 99 && n.Deleted && n.NodeName == "Node #99" {
			sawDeletedNode = true
		}
	}
	if !sawDeletedNode {
		t.Fatalf("expected deleted node 99 fallback in client detail: %+v", clientDetail.Nodes)
	}

	nodeDetail, err := svc.GetNodeDetail(0, "day", "2026-11-20", "", 1, 10)
	if err != nil {
		t.Fatalf("GetNodeDetail(0): %v", err)
	}
	if nodeDetail.Summary.Total != 500 || nodeDetail.ClientsTotal != 2 {
		t.Fatalf("node 0 total/clients = (%d, %d), want (500, 2)", nodeDetail.Summary.Total, nodeDetail.ClientsTotal)
	}
	var sawUnattr bool
	for _, c := range nodeDetail.Clients {
		if c.Unattributed && c.Total == 100 {
			sawUnattr = true
		}
	}
	if !sawUnattr {
		t.Fatalf("expected unattributed row in node 0 detail: %+v", nodeDetail.Clients)
	}

	expiredDay, err := svc.GetOverview("day", "2026-01-01")
	if err != nil {
		t.Fatalf("expired day overview: %v", err)
	}
	if !expiredDay.Summary.HourlyExpired {
		t.Fatalf("expected HourlyExpired=true for day older than 90d cutoff")
	}
}

func TestTrafficAnalytics_HalfHourTimezoneIncludesFinalHalfHour(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)
	setPanelTimeLocationForTest(t, "Asia/Kolkata")

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load Asia/Kolkata: %v", err)
	}
	// 2026-11-22 is Sunday (last day of week 2026-11-16..2026-11-22).
	// 23:45 +05:30 is 18:15 UTC -> HourBucket = 18:00 UTC (which maps to 23:30 +05:30 on 2026-11-22).
	lateSunday := time.Date(2026, 11, 22, 23, 45, 0, 0, loc)
	if err := db.Create(&model.TrafficHourly{
		HourBucket: lateSunday.UTC().Truncate(time.Hour).Unix(),
		NodeId:     0,
		Email:      "kolkata",
		Up:         111,
		Down:       222,
	}).Error; err != nil {
		t.Fatalf("seed kolkata hourly: %v", err)
	}

	svc := TrafficAnalyticsService{nowFn: func() time.Time { return time.Date(2026, 11, 23, 10, 0, 0, 0, loc) }}
	res, err := svc.GetOverview("week", "2026-11-22")
	if err != nil {
		t.Fatalf("week overview: %v", err)
	}
	if res.Summary.TotalUp != 111 || res.Summary.TotalDown != 222 {
		t.Fatalf("kolkata late Sunday total = (%d, %d), want (111, 222)", res.Summary.TotalUp, res.Summary.TotalDown)
	}
}

func TestTrafficAnalytics_ActiveClientsNeverExceedsTotalClients(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)
	setPanelTimeLocationForTest(t, "UTC")

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&model.ClientRecord{Email: "alive", UUID: "u1", SubID: "s1", Enable: true}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	h := now.Truncate(time.Hour).Unix()
	if err := db.Create(&[]model.TrafficHourly{
		{HourBucket: h, NodeId: 0, Email: "alive", Up: 10, Down: 20},
		{HourBucket: h, NodeId: 0, Email: "deleted1", Up: 30, Down: 40},
		{HourBucket: h, NodeId: 0, Email: "deleted2", Up: 50, Down: 60},
	}).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}

	svc := TrafficAnalyticsService{nowFn: func() time.Time { return now }}
	res, err := svc.GetOverview("day", "2026-11-20")
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if res.Summary.ActiveClients != 1 || res.Summary.TotalClients != 1 {
		t.Fatalf("active/total clients = (%d, %d), want (1, 1)", res.Summary.ActiveClients, res.Summary.TotalClients)
	}
}
