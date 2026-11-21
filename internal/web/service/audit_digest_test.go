package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/web/entity"
	"github.com/TingChieh/FluxPanel/internal/web/locale"
)

func initDigestLocale(t *testing.T) {
	t.Helper()
	if err := locale.InitLocalizer(os.DirFS("..")); err != nil {
		t.Fatalf("InitLocalizer: %v", err)
	}
}

func TestAuditDigestSummarizesSourcesAlertsAndClients(t *testing.T) {
	initAuditTestDB(t)
	initDigestLocale(t)
	svc := &AccessAuditService{}
	if err := database.GetDB().Create(&model.Node{Id: auditNodeID, Name: "hk-edge", Address: "198.51.100.20", Port: 2053, ApiToken: "t", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	ingestAuditLines(t, svc, ""+
		"2026/11/19 15:01:00.000000 from tcp:198.51.100.1:50001 accepted tcp:github.com:443 [in-1 >> direct] email: a<b\n"+
		"2026/11/19 15:02:00.000000 from tcp:198.51.100.1:50002 accepted tcp:api.github.com:443 [in-1 >> direct] email: a<b\n"+
		"2026/11/19 15:03:00.000000 from tcp:198.51.100.1:50003 accepted tcp:github.com:443 [in-1 >> direct] email: a<b\n"+
		"2026/11/19 15:11:00.000000 from tcp:198.51.100.1:50004 accepted tcp:github.com:443 [in-1 >> direct] email: a<b\n"+
		"2026/11/19 15:12:00.000000 from tcp:198.51.100.1:50005 accepted tcp:github.com:443 [in-1 >> direct] email: a<b\n")
	ts1 := time.Date(2026, 11, 19, 15, 2, 0, 0, auditLocation()).Unix()
	ts2 := time.Date(2026, 11, 19, 15, 12, 0, 0, auditLocation()).Unix()
	var slots auditSlotMask
	slots.set(15*auditSlotsPerHr + 0)
	slots.set(15*auditSlotsPerHr + 1)
	if err := svc.ImportNodeExport(auditNodeID, "2026-11-19", "2026-11-19", nodeExportFor(t,
		[]entity.AuditExportDaily{{Date: "2026-11-19", Email: "a<b", RootDomain: "youtube.com", Domain: "youtube.com", Port: 443, HitCount: 7, FirstSeen: ts1, LastSeen: ts2}},
		[]entity.AuditExportIP{
			{Date: "2026-11-19", Email: "a<b", SrcIP: "203.0.113.2", Subnet: "203.0.113.0/24", HitCount: 5, FirstSeen: ts1, LastSeen: ts2, ActiveHours: 1 << 15, ActiveSlots: slots.encode()},
			{Date: "2026-11-19", Email: "a<b", SrcIP: "192.0.2.3", Subnet: "192.0.2.0/24", HitCount: 5, FirstSeen: ts1, LastSeen: ts2, ActiveHours: 1 << 15, ActiveSlots: slots.encode()},
		})); err != nil {
		t.Fatal(err)
	}

	text, err := svc.BuildDigest("2026-11-19", "zh-CN")
	if err != nil {
		t.Fatalf("BuildDigest: %v", err)
	}
	for _, want := range []string{
		"访问审计日报 2026-11-19",
		"活跃用户 1 · 请求 12",
		"本机 5",
		"hk-edge 7",
		"高危 1",
		"a&lt;b",
		"并发 3 个独立网段",
		"youtube.com, github.com",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("digest lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "a<b") {
		t.Fatalf("digest leaves a raw '<' from a client name, breaking Telegram HTML:\n%s", text)
	}
}

func TestAuditDigestScheduleSendsOncePerDayAndBacksOffOnFailure(t *testing.T) {
	initAuditTestDB(t)
	initDigestLocale(t)
	ft := startFakeTelegram(t)
	configureTestTelegram(t)
	svc := &AccessAuditService{}
	if err := svc.SaveDigestConfig(AuditDigestConfigUpdate{Enabled: true, SendAt: "09:00", Lang: "zh-CN"}); err != nil {
		t.Fatalf("SaveDigestConfig: %v", err)
	}
	loc := auditLocation()
	at := func(h, m int) time.Time { return time.Date(2026, 11, 20, h, m, 0, 0, loc) }
	ctx := context.Background()

	svc.RunDigestSchedule(ctx, at(8, 59))
	if n := len(ft.texts()); n != 0 {
		t.Fatalf("before 09:00 sent %d message(s), want none", n)
	}

	ft.fail = "Bad Request: chat not found"
	svc.RunDigestSchedule(ctx, at(9, 0))
	cfg := svc.GetDigestConfig()
	if cfg.LastSentDate != "" || !strings.Contains(cfg.LastError, "chat not found") {
		t.Fatalf("after a failed send cfg = %+v, want no sent date and the Telegram error", cfg)
	}
	ft.fail = ""
	svc.RunDigestSchedule(ctx, at(9, 5))
	if n := len(ft.texts()); n != 0 {
		t.Fatalf("retried %d time(s) within 10 minutes of a failure, want none", n)
	}

	svc.RunDigestSchedule(ctx, at(9, 11))
	texts := ft.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "2026-11-19") {
		t.Fatalf("after backoff sent %v, want one digest for the previous day 2026-11-19", texts)
	}
	if cfg := svc.GetDigestConfig(); cfg.LastSentDate != "2026-11-20" || cfg.LastError != "" {
		t.Fatalf("after success cfg = %+v, want sent date 2026-11-20 and no error", cfg)
	}
	svc.RunDigestSchedule(ctx, at(18, 0))
	if n := len(ft.texts()); n != 1 {
		t.Fatalf("sent %d digests on one day, want exactly 1", n)
	}
}
