package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/TingChieh/FluxPanel/internal/crypto/nodetoken"
	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

const (
	auditTelegramSettingKey = "accessAuditTelegram"
	// telegramMaxMessageRunes stays under Telegram's 4096-character sendMessage cap.
	telegramMaxMessageRunes = 4000
	telegramRequestTimeout  = 15 * time.Second
	// MaxAuditTelegramRelayBytes bounds a relayed message so a master cannot flood a node.
	MaxAuditTelegramRelayBytes = 64 << 10
)

var (
	telegramAPIBase              = "https://api.telegram.org"
	auditTelegramTokenAAD        = []byte("settings/accessAuditTelegram.botToken")
	errAuditTelegramUnconfigured = errors.New("telegram delivery is not configured on this panel")
)

// AuditTelegramConfig is the Telegram delivery setup as shown to the panel; the
// bot token itself is write-only.
type AuditTelegramConfig struct {
	Enabled     bool   `json:"enabled" example:"true"`
	ChatID      string `json:"chatId" example:"123456789"`
	HasBotToken bool   `json:"hasBotToken" example:"true"`
}

// AuditTelegramConfigUpdate edits the Telegram setup; an empty BotToken keeps
// the stored one.
type AuditTelegramConfigUpdate struct {
	Enabled  bool   `json:"enabled" example:"true"`
	ChatID   string `json:"chatId" example:"123456789"`
	BotToken string `json:"botToken" example:""`
}

// AuditTelegramRelayRequest carries a message a master asks this panel to send.
type AuditTelegramRelayRequest struct {
	Text string `json:"text" example:"Daily access audit"`
}

type storedAuditTelegram struct {
	Enabled  bool   `json:"enabled"`
	ChatID   string `json:"chatId"`
	BotToken string `json:"botToken"`
}

func loadStoredAuditTelegram() storedAuditTelegram {
	var cfg storedAuditTelegram
	db := database.GetDB()
	if db == nil {
		return cfg
	}
	var row model.Setting
	if err := db.Where("`key` = ?", auditTelegramSettingKey).First(&row).Error; err != nil {
		return cfg
	}
	_ = json.Unmarshal([]byte(row.Value), &cfg)
	return cfg
}

// GetTelegramConfig returns the Telegram setup without the bot token.
func (s *AccessAuditService) GetTelegramConfig() AuditTelegramConfig {
	cfg := loadStoredAuditTelegram()
	return AuditTelegramConfig{Enabled: cfg.Enabled, ChatID: cfg.ChatID, HasBotToken: cfg.BotToken != ""}
}

// SaveTelegramConfig stores the Telegram setup, encrypting the bot token at rest
// when panel encryption is enabled.
func (s *AccessAuditService) SaveTelegramConfig(in AuditTelegramConfigUpdate) error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	cfg := loadStoredAuditTelegram()
	cfg.Enabled = in.Enabled
	cfg.ChatID = strings.TrimSpace(in.ChatID)
	if token := strings.TrimSpace(in.BotToken); token != "" {
		enc, err := nodetoken.EncryptBound(auditTelegramTokenAAD, token)
		if err != nil {
			return err
		}
		cfg.BotToken = enc
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return upsertSettingKV(db, auditTelegramSettingKey, string(raw))
}

func (s *AccessAuditService) telegramBotToken() (string, error) {
	cfg := loadStoredAuditTelegram()
	if cfg.BotToken == "" {
		return "", errAuditTelegramUnconfigured
	}
	return nodetoken.DecryptBound(auditTelegramTokenAAD, cfg.BotToken)
}

// SendTelegram delivers text through this panel's bot, split into as many
// messages as Telegram's length cap requires.
func (s *AccessAuditService) SendTelegram(ctx context.Context, text string) error {
	cfg := loadStoredAuditTelegram()
	if !cfg.Enabled || cfg.ChatID == "" || cfg.BotToken == "" {
		return errAuditTelegramUnconfigured
	}
	token, err := s.telegramBotToken()
	if err != nil {
		return err
	}
	client := (&SettingService{}).NewProxiedHTTPClient(telegramRequestTimeout)
	for _, part := range splitTelegramMessage(text) {
		if err := sendTelegramMessage(ctx, client, token, cfg.ChatID, part); err != nil {
			return err
		}
	}
	return nil
}

// splitTelegramMessage cuts at line breaks, falling back to a hard cut for a
// single line longer than one message.
func splitTelegramMessage(text string) []string {
	text = strings.TrimRight(text, "\n")
	var parts []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			parts = append(parts, string(cur))
			cur = cur[:0]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		r := []rune(line)
		for len(r) > telegramMaxMessageRunes {
			flush()
			parts = append(parts, string(r[:telegramMaxMessageRunes]))
			r = r[telegramMaxMessageRunes:]
		}
		sep := 0
		if len(cur) > 0 {
			sep = 1
		}
		if len(cur)+sep+len(r) > telegramMaxMessageRunes {
			flush()
			sep = 0
		}
		if sep == 1 {
			cur = append(cur, '\n')
		}
		cur = append(cur, r...)
	}
	flush()
	return parts
}

func sendTelegramMessage(ctx context.Context, client *http.Client, token, chatID, text string) error {
	body, err := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	redact := func(err error) error {
		return errors.New(strings.ReplaceAll(err.Error(), token, "<redacted>"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPIBase+"/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return redact(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return redact(fmt.Errorf("telegram request: %w", err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK || !out.OK {
		desc := out.Description
		if desc == "" {
			desc = resp.Status
		}
		return redact(fmt.Errorf("telegram rejected the message: %s", desc))
	}
	return nil
}
