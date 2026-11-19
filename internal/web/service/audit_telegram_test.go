package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

const testBotToken = "123456:AA-secret-bot-token"

type fakeTelegram struct {
	mu       sync.Mutex
	messages []map[string]any
	fail     string
}

func startFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	ft := &fakeTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot"+testBotToken+"/sendMessage" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var msg map[string]any
		_ = json.Unmarshal(body, &msg)
		ft.mu.Lock()
		defer ft.mu.Unlock()
		if ft.fail != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"` + ft.fail + `"}`))
			return
		}
		ft.messages = append(ft.messages, msg)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	prev := telegramAPIBase
	telegramAPIBase = srv.URL
	t.Cleanup(func() {
		telegramAPIBase = prev
		srv.Close()
	})
	return ft
}

func (ft *fakeTelegram) texts() []string {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]string, 0, len(ft.messages))
	for _, m := range ft.messages {
		s, _ := m["text"].(string)
		out = append(out, s)
	}
	return out
}

func configureTestTelegram(t *testing.T) {
	t.Helper()
	if err := (&AccessAuditService{}).SaveTelegramConfig(AuditTelegramConfigUpdate{Enabled: true, ChatID: "4242", BotToken: testBotToken}); err != nil {
		t.Fatalf("SaveTelegramConfig: %v", err)
	}
}

func TestAuditTelegramSendsAndSplitsLongMessages(t *testing.T) {
	initAuditTestDB(t)
	ft := startFakeTelegram(t)
	configureTestTelegram(t)

	var b strings.Builder
	for i := range 400 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 20))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString("\n")
	}
	long := b.String()
	if err := (&AccessAuditService{}).SendTelegram(context.Background(), long); err != nil {
		t.Fatalf("SendTelegram: %v", err)
	}

	texts := ft.texts()
	if len(texts) < 3 {
		t.Fatalf("a %d-char message went out in %d part(s), want it split", len(long), len(texts))
	}
	for i, txt := range texts {
		if len([]rune(txt)) > telegramMaxMessageRunes {
			t.Fatalf("part %d has %d runes, over Telegram's %d", i, len([]rune(txt)), telegramMaxMessageRunes)
		}
	}
	if strings.Join(texts, "\n") != strings.TrimRight(long, "\n") {
		t.Fatal("splitting lost or reordered lines")
	}
	ft.mu.Lock()
	first := ft.messages[0]
	ft.mu.Unlock()
	if first["chat_id"] != "4242" || first["parse_mode"] != "HTML" {
		t.Fatalf("request = %+v, want chat_id 4242 and HTML parse mode", first)
	}
}

func TestAuditTelegramErrorsNeverCarryTheToken(t *testing.T) {
	initAuditTestDB(t)
	ft := startFakeTelegram(t)
	configureTestTelegram(t)
	ft.fail = "Bad Request: chat not found"

	err := (&AccessAuditService{}).SendTelegram(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("SendTelegram error = %v, want Telegram's description", err)
	}
	if strings.Contains(err.Error(), testBotToken) {
		t.Fatalf("error leaks the bot token: %v", err)
	}

	telegramAPIBase = "http://127.0.0.1:1"
	err = (&AccessAuditService{}).SendTelegram(context.Background(), "hello")
	if err == nil || strings.Contains(err.Error(), testBotToken) || strings.Contains(err.Error(), "AA-secret") {
		t.Fatalf("transport error = %v, want a failure without the token", err)
	}
}

func TestAuditTelegramTokenIsWriteOnlyAndEncrypted(t *testing.T) {
	initAuditTestDB(t)
	enableTestInboundEncryption(t)
	svc := &AccessAuditService{}
	configureTestTelegram(t)

	if err := svc.SaveTelegramConfig(AuditTelegramConfigUpdate{Enabled: true, ChatID: "777"}); err != nil {
		t.Fatalf("update without token: %v", err)
	}
	view := svc.GetTelegramConfig()
	if !view.HasBotToken || view.ChatID != "777" || !view.Enabled {
		t.Fatalf("view = %+v, want the stored token kept and chat 777", view)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), testBotToken) {
		t.Fatalf("config view exposes the token: %s", raw)
	}
	var row model.Setting
	if err := database.GetDB().Where("`key` = ?", auditTelegramSettingKey).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.Value, testBotToken) || !strings.Contains(row.Value, "enc:v1:") {
		t.Fatalf("stored setting = %s, want the token encrypted", row.Value)
	}
	if token, err := svc.telegramBotToken(); err != nil || token != testBotToken {
		t.Fatalf("decrypted token = %q (%v), want the original", token, err)
	}
}
