package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/logger"
	"github.com/TingChieh/FluxPanel/internal/web/locale"
	"github.com/TingChieh/FluxPanel/internal/web/runtime"
)

const (
	auditDigestSettingKey = "accessAuditDigest"
	auditDigestRetryAfter = 10 * time.Minute
	auditDigestMaxAlerts  = 10
	auditDigestMaxClients = 15
	auditDigestTopDomains = 3
)

var auditDigestLangs = map[string]struct{}{"zh-CN": {}, "en-US": {}}

// AuditDigestConfig is the daily Telegram digest schedule plus the outcome of
// the last scheduled delivery.
type AuditDigestConfig struct {
	Enabled        bool   `json:"enabled" example:"true"`
	SendAt         string `json:"sendAt" example:"09:00"`
	Lang           string `json:"lang" example:"zh-CN"`
	DeliveryNodeID int    `json:"deliveryNodeId" example:"1"`
	LastSentDate   string `json:"lastSentDate" example:"2026-11-20"`
	LastAttemptAt  int64  `json:"lastAttemptAt" example:"1763600000"`
	LastError      string `json:"lastError" example:""`
}

// AuditDigestConfigUpdate edits the digest schedule; DeliveryNodeID 0 sends
// through this panel's own bot, a node id relays through that node's bot.
type AuditDigestConfigUpdate struct {
	Enabled        bool   `json:"enabled" example:"true"`
	SendAt         string `json:"sendAt" example:"09:00"`
	Lang           string `json:"lang" example:"zh-CN"`
	DeliveryNodeID int    `json:"deliveryNodeId" example:"1"`
}

// AuditDigestPreview is the rendered digest for one day.
type AuditDigestPreview struct {
	Date string `json:"date" example:"2026-11-19"`
	Text string `json:"text" example:"Access audit digest 2026-11-19"`
}

func defaultAuditDigestConfig() AuditDigestConfig {
	return AuditDigestConfig{SendAt: "09:00", Lang: "zh-CN"}
}

func parseDigestSendAt(v string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", strings.TrimSpace(v))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid send time %q, want HH:MM", v)
	}
	return t.Hour(), t.Minute(), nil
}

// GetDigestConfig returns the digest schedule and last delivery outcome.
func (s *AccessAuditService) GetDigestConfig() AuditDigestConfig {
	cfg := defaultAuditDigestConfig()
	db := database.GetDB()
	if db == nil {
		return cfg
	}
	var row model.Setting
	if err := db.Where("`key` = ?", auditDigestSettingKey).First(&row).Error; err != nil {
		return cfg
	}
	_ = json.Unmarshal([]byte(row.Value), &cfg)
	return cfg
}

func saveAuditDigestState(cfg AuditDigestConfig) error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return upsertSettingKV(db, auditDigestSettingKey, string(raw))
}

// SaveDigestConfig validates and stores the digest schedule, keeping the last
// delivery outcome.
func (s *AccessAuditService) SaveDigestConfig(in AuditDigestConfigUpdate) error {
	if _, _, err := parseDigestSendAt(in.SendAt); err != nil {
		return err
	}
	if _, ok := auditDigestLangs[in.Lang]; !ok {
		return fmt.Errorf("unsupported digest language %q", in.Lang)
	}
	if in.DeliveryNodeID < 0 {
		return fmt.Errorf("invalid delivery node %d", in.DeliveryNodeID)
	}
	cfg := s.GetDigestConfig()
	cfg.Enabled = in.Enabled
	cfg.SendAt = strings.TrimSpace(in.SendAt)
	cfg.Lang = in.Lang
	cfg.DeliveryNodeID = in.DeliveryNodeID
	return saveAuditDigestState(cfg)
}

// PreviousDay is yesterday in the panel's zone, the day a digest covers by default.
func (s *AccessAuditService) PreviousDay() string {
	return time.Now().In(auditLocation()).AddDate(0, 0, -1).Format(auditDateLayout)
}

// RunDigestSchedule sends the previous day's digest once per day after SendAt
// in the panel's zone, waiting auditDigestRetryAfter between failed attempts.
func (s *AccessAuditService) RunDigestSchedule(ctx context.Context, now time.Time) {
	cfg := s.GetDigestConfig()
	if !cfg.Enabled {
		return
	}
	local := now.In(auditLocation())
	today := local.Format(auditDateLayout)
	if cfg.LastSentDate == today {
		return
	}
	hour, minute, err := parseDigestSendAt(cfg.SendAt)
	if err != nil {
		return
	}
	if local.Before(time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, local.Location())) {
		return
	}
	if cfg.LastError != "" && now.Sub(time.Unix(cfg.LastAttemptAt, 0)) < auditDigestRetryAfter {
		return
	}

	sendErr := s.SendDigest(ctx, local.AddDate(0, 0, -1).Format(auditDateLayout))
	cfg = s.GetDigestConfig()
	cfg.LastAttemptAt = now.Unix()
	if sendErr != nil {
		cfg.LastError = sendErr.Error()
		logger.Warning("access audit digest delivery failed:", sendErr)
	} else {
		cfg.LastSentDate = today
		cfg.LastError = ""
	}
	if err := saveAuditDigestState(cfg); err != nil {
		logger.Warning("access audit digest state save failed:", err)
	}
}

// SendDigest renders date's digest and delivers it through this panel's bot
// or relays it through the configured node.
func (s *AccessAuditService) SendDigest(ctx context.Context, date string) error {
	cfg := s.GetDigestConfig()
	text, err := s.BuildDigest(date, cfg.Lang)
	if err != nil {
		return err
	}
	if cfg.DeliveryNodeID == 0 {
		return s.SendTelegram(ctx, text)
	}
	node, err := (&NodeService{}).GetById(cfg.DeliveryNodeID)
	if err != nil {
		return fmt.Errorf("digest delivery node %d: %w", cfg.DeliveryNodeID, err)
	}
	mgr := runtime.GetManager()
	if mgr == nil {
		return errors.New("node runtime is not initialized")
	}
	remote, err := mgr.RemoteFor(node)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, telegramRequestTimeout*2)
	defer cancel()
	return remote.RelayAuditTelegram(cctx, text)
}

func digestNumber(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

// BuildDigest renders one day's audit as Telegram HTML in lang; every value
// taken from traffic (emails, domains, node names) is HTML-escaped.
func (s *AccessAuditService) BuildDigest(date, lang string) (string, error) {
	if _, err := time.Parse(auditDateLayout, date); err != nil {
		return "", fmt.Errorf("invalid digest date %q", date)
	}
	tr := func(key string, kv ...string) string {
		msg := locale.I18nForLang(lang, key)
		for i := 0; i+1 < len(kv); i += 2 {
			msg = strings.ReplaceAll(msg, "{"+kv[i]+"}", kv[i+1])
		}
		return msg
	}
	summary, err := s.GetSummary(date, 1, AuditAllSources)
	if err != nil {
		return "", err
	}
	alerts, err := s.GetAlerts(date, "", false, 1000)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	line := func(v string) {
		b.WriteString(v)
		b.WriteString("\n")
	}
	line("<b>" + tr("pages.audit.digest.title", "date", date) + "</b>")
	if summary.ActiveClients == 0 {
		line(tr("pages.audit.digest.noActivity"))
	} else {
		line(tr("pages.audit.digest.overview", "clients", strconv.Itoa(summary.ActiveClients), "hits", digestNumber(summary.TotalHits)))
	}

	var parts []string
	for _, src := range summary.Sources {
		name := html.EscapeString(src.Name)
		if src.NodeId == AuditLocalSource {
			name = tr("pages.audit.sources.local")
		}
		parts = append(parts, name+" "+digestNumber(src.TotalHits))
	}
	line(tr("pages.audit.digest.sources", "list", strings.Join(parts, " · ")))
	for _, src := range summary.Sources {
		if src.SyncError != "" {
			line(tr("pages.audit.digest.syncFailed", "name", html.EscapeString(src.Name), "error", html.EscapeString(src.SyncError)))
		}
	}

	line("")
	if len(alerts) == 0 {
		line(tr("pages.audit.digest.noAlerts"))
	} else {
		line(tr("pages.audit.digest.alertsHeader", "high", strconv.Itoa(summary.HighRiskAlerts),
			"warn", strconv.Itoa(summary.WarnAlerts), "unacked", strconv.Itoa(summary.UnackedAlerts)))
		sort.SliceStable(alerts, func(i, j int) bool { return alerts[i].Severity > alerts[j].Severity })
		for i, a := range alerts {
			if i == auditDigestMaxAlerts {
				line(tr("pages.audit.digest.more", "count", strconv.Itoa(len(alerts)-i)))
				break
			}
			icon := "🟠"
			if a.Severity >= AuditRiskHigh {
				icon = "🔴"
			}
			line(fmt.Sprintf("%s %s · %s · %s", icon, html.EscapeString(a.Email), digestAlertType(tr, a.AlertType), digestAlertTarget(tr, a)))
		}
	}

	if len(summary.Clients) > 0 {
		line("")
		line(tr("pages.audit.digest.clientsHeader"))
		clients := summary.Clients
		sort.SliceStable(clients, func(i, j int) bool { return clients[i].TotalHits > clients[j].TotalHits })
		for i, c := range clients {
			if i == auditDigestMaxClients {
				line(tr("pages.audit.digest.more", "count", strconv.Itoa(len(clients)-i)))
				break
			}
			domains := c.TopDomains
			if len(domains) > auditDigestTopDomains {
				domains = domains[:auditDigestTopDomains]
			}
			escaped := make([]string, 0, len(domains))
			for _, d := range domains {
				escaped = append(escaped, html.EscapeString(d))
			}
			line("• " + tr("pages.audit.digest.clientLine", "email", html.EscapeString(c.Email),
				"hits", digestNumber(c.TotalHits), "subnets", strconv.Itoa(c.UniqueSubnets),
				"domains", strings.Join(escaped, ", ")))
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func digestAlertType(tr func(string, ...string) string, alertType string) string {
	switch alertType {
	case AuditAlertSensitiveDomain:
		return tr("pages.audit.alertTypes.sensitive_domain")
	case AuditAlertAccountSharing:
		return tr("pages.audit.alertTypes.account_sharing")
	default:
		return tr("pages.audit.alertTypes.high_risk_port")
	}
}

func digestAlertTarget(tr func(string, ...string) string, a model.AccessAuditAlert) string {
	count := strconv.FormatInt(a.HitCount, 10)
	switch a.RuleName {
	case auditRuleConcurrentSubnets:
		return tr("pages.audit.digest.concurrentSubnets", "count", count)
	case auditRuleDailySubnets:
		return tr("pages.audit.digest.dailySubnets", "count", count)
	default:
		return html.EscapeString(a.Target) + " ×" + count
	}
}
