package service

import (
	"errors"
	"slices"
	"testing"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/xray"

	"gorm.io/gorm"
)

func seedNodeTrafficFixture(t *testing.T) {
	t.Helper()
	setupConflictDB(t)
	db := database.GetDB()

	nodes := []*model.Node{
		{Id: 1, Name: "edge-node-1", Address: "198.51.100.11", Port: 2053, ApiToken: "t1", Guid: "g1"},
		{Id: 2, Name: "edge-node-2", Address: "198.51.100.12", Port: 2053, ApiToken: "t2", Guid: "g2"},
	}
	for _, n := range nodes {
		if err := db.Create(n).Error; err != nil {
			t.Fatalf("create node %s: %v", n.Name, err)
		}
	}

	n1, n2 := 1, 2
	inbounds := []*model.Inbound{
		{Id: 1, Tag: "local-1", Enable: true, Port: 11701, Protocol: model.Shadowsocks, Settings: `{"clients":[]}`, Up: 900, Down: 3100, NodeID: nil},
		{Id: 2, Tag: "n1-rs", Enable: true, Port: 10703, Protocol: model.VLESS, Settings: `{"clients":[]}`, Up: 100, Down: 200, NodeID: &n1},
		{Id: 3, Tag: "n1-legend", Enable: true, Port: 10704, Protocol: model.VLESS, Settings: `{"clients":[]}`, Up: 50, Down: 150, NodeID: &n1},
		{Id: 4, Tag: "n2-ali", Enable: true, Port: 443, Protocol: model.VLESS, Settings: `{"clients":[]}`, Up: 200, Down: 800, NodeID: &n2},
	}
	for _, ib := range inbounds {
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("create inbound %s: %v", ib.Tag, err)
		}
	}

	clients := []*model.ClientRecord{
		{Id: 1, Email: "alice", Enable: true},
		{Id: 2, Email: "bob", Enable: true},
		{Id: 3, Email: "carol", Enable: true},
		{Id: 4, Email: "remote-only", Enable: true},
	}
	for _, c := range clients {
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("create client %s: %v", c.Email, err)
		}
	}

	links := []model.ClientInbound{
		{ClientId: 1, InboundId: 1},
		{ClientId: 1, InboundId: 2},
		{ClientId: 1, InboundId: 4},
		{ClientId: 2, InboundId: 1},
		{ClientId: 3, InboundId: 1},
		{ClientId: 3, InboundId: 3},
		{ClientId: 4, InboundId: 2},
	}
	for _, l := range links {
		if err := db.Create(&l).Error; err != nil {
			t.Fatalf("create client_inbound (%d,%d): %v", l.ClientId, l.InboundId, err)
		}
	}

	traffics := []*xray.ClientTraffic{
		{InboundId: 1, Email: "alice", Enable: true, Up: 1000, Down: 4000},
		{InboundId: 1, Email: "bob", Enable: true, Up: 400, Down: 1600},
		{InboundId: 1, Email: "carol", Enable: true, Up: 100, Down: 300},
		{InboundId: 2, Email: "remote-only", Enable: true, Up: 50, Down: 100},
	}
	for _, tr := range traffics {
		if err := db.Create(tr).Error; err != nil {
			t.Fatalf("create client_traffic %s: %v", tr.Email, err)
		}
	}

	nodeRows := []model.NodeClientTraffic{
		{NodeId: 1, Email: "alice", Up: 300, Down: 1200},
		{NodeId: 2, Email: "alice", Up: 200, Down: 800},
		{NodeId: 1, Email: "remote-only", Up: 50, Down: 100},
	}
	for _, nr := range nodeRows {
		if err := db.Create(&nr).Error; err != nil {
			t.Fatalf("create node_client_traffic (%d,%s): %v", nr.NodeId, nr.Email, err)
		}
	}
}

func TestNodeService_GetAllAndViewPopulatesNodeTrafficTotals(t *testing.T) {
	seedNodeTrafficFixture(t)
	svc := NodeService{}

	nodes, err := svc.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}

	if nodes[0].TotalUp != 150 || nodes[0].TotalDown != 350 || nodes[0].TotalTraffic != 500 {
		t.Fatalf("node 1 traffic = (%d,%d,%d), want (150,350,500)", nodes[0].TotalUp, nodes[0].TotalDown, nodes[0].TotalTraffic)
	}
	if nodes[1].TotalUp != 200 || nodes[1].TotalDown != 800 || nodes[1].TotalTraffic != 1000 {
		t.Fatalf("node 2 traffic = (%d,%d,%d), want (200,800,1000)", nodes[1].TotalUp, nodes[1].TotalDown, nodes[1].TotalTraffic)
	}

	view, err := svc.GetViewById(1)
	if err != nil {
		t.Fatalf("GetViewById(1): %v", err)
	}
	if view.TotalUp != 150 || view.TotalDown != 350 || view.TotalTraffic != 500 {
		t.Fatalf("view 1 traffic = (%d,%d,%d), want (150,350,500)", view.TotalUp, view.TotalDown, view.TotalTraffic)
	}
}

func TestNodeService_GetNodeTrafficBreakdown(t *testing.T) {
	seedNodeTrafficFixture(t)
	svc := NodeService{}

	t.Run("remote node includes tracked and attached zero-traffic clients", func(t *testing.T) {
		got, err := svc.GetNodeTrafficBreakdown(1)
		if err != nil {
			t.Fatalf("GetNodeTrafficBreakdown(1): %v", err)
		}
		if got.NodeId != 1 || got.NodeName != "edge-node-1" || got.TotalUp != 150 || got.TotalDown != 350 || got.TotalTraffic != 500 {
			t.Fatalf("unexpected header: %+v", got)
		}
		want := []NodeClientTrafficItem{
			{Email: "alice", Up: 300, Down: 1200, Total: 1500},
			{Email: "remote-only", Up: 50, Down: 100, Total: 150},
			{Email: "carol", Up: 0, Down: 0, Total: 0},
		}
		if !slices.Equal(got.Clients, want) {
			t.Fatalf("clients = %+v, want %+v", got.Clients, want)
		}
	})

	t.Run("local panel subtracts remote baselines and clamps at zero", func(t *testing.T) {
		got, err := svc.GetNodeTrafficBreakdown(0)
		if err != nil {
			t.Fatalf("GetNodeTrafficBreakdown(0): %v", err)
		}
		if got.NodeId != 0 || got.TotalUp != 900 || got.TotalDown != 3100 || got.TotalTraffic != 4000 {
			t.Fatalf("unexpected local header: %+v", got)
		}
		want := []NodeClientTrafficItem{
			{Email: "alice", Up: 500, Down: 2000, Total: 2500},
			{Email: "bob", Up: 400, Down: 1600, Total: 2000},
			{Email: "carol", Up: 100, Down: 300, Total: 400},
		}
		if !slices.Equal(got.Clients, want) {
			t.Fatalf("local clients = %+v, want %+v", got.Clients, want)
		}
	})

	t.Run("missing node returns ErrRecordNotFound", func(t *testing.T) {
		_, err := svc.GetNodeTrafficBreakdown(999)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("err = %v, want gorm.ErrRecordNotFound", err)
		}
	})
}

func TestInboundService_GetClientNodeTraffics(t *testing.T) {
	seedNodeTrafficFixture(t)
	svc := InboundService{}

	t.Run("multi-node client splits local and per-node usage", func(t *testing.T) {
		got, err := svc.GetClientNodeTraffics("alice")
		if err != nil {
			t.Fatalf("GetClientNodeTraffics: %v", err)
		}
		want := []ClientNodeTrafficItem{
			{NodeId: 0, NodeName: "", Up: 500, Down: 2000, Total: 2500},
			{NodeId: 1, NodeName: "edge-node-1", Up: 300, Down: 1200, Total: 1500},
			{NodeId: 2, NodeName: "edge-node-2", Up: 200, Down: 800, Total: 1000},
		}
		if !slices.Equal(got, want) {
			t.Fatalf("got = %+v, want %+v", got, want)
		}
	})

	t.Run("attached zero-traffic remote node still appears", func(t *testing.T) {
		got, err := svc.GetClientNodeTraffics("carol")
		if err != nil {
			t.Fatalf("GetClientNodeTraffics(carol): %v", err)
		}
		want := []ClientNodeTrafficItem{
			{NodeId: 0, NodeName: "", Up: 100, Down: 300, Total: 400},
			{NodeId: 1, NodeName: "edge-node-1", Up: 0, Down: 0, Total: 0},
		}
		if !slices.Equal(got, want) {
			t.Fatalf("got = %+v, want %+v", got, want)
		}
	})

	t.Run("remote-only client omits local when not attached and residual is zero", func(t *testing.T) {
		got, err := svc.GetClientNodeTraffics("remote-only")
		if err != nil {
			t.Fatalf("GetClientNodeTraffics(remote-only): %v", err)
		}
		want := []ClientNodeTrafficItem{
			{NodeId: 1, NodeName: "edge-node-1", Up: 50, Down: 100, Total: 150},
		}
		if !slices.Equal(got, want) {
			t.Fatalf("got = %+v, want %+v", got, want)
		}
	})

	t.Run("local residual clamps at zero if remote baselines exceed total", func(t *testing.T) {
		db := database.GetDB()
		if err := db.Model(&xray.ClientTraffic{}).Where("email = ?", "alice").Updates(map[string]any{"up": 100, "down": 100}).Error; err != nil {
			t.Fatalf("update total: %v", err)
		}
		got, err := svc.GetClientNodeTraffics("alice")
		if err != nil {
			t.Fatalf("GetClientNodeTraffics: %v", err)
		}
		if got[0].NodeId != 0 || got[0].Up != 0 || got[0].Down != 0 || got[0].Total != 0 {
			t.Fatalf("expected local clamped to 0, got %+v", got[0])
		}
	})
}
