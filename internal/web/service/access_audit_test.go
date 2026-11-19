package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database/dbtest"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

func initAuditTestDB(t *testing.T) {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
}

func TestParseAuditDestinationAndSource(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantHost string
		wantRoot string
		wantPort int
		wantIP   bool
	}{
		{
			name:     "tcp subdomain collapses to eTLD+1",
			raw:      "tcp:rr3---sn-xxx.googlevideo.com:443",
			wantHost: "rr3---sn-xxx.googlevideo.com",
			wantRoot: "googlevideo.com",
			wantPort: 443,
			wantIP:   false,
		},
		{
			name:     "multi-part public suffix preserved",
			raw:      "tcp:news.bbc.co.uk:443",
			wantHost: "news.bbc.co.uk",
			wantRoot: "bbc.co.uk",
			wantPort: 443,
			wantIP:   false,
		},
		{
			name:     "udp ipv4 literal",
			raw:      "udp:1.1.1.1:53",
			wantHost: "1.1.1.1",
			wantRoot: "1.1.1.1",
			wantPort: 53,
			wantIP:   true,
		},
		{
			name:     "tcp ipv6 literal",
			raw:      "tcp:[2001:db8::1]:443",
			wantHost: "2001:db8::1",
			wantRoot: "2001:db8::1",
			wantPort: 443,
			wantIP:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, root, port, isIP := parseAuditDestination(tc.raw)
			if host != tc.wantHost || root != tc.wantRoot || port != tc.wantPort || isIP != tc.wantIP {
				t.Fatalf("parseAuditDestination(%q) = (%q, %q, %d, %v), want (%q, %q, %d, %v)",
					tc.raw, host, root, port, isIP, tc.wantHost, tc.wantRoot, tc.wantPort, tc.wantIP)
			}
		})
	}

	srcIP, subnet := parseAuditSource("tcp:198.51.100.42:54321")
	if srcIP != "198.51.100.42" || subnet != "198.51.100.0/24" {
		t.Fatalf("parseAuditSource ipv4 = (%q, %q), want (198.51.100.42, 198.51.100.0/24)", srcIP, subnet)
	}

	srcIP6, subnet6 := parseAuditSource("udp:[2001:db8:abcd:1234::9]:443")
	if srcIP6 != "2001:db8:abcd:1234::9" || subnet6 != "2001:db8:abcd:1200::/56" {
		t.Fatalf("parseAuditSource ipv6 = (%q, %q), want (2001:db8:abcd:1234::9, 2001:db8:abcd:1200::/56)", srcIP6, subnet6)
	}
}

func TestAccessAuditIncrementalIngestAndTruncationRecovery(t *testing.T) {
	initAuditTestDB(t)

	logPath := filepath.Join(t.TempDir(), "access.log")
	initial := "" +
		"2026/11/19 10:00:01.000000 from tcp:198.51.100.10:50001 accepted tcp:api.github.com:443 [in-1 >> direct] email: alice\n" +
		"2026/11/19 10:00:02.000000 from tcp:198.51.100.10:50002 accepted tcp:api.github.com:443 [in-1 >> direct] email: alice\n" +
		"2026/11/19 10:00:03.000000 from tcp:198.51.100.10:50003 accepted tcp:gateway.icloud.com:443 [in-1 >> direct] email: alice\n"
	if err := os.WriteFile(logPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := &AccessAuditService{}
	res1, err := svc.IngestFromPath(logPath)
	if err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}
	if res1.LinesProcessed != 3 {
		t.Fatalf("first ingest LinesProcessed = %d, want 3", res1.LinesProcessed)
	}

	// Re-running without new lines must not double-count.
	res2, err := svc.IngestFromPath(logPath)
	if err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}
	if res2.LinesProcessed != 0 {
		t.Fatalf("second ingest LinesProcessed = %d, want 0", res2.LinesProcessed)
	}

	// Append one new line and verify incremental read.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("2026/11/19 10:05:00.000000 from tcp:198.51.100.10:50004 accepted tcp:api.github.com:443 [in-1 >> direct] email: alice\n")
	_ = f.Close()

	res3, err := svc.IngestFromPath(logPath)
	if err != nil {
		t.Fatalf("third ingest failed: %v", err)
	}
	if res3.LinesProcessed != 1 {
		t.Fatalf("third ingest LinesProcessed = %d, want 1", res3.LinesProcessed)
	}

	// Simulate ClearLogsJob truncation followed by a fresh line.
	if err := os.WriteFile(logPath, []byte(
		"2026/11/19 11:00:00.000000 from tcp:198.51.100.10:50005 accepted tcp:youtube.com:443 [in-1 >> direct] email: alice\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	res4, err := svc.IngestFromPath(logPath)
	if err != nil {
		t.Fatalf("post-truncate ingest failed: %v", err)
	}
	if res4.LinesProcessed != 1 {
		t.Fatalf("post-truncate ingest LinesProcessed = %d, want 1", res4.LinesProcessed)
	}

	detail, err := svc.GetClientDetail("alice", "2026-11-19", 1, true)
	if err != nil {
		t.Fatalf("GetClientDetail failed: %v", err)
	}
	// Noise domain gateway.icloud.com should be hidden when hideNoise=true.
	if len(detail.Domains) != 2 {
		t.Fatalf("expected 2 non-noise root domains, got %d: %+v", len(detail.Domains), detail.Domains)
	}
	if detail.Domains[0].RootDomain != "github.com" || detail.Domains[0].HitCount != 3 {
		t.Fatalf("top domain = (%s, %d), want (github.com, 3)", detail.Domains[0].RootDomain, detail.Domains[0].HitCount)
	}
}

func TestAccessAuditSensitiveRulesAndReevaluate(t *testing.T) {
	initAuditTestDB(t)

	logPath := filepath.Join(t.TempDir(), "access.log")
	content := "" +
		"2026/11/19 14:00:01.000000 from tcp:198.51.100.5:41001 accepted tcp:sub.restricted.example:443 [in-1 >> direct] email: bob\n" +
		"2026/11/19 14:00:02.000000 from tcp:198.51.100.5:41002 accepted tcp:my-forbidden-news.org:443 [in-1 >> direct] email: bob\n" +
		"2026/11/19 14:00:03.000000 from tcp:198.51.100.5:41003 accepted tcp:github.com:443 [in-1 >> direct] email: bob\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := &AccessAuditService{}
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatalf("initial ingest failed: %v", err)
	}

	alertsBefore, err := svc.GetAlerts("2026-11-19", "bob", false, 50)
	if err != nil {
		t.Fatalf("GetAlerts failed: %v", err)
	}
	if len(alertsBefore) != 0 {
		t.Fatalf("expected 0 alerts before configuring sensitive rules, got %d", len(alertsBefore))
	}

	cfg := DefaultAuditConfig()
	cfg.SensitiveDomains = []string{"restricted.example"}
	cfg.SensitiveKeywords = []string{"forbidden"}
	if err := svc.SaveConfig(cfg, true); err != nil {
		t.Fatalf("SaveConfig with reevaluate failed: %v", err)
	}

	alertsAfter, err := svc.GetAlerts("2026-11-19", "bob", true, 50)
	if err != nil {
		t.Fatalf("GetAlerts after reevaluate failed: %v", err)
	}
	if len(alertsAfter) != 2 {
		t.Fatalf("expected 2 sensitive alerts after reevaluate, got %d: %+v", len(alertsAfter), alertsAfter)
	}
	for _, a := range alertsAfter {
		if a.AlertType != AuditAlertSensitiveDomain || a.Severity != AuditRiskHigh {
			t.Fatalf("unexpected alert: %+v", a)
		}
	}

	if err := svc.AckAlert(alertsAfter[0].Id); err != nil {
		t.Fatalf("AckAlert failed: %v", err)
	}
	unacked, err := svc.GetAlerts("2026-11-19", "bob", true, 50)
	if err != nil {
		t.Fatalf("GetAlerts unacked failed: %v", err)
	}
	if len(unacked) != 1 {
		t.Fatalf("expected 1 unacked alert after AckAlert, got %d", len(unacked))
	}
	if err := svc.AckAllAlerts(); err != nil {
		t.Fatalf("AckAllAlerts failed: %v", err)
	}
	unacked2, err := svc.GetAlerts("2026-11-19", "bob", true, 50)
	if err != nil {
		t.Fatalf("GetAlerts unacked2 failed: %v", err)
	}
	if len(unacked2) != 0 {
		t.Fatalf("expected 0 unacked alerts after AckAllAlerts, got %d", len(unacked2))
	}
}

func TestAccessAuditAccountSharingDetection(t *testing.T) {
	initAuditTestDB(t)

	logPath := filepath.Join(t.TempDir(), "access.log")
	content := "" +
		// charlie: 3 IPs in the SAME /24 in hour 15 -> must NOT trigger sharing alert.
		"2026/11/19 15:01:00.000000 from tcp:198.51.100.11:50001 accepted tcp:github.com:443 [in-1 >> direct] email: charlie\n" +
		"2026/11/19 15:02:00.000000 from tcp:198.51.100.22:50002 accepted tcp:github.com:443 [in-1 >> direct] email: charlie\n" +
		"2026/11/19 15:03:00.000000 from tcp:198.51.100.33:50003 accepted tcp:github.com:443 [in-1 >> direct] email: charlie\n" +
		// dave: 3 DIFFERENT /24 subnets in the SAME hour 15 -> triggers critical sharing alert.
		"2026/11/19 15:01:00.000000 from tcp:198.51.100.1:50001 accepted tcp:youtube.com:443 [in-1 >> direct] email: dave\n" +
		"2026/11/19 15:05:00.000000 from tcp:203.0.113.2:50002 accepted tcp:youtube.com:443 [in-1 >> direct] email: dave\n" +
		"2026/11/19 15:10:00.000000 from tcp:192.0.2.3:50003 accepted tcp:youtube.com:443 [in-1 >> direct] email: dave\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := &AccessAuditService{}
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	charlieAlerts, err := svc.GetAlerts("2026-11-19", "charlie", false, 20)
	if err != nil {
		t.Fatalf("charlie alerts failed: %v", err)
	}
	if len(charlieAlerts) != 0 {
		t.Fatalf("same /24 subnet must not trigger sharing alert, got %+v", charlieAlerts)
	}

	daveAlerts, err := svc.GetAlerts("2026-11-19", "dave", false, 20)
	if err != nil {
		t.Fatalf("dave alerts failed: %v", err)
	}
	if len(daveAlerts) != 1 || daveAlerts[0].AlertType != AuditAlertAccountSharing || daveAlerts[0].Severity != AuditRiskHigh {
		t.Fatalf("expected 1 critical account_sharing alert for dave, got %+v", daveAlerts)
	}

	summary, err := svc.GetSummary("2026-11-19", 1)
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary.ActiveClients != 2 || summary.HighRiskAlerts != 1 {
		t.Fatalf("summary = %+v, want ActiveClients=2 HighRiskAlerts=1", summary)
	}
}

func TestAccessAuditPruneOldData(t *testing.T) {
	initAuditTestDB(t)

	logPath := filepath.Join(t.TempDir(), "access.log")
	oldDate := time.Now().AddDate(0, 0, -45).Format("2006/01/02")
	today := time.Now().Format("2006/01/02")
	todayISO := time.Now().Format("2006-01-02")
	oldISO := time.Now().AddDate(0, 0, -45).Format("2006-01-02")

	content := "" +
		oldDate + " 12:00:00.000000 from tcp:198.51.100.1:50001 accepted tcp:github.com:443 [in-1 >> direct] email: eve\n" +
		today + " 12:00:00.000000 from tcp:198.51.100.1:50002 accepted tcp:github.com:443 [in-1 >> direct] email: eve\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := &AccessAuditService{}
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if err := svc.PruneOldData(30); err != nil {
		t.Fatalf("PruneOldData failed: %v", err)
	}

	oldSummary, err := svc.GetSummary(oldISO, 1)
	if err != nil {
		t.Fatal(err)
	}
	if oldSummary.TotalHits != 0 {
		t.Fatalf("expected old date %s to be pruned, got TotalHits=%d", oldISO, oldSummary.TotalHits)
	}
	todaySummary, err := svc.GetSummary(todayISO, 1)
	if err != nil {
		t.Fatal(err)
	}
	if todaySummary.TotalHits != 1 {
		t.Fatalf("expected today %s to remain, got TotalHits=%d", todayISO, todaySummary.TotalHits)
	}
}

func appendAuditLog(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func TestAccessAuditIngestDefersPartialTrailingLine(t *testing.T) {
	initAuditTestDB(t)

	logPath := filepath.Join(t.TempDir(), "access.log")
	complete := "2026/11/19 10:00:01.000000 from tcp:198.51.100.10:50001 accepted tcp:github.com:443 [in-1 >> direct] email: alice\n"
	// Xray is mid-write: the email field is cut short and there is no trailing newline yet.
	partial := "2026/11/19 10:00:02.000000 from tcp:198.51.100.10:50002 accepted tcp:github.com:443 [in-1 >> direct] email: ali"
	if err := os.WriteFile(logPath, []byte(complete+partial), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := &AccessAuditService{}
	res1, err := svc.IngestFromPath(logPath)
	if err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}
	if res1.LinesProcessed != 1 || res1.BytesRead != int64(len(complete)) {
		t.Fatalf("first ingest = %+v, want 1 line / %d bytes", res1, len(complete))
	}

	appendAuditLog(t, logPath, "ce\n")
	res2, err := svc.IngestFromPath(logPath)
	if err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}
	if res2.LinesProcessed != 1 {
		t.Fatalf("second ingest LinesProcessed = %d, want 1", res2.LinesProcessed)
	}

	summary, err := svc.GetSummary("2026-11-19", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Clients) != 1 || summary.Clients[0].Email != "alice" || summary.Clients[0].TotalHits != 2 {
		t.Fatalf("clients = %+v, want only alice with 2 hits", summary.Clients)
	}
}

func TestAccessAuditSharingAlertEscalatesInPlace(t *testing.T) {
	initAuditTestDB(t)

	logPath := filepath.Join(t.TempDir(), "access.log")
	line := func(minute int, ip string) string {
		return fmt.Sprintf("2026/11/19 15:%02d:00.000000 from tcp:%s:50001 accepted tcp:youtube.com:443 [in-1 >> direct] email: dave\n", minute, ip)
	}
	if err := os.WriteFile(logPath, []byte(line(1, "198.51.100.1")+line(2, "203.0.113.2")+line(3, "192.0.2.3")), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := &AccessAuditService{}
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatal(err)
	}
	first, err := svc.GetAlerts("2026-11-19", "dave", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("want 1 sharing alert after 3 subnets, got %+v", first)
	}
	if err := svc.AckAlert(first[0].Id); err != nil {
		t.Fatal(err)
	}

	// A 4th subnet in the same hour, then the same IP again: one alert, reopened once on escalation.
	appendAuditLog(t, logPath, line(4, "100.64.0.4"))
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatal(err)
	}
	escalated, err := svc.GetAlerts("2026-11-19", "dave", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	var concurrent []model.AccessAuditAlert
	for _, a := range escalated {
		if a.RuleName == "sharing:concurrent-subnets" {
			concurrent = append(concurrent, a)
		}
	}
	if len(concurrent) != 1 || concurrent[0].HitCount != 4 || concurrent[0].Acked {
		t.Fatalf("concurrent alerts = %+v, want one unacked row with HitCount 4", concurrent)
	}
	if err := svc.AckAllAlerts(); err != nil {
		t.Fatal(err)
	}

	appendAuditLog(t, logPath, line(5, "100.64.0.4"))
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatal(err)
	}
	unacked, err := svc.GetAlerts("2026-11-19", "dave", true, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(unacked) != 0 {
		t.Fatalf("no new subnet must keep alerts acknowledged, got %+v", unacked)
	}
}

func TestAccessAuditReevaluateDropsStaleAlertsAndKeepsSource(t *testing.T) {
	initAuditTestDB(t)

	svc := &AccessAuditService{}
	cfg := DefaultAuditConfig()
	cfg.SensitiveDomains = []string{"restricted.example"}
	if err := svc.SaveConfig(cfg, false); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(t.TempDir(), "access.log")
	content := "2026/11/19 14:00:01.000000 from tcp:198.51.100.5:41001 accepted tcp:sub.restricted.example:443 [in-1 >> direct] email: bob\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatal(err)
	}

	if err := svc.SaveConfig(cfg, true); err != nil {
		t.Fatal(err)
	}
	kept, err := svc.GetAlerts("2026-11-19", "bob", true, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].SrcIP != "198.51.100.5" {
		t.Fatalf("re-evaluating unchanged rules = %+v, want one alert keeping SrcIP 198.51.100.5", kept)
	}

	cfg.SensitiveDomains = nil
	if err := svc.SaveConfig(cfg, true); err != nil {
		t.Fatal(err)
	}
	stale, err := svc.GetAlerts("2026-11-19", "bob", true, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("removed rule must drop its unacknowledged alerts, got %+v", stale)
	}
}

func TestAccessAuditBucketsByPanelTimeLocation(t *testing.T) {
	initAuditTestDB(t)

	logged := time.Date(2026, 11, 19, 6, 0, 0, 0, time.Local)
	if _, off := logged.Zone(); off < 0 {
		logged = logged.Add(12 * time.Hour)
	}
	panelZone := "Etc/GMT-12"
	if _, off := logged.Zone(); off >= 0 {
		panelZone = "Etc/GMT+12"
	}
	if err := (&SettingService{}).setString("timeLocation", panelZone); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation(panelZone)
	if err != nil {
		t.Fatal(err)
	}
	inPanel := logged.In(loc)
	wantDate := inPanel.Format("2006-01-02")
	if wantDate == logged.Format("2006-01-02") {
		t.Fatalf("precondition: panel zone %s must move %v onto another day", panelZone, logged)
	}

	logPath := filepath.Join(t.TempDir(), "access.log")
	line := logged.Format("2006/01/02 15:04:05.000000") + " from tcp:198.51.100.10:50001 accepted tcp:github.com:443 [in-1 >> direct] email: alice\n"
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := &AccessAuditService{}
	if _, err := svc.IngestFromPath(logPath); err != nil {
		t.Fatal(err)
	}

	detail, err := svc.GetClientDetail("alice", wantDate, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TotalHits != 1 || len(detail.SourceIPs) != 1 || detail.SourceIPs[0].ActiveHours != 1<<uint(inPanel.Hour()) {
		t.Fatalf("detail for %s = %+v, want 1 hit active at panel hour %d", wantDate, detail, inPanel.Hour())
	}
}
