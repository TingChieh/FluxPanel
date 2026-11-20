package service

import (
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/web/runtime"
	"github.com/TingChieh/FluxPanel/internal/xray"
)

func sumPendingByKey(nodeID int, email string) TrafficDelta {
	var sum TrafficDelta
	for k, d := range SnapshotPendingTraffic() {
		if k.NodeId == nodeID && k.Email == email {
			sum.Up += d.Up
			sum.Down += d.Down
		}
	}
	return sum
}

func TestTrafficCollection_LocalClientAndUnattributedInbound(t *testing.T) {
	db := initTrafficLedgerTestDB(t)

	ib := &model.Inbound{
		UserId:   1,
		Tag:      "local-vless",
		Enable:   true,
		Port:     10001,
		Protocol: model.VLESS,
		Settings: `{"clients":[{"email":"alice","id":"11111111-1111-1111-1111-111111111111","enable":true}]}`,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: ib.Id, Email: "alice", Enable: true}).Error; err != nil {
		t.Fatalf("create client traffic: %v", err)
	}

	ibSvc := InboundService{}
	_, _, err := ibSvc.AddTraffic(
		[]*xray.Traffic{{IsInbound: true, Tag: "local-vless", Up: 500, Down: 1000}},
		[]*xray.ClientTraffic{{Email: "alice", Up: 300, Down: 800}},
	)
	if err != nil {
		t.Fatalf("AddTraffic: %v", err)
	}

	if got := sumPendingByKey(0, "alice"); got != (TrafficDelta{Up: 300, Down: 800}) {
		t.Fatalf("local alice pending = %+v, want {300 800}", got)
	}
	if got := sumPendingByKey(0, ""); got != (TrafficDelta{Up: 200, Down: 200}) {
		t.Fatalf("local unattributed pending = %+v, want {200 200}", got)
	}
}

func TestTrafficCollection_RemoteNodeFirstImportSkipsHistoricalTotals(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ibSvc := InboundService{}

	if err := db.Create(&model.Node{Id: 1, Name: "hk-1", Address: "198.51.100.11", Port: 2053, ApiToken: "t", Guid: "g-hk1", Enable: true}).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	rawSettings := `{"clients":[{"email":"alice","id":"11111111-1111-1111-1111-111111111111","enable":true}]}`
	snap1 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{
			Tag:         "n1-vless",
			Enable:      true,
			Port:        11703,
			Protocol:    model.VLESS,
			Settings:    rawSettings,
			Up:          10_000_000_000,
			Down:        20_000_000_000,
			ClientStats: []xray.ClientTraffic{{Email: "alice", Enable: true, Up: 10_000_000_000, Down: 20_000_000_000}},
		}},
	}
	if _, err := ibSvc.SetRemoteTraffic(1, snap1, false, false); err != nil {
		t.Fatalf("first SetRemoteTraffic: %v", err)
	}
	if len(SnapshotPendingTraffic()) != 0 {
		t.Fatalf("first import recorded historical totals: %+v", SnapshotPendingTraffic())
	}

	snap2 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{
			Tag:         "n1-vless",
			Enable:      true,
			Port:        11703,
			Protocol:    model.VLESS,
			Settings:    rawSettings,
			Up:          10_001_000_000,
			Down:        20_002_500_000,
			ClientStats: []xray.ClientTraffic{{Email: "alice", Enable: true, Up: 10_000_800_000, Down: 20_002_000_000}},
		}},
	}
	if _, err := ibSvc.SetRemoteTraffic(1, snap2, false, false); err != nil {
		t.Fatalf("second SetRemoteTraffic: %v", err)
	}
	if got := sumPendingByKey(1, "alice"); got != (TrafficDelta{Up: 800_000, Down: 2_000_000}) {
		t.Fatalf("remote alice delta = %+v, want {800000 2000000}", got)
	}
	if got := sumPendingByKey(1, ""); got != (TrafficDelta{Up: 200_000, Down: 500_000}) {
		t.Fatalf("remote unattributed delta = %+v, want {200000 500000}", got)
	}
}

func TestTrafficCollection_RemoteNodeRenewedDeltaAndResetPreservesHistory(t *testing.T) {
	db := initTrafficLedgerTestDB(t)
	ibSvc := InboundService{}

	if err := db.Create(&model.Node{Id: 2, Name: "jp-1", Address: "198.51.100.12", Port: 2053, ApiToken: "t", Guid: "g-jp1", Enable: true}).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	now := time.Now().UnixMilli()
	rawSettings := `{"clients":[{"email":"bob","id":"22222222-2222-2222-2222-222222222222","enable":true,"expiryTime":` + "1700000000000" + `,"reset":30}]}`
	snap1 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{
			Tag:         "n2-vless",
			Enable:      true,
			Port:        11704,
			Protocol:    model.VLESS,
			Settings:    rawSettings,
			Up:          500,
			Down:        1000,
			ClientStats: []xray.ClientTraffic{{Email: "bob", Enable: true, Up: 500, Down: 1000, ExpiryTime: now + 10_000, Reset: 30, ResetCount: 0}},
		}},
	}
	if _, err := ibSvc.SetRemoteTraffic(2, snap1, false, false); err != nil {
		t.Fatalf("first SetRemoteTraffic: %v", err)
	}

	// Simulate renewal on remote node: counters reset and started accumulating 120/340, expiry extended, reset_count bumped.
	renewedSettings := `{"clients":[{"email":"bob","id":"22222222-2222-2222-2222-222222222222","enable":true,"expiryTime":` + "2999999999999" + `,"reset":30}]}`
	snap2 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{
			Tag:         "n2-vless",
			Enable:      true,
			Port:        11704,
			Protocol:    model.VLESS,
			Settings:    renewedSettings,
			Up:          620,
			Down:        1340,
			ClientStats: []xray.ClientTraffic{{Email: "bob", Enable: true, Up: 120, Down: 340, ExpiryTime: now + 86_400_000*30, Reset: 30, ResetCount: 1}},
		}},
	}
	if _, err := ibSvc.SetRemoteTraffic(2, snap2, false, false); err != nil {
		t.Fatalf("renewed SetRemoteTraffic: %v", err)
	}
	if got := sumPendingByKey(2, "bob"); got != (TrafficDelta{Up: 120, Down: 340}) {
		t.Fatalf("renewed bob delta = %+v, want {120 340}", got)
	}

	if err := FlushTrafficLedger(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := ibSvc.ResetClientTrafficByEmail("bob"); err != nil {
		t.Fatalf("ResetClientTrafficByEmail: %v", err)
	}
	var row model.TrafficHourly
	if err := db.Where("node_id = ? AND email = ?", 2, "bob").First(&row).Error; err != nil {
		t.Fatalf("hourly row lost after quota reset: %v", err)
	}
	if row.Up != 120 || row.Down != 340 {
		t.Fatalf("hourly row after reset = (%d, %d), want (120, 340)", row.Up, row.Down)
	}
}

func TestTrafficCollection_ClientRenameMigratesHistory(t *testing.T) {
	db := initTrafficLedgerTestDB(t)

	rec := &model.ClientRecord{
		Email:  "alice",
		UUID:   "11111111-1111-1111-1111-111111111111",
		SubID:  "sub-alice",
		Enable: true,
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("create client record: %v", err)
	}
	hour := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC).Unix()
	if err := db.Create(&[]model.TrafficHourly{
		{HourBucket: hour, NodeId: 0, Email: "alice", Up: 100, Down: 200},
		{HourBucket: hour, NodeId: 0, Email: "alice_new", Up: 40, Down: 60},
	}).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}
	RecordTrafficDelta(time.Unix(hour, 0), 0, "alice", 10, 20)

	clientSvc := ClientService{}
	ibSvc := InboundService{}
	updated := model.Client{
		Email:  "alice_new",
		ID:     rec.UUID,
		SubID:  rec.SubID,
		Enable: true,
	}
	if _, err := clientSvc.Update(&ibSvc, rec.Id, updated); err != nil {
		t.Fatalf("ClientService.Update rename: %v", err)
	}

	var merged model.TrafficHourly
	if err := db.Where("hour_bucket = ? AND node_id = ? AND email = ?", hour, 0, "alice_new").First(&merged).Error; err != nil {
		t.Fatalf("query renamed hourly: %v", err)
	}
	if merged.Up != 140 || merged.Down != 260 {
		t.Fatalf("merged hourly = (%d, %d), want (140, 260)", merged.Up, merged.Down)
	}
	if got := sumPendingByKey(0, "alice_new"); got != (TrafficDelta{Up: 10, Down: 20}) {
		t.Fatalf("pending renamed delta = %+v, want {10 20}", got)
	}
	if got := sumPendingByKey(0, "alice"); got != (TrafficDelta{}) {
		t.Fatalf("old pending delta remained: %+v", got)
	}
}
