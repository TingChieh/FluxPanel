package service

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func sharingLinesFor(email string, ips ...string) string {
	var b strings.Builder
	for _, min := range []int{1, 11} {
		for _, ip := range ips {
			writeBurst(&b, "2026/11/19", 15, min, 3, ip, email)
		}
	}
	return b.String()
}

func alertTargets(t *testing.T, svc *AccessAuditService, email string) []string {
	t.Helper()
	alerts, err := svc.GetAlerts("2026-11-19", email, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(alerts))
	for _, a := range alerts {
		out = append(out, a.Target)
	}
	slices.Sort(out)
	return out
}

func TestAccessAuditTrustedTargetsSuppressRiskAlerts(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	ingestAuditLines(t, svc, ""+
		"2026/11/19 10:00:01.000000 from tcp:198.51.100.1:5001 accepted tcp:ali.example.com:22 [in-1 >> direct] email: ops\n"+
		"2026/11/19 10:00:02.000000 from tcp:198.51.100.1:5002 accepted tcp:ali.example.com:3389 [in-1 >> direct] email: ops\n"+
		"2026/11/19 10:00:03.000000 from tcp:198.51.100.1:5003 accepted tcp:203.0.113.7:22 [in-1 >> direct] email: ops\n"+
		"2026/11/19 10:00:04.000000 from tcp:198.51.100.1:5004 accepted tcp:desktop-lan:445 [in-1 >> direct] email: ops\n")
	if got := alertTargets(t, svc, "ops"); len(got) != 4 {
		t.Fatalf("before trust alerts = %v, want 4 high-risk-port alerts", got)
	}
	if err := svc.AckAllAlerts(); err != nil {
		t.Fatal(err)
	}

	cfg := svc.GetConfig()
	cfg.TrustedTargets = []string{"example.com:22", "203.0.113.0/24:22", "desktop-lan"}
	if err := svc.SaveConfig(cfg, true); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if got := alertTargets(t, svc, "ops"); !slices.Equal(got, []string{"ali.example.com:3389"}) {
		t.Fatalf("after trust alerts = %v, want only the untrusted port ali.example.com:3389 (acknowledged ones included)", got)
	}
}

func TestAccessAuditTrustedClientSkipsSharing(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	ingestAuditLines(t, svc, sharingLinesFor("Roamer", "198.51.100.1", "203.0.113.2", "192.0.2.3"))
	if got := alertTargets(t, svc, "Roamer"); len(got) == 0 {
		t.Fatal("precondition: three concurrent networks must raise a sharing alert")
	}

	cfg := svc.GetConfig()
	cfg.TrustedClients = []string{"roamer"}
	if err := svc.SaveConfig(cfg, true); err != nil {
		t.Fatal(err)
	}
	if got := alertTargets(t, svc, "Roamer"); len(got) != 0 {
		t.Fatalf("trusted client still has alerts %v", got)
	}

	ingestAuditLines(t, svc, sharingLinesFor("Roamer", "93.184.216.4"))
	if got := alertTargets(t, svc, "Roamer"); len(got) != 0 {
		t.Fatalf("a new network re-raised sharing for a trusted client: %v", got)
	}
	if got := svc.GetConfig().TrustedClients; !slices.Equal(got, []string{"roamer"}) {
		t.Fatalf("trusted clients = %v, want the entry kept as typed", got)
	}
}

func TestAccessAuditTrustFromAlert(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	ingestAuditLines(t, svc, ""+
		"2026/11/19 10:00:01.000000 from tcp:198.51.100.1:5001 accepted tcp:box.example.net:22 [in-1 >> direct] email: ops\n"+
		"2026/11/19 10:00:02.000000 from tcp:198.51.100.1:5002 accepted tcp:box.example.net:3389 [in-1 >> direct] email: ops\n"+
		"2026/11/19 10:00:03.000000 from tcp:198.51.100.1:5003 accepted tcp:[2001:db8::7]:22 [in-1 >> direct] email: ops\n"+
		sharingLinesFor("sharer", "198.51.100.1", "203.0.113.2", "192.0.2.3"))

	find := func(email, target string) uint {
		t.Helper()
		alerts, err := svc.GetAlerts("2026-11-19", email, false, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range alerts {
			if a.Target == target {
				return a.Id
			}
		}
		t.Fatalf("no alert %q for %s among %+v", target, email, alerts)
		return 0
	}

	if err := svc.TrustAlert(find("ops", "2001:db8::7:22"), AuditTrustTarget); err != nil {
		t.Fatalf("TrustAlert target ipv6: %v", err)
	}
	if err := svc.TrustAlert(find("ops", "box.example.net:22"), AuditTrustHost); err != nil {
		t.Fatalf("TrustAlert host: %v", err)
	}
	if err := svc.TrustAlert(find("sharer", "concurrent subnets"), AuditTrustClient); err != nil {
		t.Fatalf("TrustAlert client: %v", err)
	}

	cfg := svc.GetConfig()
	if !slices.Equal(cfg.TrustedTargets, []string{"[2001:db8::7]:22", "box.example.net"}) {
		t.Fatalf("trusted targets = %v, want the exact ipv6 target and the whole host", cfg.TrustedTargets)
	}
	if !slices.Equal(cfg.TrustedClients, []string{"sharer"}) {
		t.Fatalf("trusted clients = %v, want sharer", cfg.TrustedClients)
	}
	if got := alertTargets(t, svc, "ops"); len(got) != 0 {
		t.Fatalf("ops alerts after trusting = %v, want none (host trust covers port 3389 too)", got)
	}
	if got := alertTargets(t, svc, "sharer"); len(got) != 0 {
		t.Fatalf("sharer alerts after trusting = %v, want none", got)
	}
}

// A sharing alert has no destination, so trusting its "host" must be refused
// rather than writing the label "concurrent subnets" as a trusted target.
func TestAccessAuditTrustRejectsScopeThatDoesNotFitTheAlert(t *testing.T) {
	initAuditTestDB(t)
	svc := &AccessAuditService{}
	ingestAuditLines(t, svc, sharingLinesFor("sharer", "198.51.100.1", "203.0.113.2", "192.0.2.3"))
	alerts, err := svc.GetAlerts("2026-11-19", "sharer", false, 50)
	if err != nil || len(alerts) == 0 {
		t.Fatalf("alerts = %+v (%v), want a sharing alert", alerts, err)
	}
	if err := svc.TrustAlert(alerts[0].Id, AuditTrustHost); !errors.Is(err, ErrAuditTrustScope) {
		t.Fatalf("TrustAlert(sharing, host) error = %v, want ErrAuditTrustScope", err)
	}
	if got := svc.GetConfig().TrustedTargets; len(got) != 0 {
		t.Fatalf("trusted targets = %v, want nothing written", got)
	}
}
