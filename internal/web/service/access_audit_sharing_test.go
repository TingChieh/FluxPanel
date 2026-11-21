package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

func auditLogLine(date, hhmmss, srcIP, dst, email string) string {
	hostPort := srcIP + ":50001"
	if strings.Contains(srcIP, ":") {
		hostPort = "[" + srcIP + "]:50001"
	}
	return fmt.Sprintf("%s %s.000000 from tcp:%s accepted tcp:%s:443 [in-1 >> direct] email: %s\n",
		date, hhmmss, hostPort, dst, email)
}

func writeBurst(b *strings.Builder, date string, hour, minute, count int, srcIP, email string) {
	for i := range count {
		ts := fmt.Sprintf("%02d:%02d:%02d", hour, minute, i%60)
		b.WriteString(auditLogLine(date, ts, srcIP, "github.com", email))
	}
}

func sharingAlertsFor(t *testing.T, svc *AccessAuditService, date, email string) []string {
	t.Helper()
	alerts, err := svc.GetAlerts(date, email, false, 50)
	if err != nil {
		t.Fatalf("GetAlerts(%s, %s): %v", date, email, err)
	}
	var out []string
	for _, a := range alerts {
		if a.AlertType == AuditAlertAccountSharing {
			out = append(out, fmt.Sprintf("%s:%d:%s", a.RuleName, a.HitCount, a.Detail))
		}
	}
	return out
}

// Normal single-user mobility, CGNAT/IPv6 rotation, dual-stack, private relays,
// low-hit keepalives, and brief single-window handoffs must not raise sharing alerts.
func TestAccessAuditSharingFalsePositiveScenarios(t *testing.T) {
	const logDate = "2026/11/19"
	const isoDate = "2026-11-19"

	t.Run("round-trip commute in same hour does not trigger concurrent sharing", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		writeBurst(&b, logDate, 8, 1, 6, "180.168.1.10", "commuter")
		writeBurst(&b, logDate, 8, 12, 6, "223.104.1.20", "commuter")
		writeBurst(&b, logDate, 8, 25, 6, "112.64.1.30", "commuter")
		writeBurst(&b, logDate, 18, 40, 6, "223.104.1.20", "commuter")
		writeBurst(&b, logDate, 19, 10, 6, "180.168.1.10", "commuter")

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "commuter"); len(got) != 0 {
			t.Fatalf("sequential round-trip commute raised sharing alerts: %v", got)
		}
	})

	t.Run("mobile CGNAT /24 rotation within one /16 collapses to one network", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		ips := []string{"223.104.1.1", "223.104.22.2", "223.104.88.3", "223.104.150.4", "223.104.200.5"}
		for _, ip := range ips {
			writeBurst(&b, logDate, 14, 5, 6, ip, "mobile")
			writeBurst(&b, logDate, 14, 15, 6, ip, "mobile")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "mobile"); len(got) != 0 {
			t.Fatalf("same-/16 CGNAT pool raised sharing alerts: %v", got)
		}
	})

	t.Run("IPv6 privacy and /56 prefix rotation within one /48 collapses to one network", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		ips := []string{
			"2409:8a00:1234:1100::1",
			"2409:8a00:1234:2200::2",
			"2409:8a00:1234:3300::3",
			"2409:8a00:1234:4400::4",
		}
		for _, ip := range ips {
			writeBurst(&b, logDate, 14, 5, 6, ip, "v6roamer")
			writeBurst(&b, logDate, 14, 15, 6, ip, "v6roamer")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "v6roamer"); len(got) != 0 {
			t.Fatalf("same-/48 IPv6 rotation raised sharing alerts: %v", got)
		}
	})

	t.Run("dual-stack across two physical networks does not double count to four", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		for _, min := range []int{5, 15, 25} {
			writeBurst(&b, logDate, 15, min, 6, "180.168.1.5", "dualstack")
			writeBurst(&b, logDate, 15, min, 6, "2409:8a00:1111::1", "dualstack")
			writeBurst(&b, logDate, 15, min, 6, "223.104.1.5", "dualstack")
			writeBurst(&b, logDate, 15, min, 6, "2409:8a00:2222::1", "dualstack")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "dualstack"); len(got) != 0 {
			t.Fatalf("dual-stack across 2 networks raised sharing alerts: %v", got)
		}
	})

	t.Run("private loopback link-local ULA and RFC6598 relays are excluded from sharing but kept in detail", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		nonPublic := []string{"127.0.0.1", "10.0.0.5", "172.16.1.5", "192.168.1.5", "100.64.10.5", "fe80::1", "fd00::1"}
		for _, min := range []int{5, 15} {
			for _, ip := range nonPublic {
				writeBurst(&b, logDate, 15, min, 6, ip, "relayed")
			}
			writeBurst(&b, logDate, 15, min, 6, "198.51.100.1", "relayed")
			writeBurst(&b, logDate, 15, min, 6, "203.0.113.2", "relayed")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "relayed"); len(got) != 0 {
			t.Fatalf("non-public relay IPs inflated sharing count: %v", got)
		}
		detail, err := svc.GetClientDetail("relayed", isoDate, 1, false, AuditAllSources)
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.SourceIPs) != len(nonPublic)+2 {
			t.Fatalf("detail retained %d source IPs, want all %d preserved for audit visibility", len(detail.SourceIPs), len(nonPublic)+2)
		}
	})

	t.Run("low-hit transient reconnect on a third network does not trigger sharing", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		for _, min := range []int{5, 15} {
			writeBurst(&b, logDate, 15, min, 6, "198.51.100.1", "keepalive")
			writeBurst(&b, logDate, 15, min, 6, "203.0.113.2", "keepalive")
			writeBurst(&b, logDate, 15, min, 1, "192.0.2.3", "keepalive")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "keepalive"); len(got) != 0 {
			t.Fatalf("low-hit transient third network raised sharing alerts: %v", got)
		}
	})

	t.Run("brief single 10-minute handoff overlap does not trigger concurrent sharing", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		writeBurst(&b, logDate, 15, 12, 6, "198.51.100.1", "handoff")
		writeBurst(&b, logDate, 15, 13, 6, "203.0.113.2", "handoff")
		writeBurst(&b, logDate, 15, 14, 6, "192.0.2.3", "handoff")

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "handoff"); len(got) != 0 {
			t.Fatalf("single 10m window handoff raised sharing alerts: %v", got)
		}
	})

	t.Run("five distinct networks sequentially across the day stays below daily threshold", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		ips := []string{"198.51.100.1", "203.0.113.2", "192.0.2.3", "93.184.216.4", "151.101.1.5"}
		for i, ip := range ips {
			writeBurst(&b, logDate, 9+i*2, 10, 6, ip, "traveler")
		}

		logPath := filepath.Join(t.TempDir(), "access.log")
		if err := os.WriteFile(logPath, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		svc := &AccessAuditService{}
		if _, err := svc.IngestFromPath(logPath); err != nil {
			t.Fatal(err)
		}
		if got := sharingAlertsFor(t, svc, isoDate, "traveler"); len(got) != 0 {
			t.Fatalf("5 sequential daily networks raised sharing alerts: %v", got)
		}
	})
}

func TestAccessAuditSharingTruePositivesAndBoundaries(t *testing.T) {
	const logDate = "2026/11/19"
	const isoDate = "2026-11-19"

	t.Run("three concurrent public IPv4 networks across two 10m windows triggers high alert", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		for _, min := range []int{2, 12} {
			writeBurst(&b, logDate, 20, min, 3, "198.51.100.1", "sharer-v4")
			writeBurst(&b, logDate, 20, min, 3, "203.0.113.2", "sharer-v4")
			writeBurst(&b, logDate, 20, min, 3, "192.0.2.3", "sharer-v4")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		alerts, err := svc.GetAlerts(isoDate, "sharer-v4", false, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(alerts) != 1 || alerts[0].RuleName != auditRuleConcurrentSubnets || alerts[0].Severity != AuditRiskHigh || alerts[0].HitCount != 3 {
			t.Fatalf("alerts = %+v, want 1 high concurrent-subnets alert with HitCount=3", alerts)
		}
		if !strings.Contains(alerts[0].Detail, "20:00-20:09") || !strings.Contains(alerts[0].Detail, "across 2 10m window(s)") {
			t.Fatalf("alert detail lacks window evidence: %q", alerts[0].Detail)
		}
		summary, err := svc.GetSummary(isoDate, 1, AuditAllSources)
		if err != nil {
			t.Fatal(err)
		}
		if summary.SharingClients != 1 || len(summary.Clients) != 1 || !summary.Clients[0].SharingAlert {
			t.Fatalf("summary = %+v, want SharingClients=1 and client SharingAlert=true", summary)
		}
	})

	t.Run("three concurrent public IPv6 /48 networks across two 10m windows triggers high alert", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		for _, min := range []int{5, 15} {
			writeBurst(&b, logDate, 21, min, 3, "2409:8a00:1111::1", "sharer-v6")
			writeBurst(&b, logDate, 21, min, 3, "2409:8a00:2222::2", "sharer-v6")
			writeBurst(&b, logDate, 21, min, 3, "2409:8a00:3333::3", "sharer-v6")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		alerts, err := svc.GetAlerts(isoDate, "sharer-v6", false, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(alerts) != 1 || alerts[0].RuleName != auditRuleConcurrentSubnets || alerts[0].Severity != AuditRiskHigh {
			t.Fatalf("ipv6 alerts = %+v, want 1 high concurrent-subnets alert", alerts)
		}
	})

	t.Run("eight sequential daily public networks triggers warn and maxDailySubnets=0 disables it", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		ips := []string{
			"198.51.100.1", "203.0.113.2", "192.0.2.3", "93.184.216.4",
			"151.101.1.5", "104.16.1.6", "140.82.1.7", "172.64.1.8",
		}
		for i, ip := range ips {
			writeBurst(&b, logDate, 8+i, 5, 5, ip, "heavy-roamer")
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		alerts, err := svc.GetAlerts(isoDate, "heavy-roamer", false, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(alerts) != 1 || alerts[0].RuleName != auditRuleDailySubnets || alerts[0].Severity != AuditRiskWarn || alerts[0].HitCount != 8 {
			t.Fatalf("daily alerts = %+v, want 1 warn daily-subnets alert with HitCount=8", alerts)
		}

		cfg := svc.GetConfig()
		cfg.MaxDailySubnets = 0
		if err := svc.SaveConfig(cfg, true); err != nil {
			t.Fatal(err)
		}
		if got := svc.GetConfig().MaxDailySubnets; got != 0 {
			t.Fatalf("MaxDailySubnets = %d after saving 0, want 0 (disabled)", got)
		}
		if got := sharingAlertsFor(t, svc, isoDate, "heavy-roamer"); len(got) != 0 {
			t.Fatalf("MaxDailySubnets=0 must clear daily-subnets alert, got %v", got)
		}
	})

	t.Run("ten-minute window boundary separates 15:09:59 from 15:10:00", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		for i := range 5 {
			b.WriteString(auditLogLine(logDate, fmt.Sprintf("15:09:5%d", i), "198.51.100.1", "github.com", "boundary"))
			b.WriteString(auditLogLine(logDate, fmt.Sprintf("15:10:0%d", i), "203.0.113.2", "github.com", "boundary"))
			b.WriteString(auditLogLine(logDate, fmt.Sprintf("15:20:0%d", i), "192.0.2.3", "github.com", "boundary"))
		}

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		summary, err := svc.GetSummary(isoDate, 1, AuditAllSources)
		if err != nil {
			t.Fatal(err)
		}
		if len(summary.Clients) != 1 || summary.Clients[0].MaxConcurrentSubnets != 1 || summary.Clients[0].SharingAlert {
			t.Fatalf("boundary summary = %+v, want MaxConcurrentSubnets=1 and SharingAlert=false", summary.Clients)
		}
	})

	t.Run("config thresholds and reevaluate dynamically toggle alerts and trusted client clears SharingAlert", func(t *testing.T) {
		initAuditTestDB(t)
		var b strings.Builder
		writeBurst(&b, logDate, 15, 12, 6, "198.51.100.1", "tunable")
		writeBurst(&b, logDate, 15, 13, 6, "203.0.113.2", "tunable")
		writeBurst(&b, logDate, 15, 14, 6, "192.0.2.3", "tunable")

		svc := &AccessAuditService{}
		ingestAuditLines(t, svc, b.String())
		if got := sharingAlertsFor(t, svc, isoDate, "tunable"); len(got) != 0 {
			t.Fatalf("precondition: single window must not alert under default MinConcurrentWindows=2, got %v", got)
		}

		cfg := svc.GetConfig()
		cfg.MinConcurrentWindows = 1
		if err := svc.SaveConfig(cfg, true); err != nil {
			t.Fatal(err)
		}
		if got := sharingAlertsFor(t, svc, isoDate, "tunable"); len(got) != 1 {
			t.Fatalf("lowering MinConcurrentWindows=1 must trigger alert on reevaluate, got %v", got)
		}

		cfg.TrustedClients = []string{"tunable"}
		if err := svc.SaveConfig(cfg, true); err != nil {
			t.Fatal(err)
		}
		summary, err := svc.GetSummary(isoDate, 1, AuditAllSources)
		if err != nil {
			t.Fatal(err)
		}
		if summary.SharingClients != 0 || len(summary.Clients) != 1 || summary.Clients[0].SharingAlert {
			t.Fatalf("trusted client summary = %+v, want SharingClients=0 and SharingAlert=false", summary)
		}
	})
}

func TestAccessAuditLegacySlotUpgradeAndDSTFallback(t *testing.T) {
	t.Run("first post-upgrade hit preserves pre-upgrade legacy activity in ActiveSlots", func(t *testing.T) {
		initAuditTestDB(t)
		svc := &AccessAuditService{}
		loc := auditLocation()
		ts1 := time.Date(2026, 11, 19, 10, 2, 0, 0, loc).Unix()
		ts2 := time.Date(2026, 11, 19, 10, 15, 0, 0, loc).Unix()
		var slots auditSlotMask
		slots.set(10*auditSlotsPerHr + 0)
		slots.set(10*auditSlotsPerHr + 1)

		// Seed a pre-upgrade local row without ActiveSlots plus two other concurrent networks.
		db := database.GetDB()
		for _, row := range []model.AccessAuditClientIP{
			{Date: "2026-11-19", Email: "upgraded", SrcIP: "198.51.100.1", Subnet: "198.51.100.0/24", HitCount: 5, FirstSeen: ts1, LastSeen: ts2, ActiveHours: 1 << 10, ActiveSlots: ""},
			{Date: "2026-11-19", Email: "upgraded", SrcIP: "203.0.113.2", Subnet: "203.0.113.0/24", HitCount: 5, FirstSeen: ts1, LastSeen: ts2, ActiveHours: 1 << 10, ActiveSlots: slots.encode()},
			{Date: "2026-11-19", Email: "upgraded", SrcIP: "192.0.2.3", Subnet: "192.0.2.0/24", HitCount: 5, FirstSeen: ts1, LastSeen: ts2, ActiveHours: 1 << 10, ActiveSlots: slots.encode()},
		} {
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
		}

		// Ingesting a new hit at noon must materialize the 10:00-10:19 fallback slots before merging.
		ingestAuditLines(t, svc, auditLogLine("2026/11/19", "12:05:00", "198.51.100.1", "github.com", "upgraded"))
		alerts := sharingAlertsFor(t, svc, "2026-11-19", "upgraded")
		if len(alerts) != 1 || !strings.Contains(alerts[0], "10:00-10:09") {
			t.Fatalf("post-upgrade ingest lost pre-upgrade sharing evidence: alerts = %v", alerts)
		}
	})

	t.Run("legacy fallback uses local wall-clock hours on DST transition days", func(t *testing.T) {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skipf("America/New_York tzdata unavailable: %v", err)
		}
		// 2026-11-01 is a 25-hour fall-back DST day in America/New_York.
		t1 := time.Date(2026, 11, 1, 15, 2, 0, 0, loc).Unix()
		t2 := time.Date(2026, 11, 1, 15, 12, 0, 0, loc).Unix()
		row := model.AccessAuditClientIP{
			Date:        "2026-11-01",
			Email:       "dst-user",
			SrcIP:       "198.51.100.1",
			Subnet:      "198.51.100.0/24",
			HitCount:    5,
			FirstSeen:   t1,
			LastSeen:    t2,
			ActiveHours: 1 << 15,
		}
		mask := slotsForClientIPRow(row, loc)
		if !mask.has(15*auditSlotsPerHr+0) || !mask.has(15*auditSlotsPerHr+1) {
			t.Fatalf("fall-back DST fallback mask missed 15:00/15:10 slots: encoded=%q", mask.encode())
		}

		// 2026-03-08 is a 23-hour spring-forward DST day in America/New_York.
		s1 := time.Date(2026, 3, 8, 15, 2, 0, 0, loc).Unix()
		s2 := time.Date(2026, 3, 8, 15, 12, 0, 0, loc).Unix()
		springRow := model.AccessAuditClientIP{
			Date:        "2026-03-08",
			Email:       "dst-user",
			SrcIP:       "198.51.100.1",
			Subnet:      "198.51.100.0/24",
			HitCount:    5,
			FirstSeen:   s1,
			LastSeen:    s2,
			ActiveHours: 1 << 15,
		}
		springMask := slotsForClientIPRow(springRow, loc)
		if !springMask.has(15*auditSlotsPerHr+0) || !springMask.has(15*auditSlotsPerHr+1) {
			t.Fatalf("spring-forward DST fallback mask missed 15:00/15:10 slots: encoded=%q", springMask.encode())
		}
	})
}

func TestAccessAuditDSTTransitionHoursAndDisambiguation(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York tzdata unavailable: %v", err)
	}

	t.Run("fall-back repeated 01:xx hour maps both EDT and EST UTC intervals", func(t *testing.T) {
		// 2026-11-01 in America/New_York repeats 01:00-01:59 (EDT -04:00 then EST -05:00).
		edtStart := time.Date(2026, 11, 1, 5, 5, 0, 0, time.UTC).Unix()
		edtEnd := time.Date(2026, 11, 1, 5, 15, 0, 0, time.UTC).Unix()
		estStart := time.Date(2026, 11, 1, 6, 25, 0, 0, time.UTC).Unix()
		estEnd := time.Date(2026, 11, 1, 6, 35, 0, 0, time.UTC).Unix()

		edtRow := model.AccessAuditClientIP{
			Date: "2026-11-01", Email: "dst", SrcIP: "198.51.100.1",
			HitCount: 6, FirstSeen: edtStart, LastSeen: edtEnd, ActiveHours: 1 << 1,
		}
		edtMask := slotsForClientIPRow(edtRow, loc)
		if !edtMask.has(1*auditSlotsPerHr+0) || !edtMask.has(1*auditSlotsPerHr+1) || edtMask.has(1*auditSlotsPerHr+2) {
			t.Fatalf("EDT 01:05-01:15 slots mismatch: encoded=%q", edtMask.encode())
		}

		estRow := model.AccessAuditClientIP{
			Date: "2026-11-01", Email: "dst", SrcIP: "203.0.113.2",
			HitCount: 6, FirstSeen: estStart, LastSeen: estEnd, ActiveHours: 1 << 1,
		}
		estMask := slotsForClientIPRow(estRow, loc)
		if !estMask.has(1*auditSlotsPerHr+2) || !estMask.has(1*auditSlotsPerHr+3) || estMask.has(1*auditSlotsPerHr+0) {
			t.Fatalf("EST 01:25-01:35 slots mismatch: encoded=%q", estMask.encode())
		}

		// formatSlotRange must distinguish the two 01:00 occurrences unambiguously.
		slot0100 := 1 * auditSlotsPerHr
		lblEDT := formatSlotRange("2026-11-01", slot0100, loc, []model.AccessAuditClientIP{edtRow})
		if lblEDT != "01:00-01:09 (EDT -04:00)" {
			t.Fatalf("EDT 01:00 label = %q, want %q", lblEDT, "01:00-01:09 (EDT -04:00)")
		}
		est0100Row := model.AccessAuditClientIP{
			Date: "2026-11-01", FirstSeen: time.Date(2026, 11, 1, 6, 2, 0, 0, time.UTC).Unix(),
			LastSeen: time.Date(2026, 11, 1, 6, 8, 0, 0, time.UTC).Unix(),
		}
		lblEST := formatSlotRange("2026-11-01", slot0100, loc, []model.AccessAuditClientIP{est0100Row})
		if lblEST != "01:00-01:09 (EST -05:00)" {
			t.Fatalf("EST 01:00 label = %q, want %q", lblEST, "01:00-01:09 (EST -05:00)")
		}
		lblBoth := formatSlotRange("2026-11-01", slot0100, loc, []model.AccessAuditClientIP{edtRow, est0100Row})
		if lblBoth != "01:00-01:09 (EDT -04:00 / EST -05:00)" {
			t.Fatalf("both-occurrence 01:00 label = %q, want %q", lblBoth, "01:00-01:09 (EDT -04:00 / EST -05:00)")
		}
		if normal := formatSlotRange("2026-11-01", 15*auditSlotsPerHr, loc, nil); normal != "15:00-15:09" {
			t.Fatalf("non-repeated slot label = %q, want 15:00-15:09", normal)
		}
	})

	t.Run("spring-forward skipped 02:xx hour sets 01:50 and 03:00 without phantom 02:xx slots", func(t *testing.T) {
		// 2026-03-08 06:55Z is 01:55 EST; 07:05Z is 03:05 EDT (02:00-02:59 does not exist).
		startUTC := time.Date(2026, 3, 8, 6, 55, 0, 0, time.UTC).Unix()
		endUTC := time.Date(2026, 3, 8, 7, 5, 0, 0, time.UTC).Unix()
		row := model.AccessAuditClientIP{
			Date: "2026-03-08", Email: "dst", SrcIP: "198.51.100.1",
			HitCount: 6, FirstSeen: startUTC, LastSeen: endUTC,
			ActiveHours: (1 << 1) | (1 << 3),
		}
		mask := slotsForClientIPRow(row, loc)
		if !mask.has(1*auditSlotsPerHr+5) || !mask.has(3*auditSlotsPerHr+0) {
			t.Fatalf("spring-forward transition missed 01:50 or 03:00 slot: encoded=%q", mask.encode())
		}
		for s := 2 * auditSlotsPerHr; s < 3*auditSlotsPerHr; s++ {
			if mask.has(s) {
				t.Fatalf("spring-forward skipped hour 02:xx must not be marked (slot %d)", s)
			}
		}
	})
}

func TestAccessAuditConfigRejectsDailyThresholdOne(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}

	cfg := DefaultAuditConfig()
	cfg.MaxDailySubnets = 1
	err := svc.SaveConfig(cfg, false)
	if !errors.Is(err, ErrInvalidAuditConfig) {
		t.Fatalf("SaveConfig(MaxDailySubnets=1) error = %v, want ErrInvalidAuditConfig", err)
	}
	if got := svc.GetConfig().MaxDailySubnets; got != 8 {
		t.Fatalf("stored MaxDailySubnets = %d after rejected save, want unchanged default 8", got)
	}

	cfg.MaxDailySubnets = 0
	if err := svc.SaveConfig(cfg, false); err != nil {
		t.Fatalf("SaveConfig(MaxDailySubnets=0) failed: %v", err)
	}
	if got := svc.GetConfig().MaxDailySubnets; got != 0 {
		t.Fatalf("MaxDailySubnets = %d, want 0", got)
	}
}

func TestAccessAuditLegacyConcurrentAlertDedupeAndUpgrade(t *testing.T) {
	t.Run("pre-upgrade acked same-hour alert upgrades in place without duplicating", func(t *testing.T) {
		initAuditTestDB(t)
		svc := &AccessAuditService{}
		db := database.GetDB()
		if err := db.Create(&model.AccessAuditAlert{
			CreatedAt: time.Date(2026, 11, 19, 15, 5, 0, 0, auditLocation()).Unix(),
			Date:      "2026-11-19",
			Email:     "legacy-ack",
			AlertType: AuditAlertAccountSharing,
			Severity:  AuditRiskHigh,
			RuleName:  auditRuleConcurrentSubnets,
			Target:    auditTargetLegacyConcurrentSubnets,
			SrcIP:     "198.51.100.1",
			HitCount:  3,
			Detail:    "Hour 15:00 saw 3 distinct subnets concurrently",
			Acked:     true,
		}).Error; err != nil {
			t.Fatal(err)
		}

		var b strings.Builder
		for _, min := range []int{2, 12} {
			writeBurst(&b, "2026/11/19", 15, min, 3, "198.51.100.1", "legacy-ack")
			writeBurst(&b, "2026/11/19", 15, min, 3, "203.0.113.2", "legacy-ack")
			writeBurst(&b, "2026/11/19", 15, min, 3, "192.0.2.3", "legacy-ack")
		}
		ingestAuditLines(t, svc, b.String())

		alerts, err := svc.GetAlerts("2026-11-19", "legacy-ack", false, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(alerts) != 1 || alerts[0].Target != auditTargetConcurrentSubnets || !alerts[0].Acked || alerts[0].HitCount != 3 {
			t.Fatalf("alerts = %+v, want 1 upgraded acked concurrent-subnets alert with HitCount=3", alerts)
		}
	})

	t.Run("read path deduplicates coexisting legacy and current rows while keeping untouched history", func(t *testing.T) {
		initAuditTestDB(t)
		svc := &AccessAuditService{}
		db := database.GetDB()
		ts := time.Date(2026, 11, 19, 15, 10, 0, 0, auditLocation()).Unix()
		rows := []model.AccessAuditAlert{
			{
				CreatedAt: ts - 60, Date: "2026-11-19", Email: "dup",
				AlertType: AuditAlertAccountSharing, Severity: AuditRiskHigh,
				RuleName: auditRuleConcurrentSubnets, Target: auditTargetLegacyConcurrentSubnets,
				HitCount: 3, Acked: true,
			},
			{
				CreatedAt: ts, Date: "2026-11-19", Email: "dup",
				AlertType: AuditAlertAccountSharing, Severity: AuditRiskHigh,
				RuleName: auditRuleConcurrentSubnets, Target: auditTargetConcurrentSubnets,
				HitCount: 3, Acked: false,
			},
			{
				CreatedAt: ts - 86400, Date: "2026-11-18", Email: "dup",
				AlertType: AuditAlertAccountSharing, Severity: AuditRiskHigh,
				RuleName: auditRuleConcurrentSubnets, Target: auditTargetLegacyConcurrentSubnets,
				HitCount: 3, Acked: true,
			},
		}
		for _, r := range rows {
			if err := db.Create(&r).Error; err != nil {
				t.Fatal(err)
			}
		}

		allAlerts, err := svc.GetAlerts("", "dup", false, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(allAlerts) != 2 {
			t.Fatalf("allAlerts = %+v, want 2 (1 current on 11-19 + 1 untouched legacy on 11-18)", allAlerts)
		}
		summary, err := svc.GetSummary("2026-11-19", 1, AuditAllSources)
		if err != nil {
			t.Fatal(err)
		}
		if summary.HighRiskAlerts != 1 || summary.UnackedAlerts != 1 {
			t.Fatalf("summary = %+v, want HighRiskAlerts=1 UnackedAlerts=1 without double-counting legacy row", summary)
		}
		detail, err := svc.GetClientDetail("dup", "2026-11-19", 1, true, AuditAllSources)
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Alerts) != 1 || detail.Alerts[0].Target != auditTargetConcurrentSubnets {
			t.Fatalf("detail.Alerts = %+v, want single current alert", detail.Alerts)
		}
	})
}

func TestAccessAuditSummaryDistinguishesConcurrentAndDailySharingFlags(t *testing.T) {
	initAuditTestDB(t)
	var b strings.Builder
	// daily-only client: 8 sequential networks, peak concurrency = 1.
	dailyIPs := []string{
		"198.51.100.1", "203.0.113.2", "192.0.2.3", "93.184.216.4",
		"151.101.1.5", "104.16.1.6", "140.82.1.7", "172.64.1.8",
	}
	for i, ip := range dailyIPs {
		writeBurst(&b, "2026/11/19", 8+i, 5, 5, ip, "daily-only")
	}
	// concurrent-only client: 3 concurrent networks across 2 slots.
	for _, min := range []int{2, 12} {
		writeBurst(&b, "2026/11/19", 20, min, 3, "198.51.100.1", "concurrent-only")
		writeBurst(&b, "2026/11/19", 20, min, 3, "203.0.113.2", "concurrent-only")
		writeBurst(&b, "2026/11/19", 20, min, 3, "192.0.2.3", "concurrent-only")
	}

	svc := &AccessAuditService{}
	ingestAuditLines(t, svc, b.String())
	summary, err := svc.GetSummary("2026-11-19", 1, AuditAllSources)
	if err != nil {
		t.Fatal(err)
	}
	byEmail := make(map[string]AuditClientSummary, len(summary.Clients))
	for _, c := range summary.Clients {
		byEmail[c.Email] = c
	}
	daily := byEmail["daily-only"]
	if !daily.SharingAlert || daily.ConcurrentSharingAlert || !daily.DailySharingAlert || daily.MaxConcurrentSubnets != 1 {
		t.Fatalf("daily-only summary = %+v, want SharingAlert=true ConcurrentSharingAlert=false DailySharingAlert=true Peak=1", daily)
	}
	conc := byEmail["concurrent-only"]
	if !conc.SharingAlert || !conc.ConcurrentSharingAlert || conc.DailySharingAlert || conc.MaxConcurrentSubnets != 3 {
		t.Fatalf("concurrent-only summary = %+v, want SharingAlert=true ConcurrentSharingAlert=true DailySharingAlert=false Peak=3", conc)
	}
}
