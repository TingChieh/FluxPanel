package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/util/crypto"
	"github.com/TingChieh/FluxPanel/internal/web/service"
	"github.com/TingChieh/FluxPanel/internal/xray"
)

func seedWebNodeTrafficFixture(t *testing.T) string {
	t.Helper()
	db := database.GetDB()
	token := "admin-breakdown-token"
	if err := db.Create(&model.ApiToken{
		Name: "admin", Token: crypto.HashTokenSHA256(token), Enabled: true, Scope: model.ApiScopeAdmin,
	}).Error; err != nil {
		t.Fatalf("seed token: %v", err)
	}
	if err := db.Create(&model.Node{
		Id: 1, Name: "edge-node-1", Address: "198.51.100.11", Port: 2053, ApiToken: "t", Guid: "g1",
	}).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	n1 := 1
	for _, ib := range []*model.Inbound{
		{Id: 1, Tag: "local-1", Enable: true, Port: 11701, Protocol: model.Shadowsocks, Settings: `{"clients":[]}`, Up: 50, Down: 150, NodeID: nil},
		{Id: 2, Tag: "n1-rs", Enable: true, Port: 10703, Protocol: model.VLESS, Settings: `{"clients":[]}`, Up: 300, Down: 700, NodeID: &n1},
	} {
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", ib.Tag, err)
		}
	}
	if err := db.Create(&model.ClientRecord{Id: 1, Email: "alice@test", Enable: true}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	for _, ci := range []model.ClientInbound{
		{ClientId: 1, InboundId: 1},
		{ClientId: 1, InboundId: 2},
	} {
		if err := db.Create(&ci).Error; err != nil {
			t.Fatalf("seed client_inbound: %v", err)
		}
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: 1, Email: "alice@test", Enable: true, Up: 350, Down: 850}).Error; err != nil {
		t.Fatalf("seed client_traffic: %v", err)
	}
	if err := db.Create(&model.NodeClientTraffic{NodeId: 1, Email: "alice@test", Up: 300, Down: 700}).Error; err != nil {
		t.Fatalf("seed node_client_traffic: %v", err)
	}
	return token
}

func TestNodeAndClientTrafficBreakdownEndpoints(t *testing.T) {
	engine := newTestEngine(t)
	token := seedWebNodeTrafficFixture(t)

	doGet := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}

	t.Run("node trafficBreakdown returns per-user node usage", func(t *testing.T) {
		w := doGet("/panel/api/nodes/trafficBreakdown/1")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Success bool                         `json:"success"`
			Obj     service.NodeTrafficBreakdown `json:"obj"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success || resp.Obj.NodeName != "edge-node-1" || resp.Obj.TotalTraffic != 1000 {
			t.Fatalf("unexpected breakdown: %+v", resp)
		}
		if len(resp.Obj.Clients) != 1 || resp.Obj.Clients[0].Email != "alice@test" || resp.Obj.Clients[0].Total != 1000 {
			t.Fatalf("unexpected clients: %+v", resp.Obj.Clients)
		}
	})

	t.Run("client nodeTraffics splits local and remote node usage", func(t *testing.T) {
		w := doGet("/panel/api/clients/nodeTraffics/alice@test")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Success bool                            `json:"success"`
			Obj     []service.ClientNodeTrafficItem `json:"obj"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success || len(resp.Obj) != 2 {
			t.Fatalf("unexpected resp: %+v", resp)
		}
		if resp.Obj[0].NodeId != 0 || resp.Obj[0].Total != 200 || resp.Obj[1].NodeId != 1 || resp.Obj[1].Total != 1000 {
			t.Fatalf("unexpected items: %+v", resp.Obj)
		}
	})

	t.Run("missing node returns failure envelope", func(t *testing.T) {
		w := doGet("/panel/api/nodes/trafficBreakdown/999")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var resp struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Success {
			t.Fatalf("expected success=false, got body=%s", w.Body.String())
		}
	})
}
