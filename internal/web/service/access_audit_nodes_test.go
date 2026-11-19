package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/web/entity"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const auditNodeID = 7

func ingestAuditLines(t *testing.T, svc *AccessAuditService, content string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatalf("ingest: %v", err)
	}
}

func nodeExportFor(t *testing.T, daily []entity.AuditExportDaily, ips []entity.AuditExportIP) entity.AuditExport {
	t.Helper()
	exp := newAuditExportHeader(auditLocation(), "2026-11-19", "2026-11-19")
	exp.Daily = daily
	exp.IPs = ips
	return exp
}

func dailyRowsFor(t *testing.T, nodeID int) []model.AccessAuditDaily {
	t.Helper()
	var rows []model.AccessAuditDaily
	if err := database.GetDB().Where("node_id = ?", nodeID).Order("domain").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// A client seen from one subnet locally and two more on a node in the same hour
// is sharing even though neither source alone crosses the threshold.
func TestAccessAuditNodeImportMergesSourcesWithoutDoubleCounting(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	ingestAuditLines(t, svc, "2026/11/19 15:01:00.000000 from tcp:198.51.100.1:50001 accepted tcp:github.com:443 [in-1 >> direct] email: alice\n")

	ts := time.Date(2026, 11, 19, 15, 10, 0, 0, auditLocation()).Unix()
	exp := nodeExportFor(t,
		[]entity.AuditExportDaily{{Date: "2026-11-19", Email: "alice", RootDomain: "youtube.com", Domain: "youtube.com", Port: 443, Outbound: "to-sg", HitCount: 5, FirstSeen: ts, LastSeen: ts}},
		[]entity.AuditExportIP{
			{Date: "2026-11-19", Email: "alice", SrcIP: "203.0.113.2", Subnet: "203.0.113.0/24", HitCount: 3, FirstSeen: ts, LastSeen: ts, ActiveHours: 1 << 15},
			{Date: "2026-11-19", Email: "alice", SrcIP: "192.0.2.3", Subnet: "192.0.2.0/24", HitCount: 2, FirstSeen: ts, LastSeen: ts, ActiveHours: 1 << 15},
		})
	for range 2 {
		if err := svc.ImportNodeExport(auditNodeID, "2026-11-19", "2026-11-19", exp); err != nil {
			t.Fatalf("ImportNodeExport: %v", err)
		}
	}

	summary, err := svc.GetSummary("2026-11-19", 1, AuditAllSources)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Clients) != 1 || summary.Clients[0].TotalHits != 6 || summary.Clients[0].MaxConcurrentSubnets != 3 {
		t.Fatalf("clients = %+v, want alice with 6 hits (1 local + 5 node, imported twice) and 3 concurrent subnets", summary.Clients)
	}
	alerts, err := svc.GetAlerts("2026-11-19", "alice", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].RuleName != "sharing:concurrent-subnets" || alerts[0].HitCount != 3 {
		t.Fatalf("alerts = %+v, want one cross-source concurrent-subnets alert with 3 subnets", alerts)
	}

	exp.Daily[0].HitCount = 8
	exp.IPs = exp.IPs[:1]
	if err := svc.ImportNodeExport(auditNodeID, "2026-11-19", "2026-11-19", exp); err != nil {
		t.Fatal(err)
	}
	if rows := dailyRowsFor(t, auditNodeID); len(rows) != 1 || rows[0].HitCount != 8 {
		t.Fatalf("node rows after re-import = %+v, want the snapshot's single row with 8 hits", rows)
	}
	nodeOnly, err := svc.GetSummary("2026-11-19", 1, auditNodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodeOnly.Clients) != 1 || nodeOnly.Clients[0].TotalHits != 8 {
		t.Fatalf("node-filtered clients = %+v, want alice with 8 node hits", nodeOnly.Clients)
	}
	unacked, err := svc.GetAlerts("2026-11-19", "alice", true, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(unacked) != 0 {
		t.Fatalf("with only 2 subnets left the sharing alert must clear, got %+v", unacked)
	}
}

// Node rows are scored with the master's rules, and a domain hit on both
// sources yields one alert whose count is the sum, not a running double count.
func TestAccessAuditNodeImportAppliesMasterRules(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	cfg := DefaultAuditConfig()
	cfg.SensitiveDomains = []string{"restricted.example"}
	if err := svc.SaveConfig(cfg, false); err != nil {
		t.Fatal(err)
	}
	ingestAuditLines(t, svc, "2026/11/19 14:00:01.000000 from tcp:198.51.100.5:41001 accepted tcp:sub.restricted.example:443 [in-1 >> direct] email: bob\n")

	ts := time.Date(2026, 11, 19, 14, 30, 0, 0, auditLocation()).Unix()
	exp := nodeExportFor(t,
		[]entity.AuditExportDaily{{Date: "2026-11-19", Email: "bob", RootDomain: "restricted.example", Domain: "sub.restricted.example", Port: 443, HitCount: 4, FirstSeen: ts, LastSeen: ts}},
		nil)
	for range 2 {
		if err := svc.ImportNodeExport(auditNodeID, "2026-11-19", "2026-11-19", exp); err != nil {
			t.Fatal(err)
		}
	}

	rows := dailyRowsFor(t, auditNodeID)
	if len(rows) != 1 || rows[0].Category != AuditCatSensitive || rows[0].RiskLevel != AuditRiskHigh {
		t.Fatalf("node rows = %+v, want sensitive/high from the master's rule", rows)
	}
	alerts, err := svc.GetAlerts("2026-11-19", "bob", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].HitCount != 5 || alerts[0].SrcIP != "198.51.100.5" {
		t.Fatalf("alerts = %+v, want one sensitive alert counting 1 local + 4 node hits and keeping the local source IP", alerts)
	}
}

func TestAccessAuditNodeImportRejectsTimezoneMismatch(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	other := "Etc/GMT+12"
	if auditLocation().String() == other {
		other = "Etc/GMT-12"
	}
	loc, err := time.LoadLocation(other)
	if err != nil {
		t.Fatal(err)
	}
	exp := newAuditExportHeader(loc, "2026-11-19", "2026-11-19")
	exp.Daily = []entity.AuditExportDaily{{Date: "2026-11-19", Email: "alice", RootDomain: "github.com", Domain: "github.com", Port: 443, HitCount: 1}}

	err = svc.ImportNodeExport(auditNodeID, "2026-11-19", "2026-11-19", exp)
	if !errors.Is(err, ErrAuditTimezoneMismatch) {
		t.Fatalf("ImportNodeExport error = %v, want ErrAuditTimezoneMismatch", err)
	}
	if rows := dailyRowsFor(t, auditNodeID); len(rows) != 0 {
		t.Fatalf("mismatched export must not be stored, got %+v", rows)
	}
}

// A panel exports only what its own Xray saw, so a node that is itself a master
// never re-exports rows it imported from further down.
func TestAccessAuditExportLocalOnlyWithinWindow(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	ingestAuditLines(t, svc, ""+
		"2026/11/18 10:00:00.000000 from tcp:198.51.100.1:50001 accepted tcp:github.com:443 [in-1 >> direct] email: alice\n"+
		"2026/11/19 10:00:00.000000 from tcp:198.51.100.1:50002 accepted tcp:github.com:443 [in-1 >> direct] email: alice\n")
	if err := svc.ImportNodeExport(auditNodeID, "2026-11-19", "2026-11-19", nodeExportFor(t,
		[]entity.AuditExportDaily{{Date: "2026-11-19", Email: "carol", RootDomain: "x.com", Domain: "x.com", Port: 443, HitCount: 1}}, nil)); err != nil {
		t.Fatal(err)
	}

	exp, err := svc.ExportLocal("2026-11-19", "2026-11-19")
	if err != nil {
		t.Fatal(err)
	}
	if exp.TimeLocation != auditLocation().String() {
		t.Fatalf("export timeLocation = %q, want %q", exp.TimeLocation, auditLocation().String())
	}
	if len(exp.Daily) != 1 || exp.Daily[0].Email != "alice" || exp.Daily[0].Date != "2026-11-19" {
		t.Fatalf("export daily = %+v, want only alice's local 2026-11-19 row", exp.Daily)
	}
	if len(exp.IPs) != 1 || exp.IPs[0].SrcIP != "198.51.100.1" {
		t.Fatalf("export ips = %+v, want alice's one local source", exp.IPs)
	}
}

// Tables created before node sources existed carry a unique key without
// node_id, which would reject a node row for a domain the master also saw.
func TestAccessAuditLegacyUniqueKeyIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"CREATE TABLE access_audit_dailies (id integer PRIMARY KEY AUTOINCREMENT, date text NOT NULL, email text NOT NULL, root_domain text NOT NULL, domain text NOT NULL, port integer NOT NULL, outbound text, category text, risk_level integer NOT NULL DEFAULT 0, rule_hit text, hit_count integer NOT NULL DEFAULT 0, first_seen integer NOT NULL DEFAULT 0, last_seen integer NOT NULL DEFAULT 0)",
		"CREATE UNIQUE INDEX idx_audit_daily_key ON access_audit_dailies(date, email, domain, port)",
		"CREATE TABLE access_audit_client_ips (id integer PRIMARY KEY AUTOINCREMENT, date text NOT NULL, email text NOT NULL, src_ip text NOT NULL, subnet text NOT NULL, hit_count integer NOT NULL DEFAULT 0, first_seen integer NOT NULL DEFAULT 0, last_seen integer NOT NULL DEFAULT 0, active_hours integer NOT NULL DEFAULT 0)",
		"CREATE UNIQUE INDEX idx_audit_ip_key ON access_audit_client_ips(date, email, src_ip)",
		"INSERT INTO access_audit_dailies (date, email, root_domain, domain, port, hit_count) VALUES ('2026-11-19', 'alice', 'github.com', 'github.com', 443, 2)",
	} {
		if err := legacy.Exec(stmt).Error; err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	sqlDB, _ := legacy.DB()
	_ = sqlDB.Close()

	if err := database.InitDB(path); err != nil {
		t.Fatalf("InitDB on legacy audit schema: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	if err := db.Create(&model.AccessAuditDaily{NodeId: auditNodeID, Date: "2026-11-19", Email: "alice", RootDomain: "github.com", Domain: "github.com", Port: 443, HitCount: 1}).Error; err != nil {
		t.Fatalf("node row next to the legacy local row: %v", err)
	}
	if err := db.Create(&model.AccessAuditClientIP{NodeId: auditNodeID, Date: "2026-11-19", Email: "alice", SrcIP: "198.51.100.1", Subnet: "198.51.100.0/24"}).Error; err != nil {
		t.Fatalf("node ip row: %v", err)
	}
	if err := db.Create(&model.AccessAuditClientIP{Date: "2026-11-19", Email: "alice", SrcIP: "198.51.100.1", Subnet: "198.51.100.0/24"}).Error; err != nil {
		t.Fatalf("local ip row with the same source: %v", err)
	}
	var local model.AccessAuditDaily
	if err := db.Where("node_id = 0").First(&local).Error; err != nil || local.HitCount != 2 {
		t.Fatalf("legacy row = %+v (%v), want it kept as a local row with 2 hits", local, err)
	}
}
