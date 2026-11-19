package nodee2e

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

// seedAuditRows writes one local audit day for email, as if the node's own
// access-log ingest had produced it; it must not run against a live panel.
func (p *panel) seedAuditRows(date, email, srcIP string, hits int64) {
	p.t.Helper()
	if p.cmd != nil {
		p.t.Fatalf("%s: seedAuditRows on a running panel", p.name)
	}
	sharedDBMu.Lock()
	defer sharedDBMu.Unlock()
	if err := database.InitDB(filepath.Join(p.dir, "db", "x-ui.db")); err != nil {
		p.t.Fatalf("%s: open db: %v", p.name, err)
	}
	defer func() { _ = database.CloseDB() }()
	db := database.GetDB()
	now := time.Now().Unix()
	if err := db.Create(&model.AccessAuditDaily{
		Date: date, Email: email, RootDomain: "example.com", Domain: "www.example.com", Port: 443,
		HitCount: hits, FirstSeen: now, LastSeen: now,
	}).Error; err != nil {
		p.t.Fatalf("%s: seed audit daily: %v", p.name, err)
	}
	if err := db.Create(&model.AccessAuditClientIP{
		Date: date, Email: email, SrcIP: srcIP, Subnet: srcIP + "/32", HitCount: hits,
		FirstSeen: now, LastSeen: now, ActiveHours: 1 << uint(time.Now().Hour()),
	}).Error; err != nil {
		p.t.Fatalf("%s: seed audit ip: %v", p.name, err)
	}
}

// A node's own audit days reach the master through the node-sync token, tagged
// with the node as their source, and the pull is recorded on the source status.
func TestNodeAuditPull(t *testing.T) {
	bin := panelBinary(t)
	master := newPanel(t, bin, "master")
	node := newPanel(t, bin, "node")
	today := time.Now().Format("2006-01-02")
	node.seedAuditRows(today, "hk-only@e2e", "203.0.113.9", 42)
	linkToken := node.mintToken("master-link", "node-sync")
	master.start()
	node.start()

	var nodeView struct {
		Id int `json:"id"`
	}
	obj := master.call(http.MethodPost, "/panel/api/nodes/add", map[string]any{
		"name": "hk", "scheme": "http", "address": "127.0.0.1", "port": node.port, "basePath": "/",
		"apiToken": linkToken, "enable": true, "allowPrivateAddress": true,
	})
	if err := json.Unmarshal(obj, &nodeView); err != nil || nodeView.Id == 0 {
		t.Fatalf("decode node add: %v (%s)", err, obj)
	}

	eventually(t, settleTimeout, "a manual ingest merges the node's audit snapshot", func() (bool, string) {
		env, err := master.try(http.MethodPost, "/panel/api/audit/ingest", nil)
		if err != nil {
			return false, err.Error()
		}
		var res struct {
			NodesSynced int `json:"nodesSynced"`
			NodesFailed int `json:"nodesFailed"`
		}
		_ = json.Unmarshal(env.Obj, &res)
		return res.NodesSynced == 1, "nodesSynced=" + strconv.Itoa(res.NodesSynced) + " nodesFailed=" + strconv.Itoa(res.NodesFailed)
	})

	var summary struct {
		Clients []struct {
			Email     string `json:"email"`
			TotalHits int64  `json:"totalHits"`
			NodeIds   []int  `json:"nodeIds"`
		} `json:"clients"`
		Sources []struct {
			NodeId     int    `json:"nodeId"`
			TotalHits  int64  `json:"totalHits"`
			LastSyncAt int64  `json:"lastSyncAt"`
			SyncError  string `json:"syncError"`
		} `json:"sources"`
	}
	raw := master.call(http.MethodGet, "/panel/api/audit/summary?days=1&date="+today+"&nodeId="+strconv.Itoa(nodeView.Id), nil)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("decode summary: %v (%s)", err, raw)
	}
	if len(summary.Clients) != 1 || summary.Clients[0].Email != "hk-only@e2e" || summary.Clients[0].TotalHits != 42 ||
		len(summary.Clients[0].NodeIds) != 1 || summary.Clients[0].NodeIds[0] != nodeView.Id {
		t.Fatalf("node-filtered clients = %+v, want hk-only@e2e with 42 hits from node %d", summary.Clients, nodeView.Id)
	}
	var found bool
	for _, src := range summary.Sources {
		if src.NodeId == nodeView.Id {
			found = true
			if src.TotalHits != 42 || src.LastSyncAt == 0 || src.SyncError != "" {
				t.Fatalf("node source = %+v, want 42 hits, a recorded pull and no error", src)
			}
		}
	}
	if !found {
		t.Fatalf("sources = %+v, want node %d listed", summary.Sources, nodeView.Id)
	}
}
