package service

import (
	"path/filepath"
	"testing"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

func TestLegacySQLiteSchemaCompatibility(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-x-ui.db")
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("seed InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	legacyDDL := []string{
		`CREATE TABLE tuic_traffic_receipts (id integer primary key, inbound_id integer, email text, up integer, down integer)`,
		`INSERT INTO tuic_traffic_receipts (id, inbound_id, email, up, down) VALUES (1, 1, 'alice', 100, 200)`,
		`CREATE TABLE client_hwids (id integer primary key, sub_id text, hwid_hash text, first_seen integer, last_seen integer)`,
		`INSERT INTO client_hwids (id, sub_id, hwid_hash, first_seen, last_seen) VALUES (1, 'sub-alice', 'deadbeef', 1000, 2000)`,
		`ALTER TABLE clients ADD COLUMN limit_hwid integer DEFAULT 0`,
	}
	for _, stmt := range legacyDDL {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("legacy DDL %q: %v", stmt, err)
		}
	}

	if err := db.Exec(
		`INSERT INTO clients (email, sub_id, uuid, enable, limit_hwid) VALUES (?, ?, ?, ?, ?)`,
		"alice", "sub-alice", "11111111-1111-1111-1111-111111111111", true, 3,
	).Error; err != nil {
		t.Fatalf("insert legacy client: %v", err)
	}

	legacySettings := []model.Setting{
		{Key: "tgBotEnable", Value: "true"},
		{Key: "smtpEnable", Value: "true"},
		{Key: "ldapEnable", Value: "true"},
		{Key: "happLinkEnable", Value: "true"},
		{Key: "warp", Value: `{"privateKey":"legacy"}`},
		{Key: "subHappAutoDetect", Value: "true"},
		{Key: "subIncyAppAutoDetect", Value: "true"},
	}
	for _, row := range legacySettings {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("insert legacy setting %q: %v", row.Key, err)
		}
	}

	if err := database.CloseDB(); err != nil {
		t.Fatalf("CloseDB before reopen: %v", err)
	}

	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("reopen InitDB on legacy schema: %v", err)
	}

	settingSvc := &SettingService{}
	view, err := settingSvc.GetAllSettingView()
	if err != nil {
		t.Fatalf("GetAllSettingView on legacy DB: %v", err)
	}
	if view.WebPort == 0 {
		t.Fatalf("GetAllSettingView returned zero WebPort")
	}

	clientSvc := &ClientService{}
	clients, err := clientSvc.List()
	if err != nil {
		t.Fatalf("ClientService.List on legacy DB: %v", err)
	}
	if len(clients) != 1 || clients[0].Email != "alice" || clients[0].SubID != "sub-alice" {
		t.Fatalf("unexpected clients after legacy reopen: %+v", clients)
	}
}
