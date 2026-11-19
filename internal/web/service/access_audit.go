package service

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/xray"

	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AuditRiskNone = 0
	AuditRiskWarn = 1
	AuditRiskHigh = 2

	AuditAlertSensitiveDomain = "sensitive_domain"
	AuditAlertAccountSharing  = "account_sharing"
	AuditAlertHighRiskPort    = "high_risk_port"

	AuditCatSensitive = "sensitive"
	AuditCatRisk      = "risk"
	AuditCatAIDev     = "ai_dev"
	AuditCatVideo     = "video"
	AuditCatSocial    = "social"
	AuditCatGaming    = "gaming"
	AuditCatShopping  = "shopping"
	AuditCatNoise     = "noise"
	AuditCatOther     = "other"

	auditRuleConcurrentSubnets = "sharing:concurrent-subnets"
	auditRuleDailySubnets      = "sharing:daily-subnets"

	auditConfigSettingKey = "accessAuditConfig"
	auditCursorSettingKey = "accessAuditCursor"
)

// AuditConfig stores user-editable audit thresholds and domain rules in settings.
type AuditConfig struct {
	Enabled              bool     `json:"enabled" example:"true"`
	RetentionDays        int      `json:"retentionDays" example:"30"`
	MaxDailySubnets      int      `json:"maxDailySubnets" example:"4"`
	MaxConcurrentSubnets int      `json:"maxConcurrentSubnets" example:"3"`
	SensitiveDomains     []string `json:"sensitiveDomains"`
	SensitiveKeywords    []string `json:"sensitiveKeywords"`
	IgnoreNoiseDomains   []string `json:"ignoreNoiseDomains"`
}

// AuditConfigUpdateRequest carries config edits and an optional historical re-scan flag.
type AuditConfigUpdateRequest struct {
	Config     AuditConfig `json:"config"`
	Reevaluate bool        `json:"reevaluate" example:"true"`
}

// AuditIngestResult reports lines consumed from the local access log and, for a
// manual ingest, how many node snapshots were merged or failed.
type AuditIngestResult struct {
	LinesProcessed int   `json:"linesProcessed" example:"42"`
	BytesRead      int64 `json:"bytesRead" example:"4096"`
	NodesSynced    int   `json:"nodesSynced" example:"2"`
	NodesFailed    int   `json:"nodesFailed" example:"0"`
}

// AuditClientSummary summarizes one client's activity and risk signals over the queried window.
type AuditClientSummary struct {
	Email                string   `json:"email" example:"alice"`
	TotalHits            int64    `json:"totalHits" example:"1250"`
	UniqueRootDomains    int      `json:"uniqueRootDomains" example:"38"`
	UniqueIPs            int      `json:"uniqueIps" example:"2"`
	UniqueSubnets        int      `json:"uniqueSubnets" example:"1"`
	MaxConcurrentSubnets int      `json:"maxConcurrentSubnets" example:"1"`
	HighRiskHits         int64    `json:"highRiskHits" example:"0"`
	WarnRiskHits         int64    `json:"warnRiskHits" example:"0"`
	UnackedAlerts        int      `json:"unackedAlerts" example:"0"`
	TopDomains           []string `json:"topDomains"`
	LastSeen             int64    `json:"lastSeen" example:"1763540000"`
	NodeIds              []int    `json:"nodeIds"`
}

// AuditSourceSummary is one audit source (this panel or a node) in the window,
// with the outcome of the latest pull for nodes.
type AuditSourceSummary struct {
	NodeId        int    `json:"nodeId" example:"0"`
	Name          string `json:"name" example:"local"`
	TotalHits     int64  `json:"totalHits" example:"15000"`
	ActiveClients int    `json:"activeClients" example:"7"`
	LastSyncAt    int64  `json:"lastSyncAt" example:"1763540000"`
	SyncError     string `json:"syncError" example:""`
}

// AuditSummaryResponse is the top-level daily audit overview for all active clients.
type AuditSummaryResponse struct {
	Date           string               `json:"date" example:"2026-11-19"`
	Days           int                  `json:"days" example:"1"`
	ActiveClients  int                  `json:"activeClients" example:"7"`
	TotalHits      int64                `json:"totalHits" example:"15000"`
	HighRiskAlerts int                  `json:"highRiskAlerts" example:"0"`
	WarnAlerts     int                  `json:"warnAlerts" example:"1"`
	UnackedAlerts  int                  `json:"unackedAlerts" example:"1"`
	SharingClients int                  `json:"sharingClients" example:"0"`
	Clients        []AuditClientSummary `json:"clients"`
	Sources        []AuditSourceSummary `json:"sources"`
}

// AuditCategoryStat aggregates hits and domain counts for one traffic category.
type AuditCategoryStat struct {
	Category    string `json:"category" example:"ai_dev"`
	HitCount    int64  `json:"hitCount" example:"420"`
	DomainCount int    `json:"domainCount" example:"8"`
}

// AuditSubdomainItem shows one concrete host+port row under a collapsed root domain.
type AuditSubdomainItem struct {
	Domain    string `json:"domain" example:"api.github.com"`
	Port      int    `json:"port" example:"443"`
	Outbound  string `json:"outbound" example:"direct"`
	Category  string `json:"category" example:"ai_dev"`
	RiskLevel int    `json:"riskLevel" example:"0"`
	RuleHit   string `json:"ruleHit" example:""`
	HitCount  int64  `json:"hitCount" example:"120"`
	FirstSeen int64  `json:"firstSeen" example:"1763510000"`
	LastSeen  int64  `json:"lastSeen" example:"1763540000"`
	NodeIds   []int  `json:"nodeIds"`
}

// AuditDomainGroup collapses subdomains under one eTLD+1 root domain for readable inspection.
type AuditDomainGroup struct {
	RootDomain string               `json:"rootDomain" example:"github.com"`
	Category   string               `json:"category" example:"ai_dev"`
	RiskLevel  int                  `json:"riskLevel" example:"0"`
	RuleHit    string               `json:"ruleHit" example:""`
	HitCount   int64                `json:"hitCount" example:"240"`
	FirstSeen  int64                `json:"firstSeen" example:"1763510000"`
	LastSeen   int64                `json:"lastSeen" example:"1763540000"`
	Subdomains []AuditSubdomainItem `json:"subdomains"`
}

// AuditClientIPView exposes one source IP and its 24-hour activity bitmap for sharing review.
type AuditClientIPView struct {
	SrcIP       string `json:"srcIp" example:"198.51.100.10"`
	Subnet      string `json:"subnet" example:"198.51.100.0/24"`
	HitCount    int64  `json:"hitCount" example:"300"`
	FirstSeen   int64  `json:"firstSeen" example:"1763510000"`
	LastSeen    int64  `json:"lastSeen" example:"1763540000"`
	ActiveHours uint32 `json:"activeHours" example:"1536"`
	ActiveCount int    `json:"activeCount" example:"2"`
	NodeId      int    `json:"nodeId" example:"0"`
}

// AuditClientDetailResponse provides a single client's domains, categories, IPs, and alerts.
type AuditClientDetailResponse struct {
	Email                string                   `json:"email" example:"alice"`
	Date                 string                   `json:"date" example:"2026-11-19"`
	Days                 int                      `json:"days" example:"1"`
	HideNoise            bool                     `json:"hideNoise" example:"true"`
	TotalHits            int64                    `json:"totalHits" example:"1250"`
	NoiseHits            int64                    `json:"noiseHits" example:"300"`
	UniqueSubnets        int                      `json:"uniqueSubnets" example:"1"`
	MaxConcurrentSubnets int                      `json:"maxConcurrentSubnets" example:"1"`
	Categories           []AuditCategoryStat      `json:"categories"`
	Domains              []AuditDomainGroup       `json:"domains"`
	SourceIPs            []AuditClientIPView      `json:"sourceIps"`
	Alerts               []model.AccessAuditAlert `json:"alerts"`
}

type auditCursor struct {
	Path          string `json:"path"`
	Offset        int64  `json:"offset"`
	FirstLineHash string `json:"firstLineHash"`
}

// AccessAuditService ingests Xray access logs into aggregated SQLite tables and evaluates risk rules.
type AccessAuditService struct{}

var auditIngestMu sync.Mutex

// auditLocation follows the panel's timeLocation so audit days and hours match the daily cron
// jobs and the admin's clock, not the server's (often UTC) zone.
func auditLocation() *time.Location {
	if database.GetDB() == nil {
		return time.Local
	}
	if loc, err := (&SettingService{}).GetTimeLocation(); err == nil && loc != nil {
		return loc
	}
	return time.Local
}

// DefaultAuditConfig returns balanced thresholds for personal/small-team proxy auditing.
func DefaultAuditConfig() AuditConfig {
	return AuditConfig{
		Enabled:              true,
		RetentionDays:        30,
		MaxDailySubnets:      4,
		MaxConcurrentSubnets: 3,
		SensitiveDomains:     []string{},
		SensitiveKeywords:    []string{},
		IgnoreNoiseDomains:   []string{},
	}
}

func normalizeAuditConfig(cfg AuditConfig) AuditConfig {
	if cfg.RetentionDays < 1 {
		cfg.RetentionDays = 30
	}
	if cfg.RetentionDays > 365 {
		cfg.RetentionDays = 365
	}
	if cfg.MaxDailySubnets < 2 {
		cfg.MaxDailySubnets = 4
	}
	if cfg.MaxConcurrentSubnets < 2 {
		cfg.MaxConcurrentSubnets = 3
	}
	cfg.SensitiveDomains = cleanRuleList(cfg.SensitiveDomains)
	cfg.SensitiveKeywords = cleanRuleList(cfg.SensitiveKeywords)
	cfg.IgnoreNoiseDomains = cleanRuleList(cfg.IgnoreNoiseDomains)
	return cfg
}

func cleanRuleList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, raw := range items {
		v := strings.ToLower(strings.TrimSpace(raw))
		v = strings.TrimPrefix(v, "*.")
		v = strings.TrimPrefix(v, ".")
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (s *AccessAuditService) GetConfig() AuditConfig {
	cfg := DefaultAuditConfig()
	db := database.GetDB()
	if db == nil {
		return cfg
	}
	var row model.Setting
	if err := db.Where("`key` = ?", auditConfigSettingKey).First(&row).Error; err != nil {
		return cfg
	}
	if err := json.Unmarshal([]byte(row.Value), &cfg); err != nil {
		return DefaultAuditConfig()
	}
	return normalizeAuditConfig(cfg)
}

func (s *AccessAuditService) SaveConfig(cfg AuditConfig, reevaluate bool) error {
	cfg = normalizeAuditConfig(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	db := database.GetDB()
	if err := upsertSettingKV(db, auditConfigSettingKey, string(raw)); err != nil {
		return err
	}
	if reevaluate {
		return s.ReevaluateRules()
	}
	return nil
}

func upsertSettingKV(db *gorm.DB, key, value string) error {
	var existing model.Setting
	err := db.Where("`key` = ?", key).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(&model.Setting{Key: key, Value: value}).Error
	}
	if err != nil {
		return err
	}
	return db.Model(&model.Setting{}).Where("id = ?", existing.Id).Update("value", value).Error
}

func loadAuditCursor(db *gorm.DB) auditCursor {
	var cur auditCursor
	var row model.Setting
	if err := db.Where("`key` = ?", auditCursorSettingKey).First(&row).Error; err != nil {
		return cur
	}
	_ = json.Unmarshal([]byte(row.Value), &cur)
	return cur
}

func saveAuditCursor(db *gorm.DB, cur auditCursor) error {
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	return upsertSettingKV(db, auditCursorSettingKey, string(raw))
}

func parseAuditDestination(raw string) (host, root string, port int, isIP bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "tcp:")
	s = strings.TrimPrefix(s, "udp:")
	s = strings.TrimLeft(s, "/")
	if s == "" {
		return "", "", 0, false
	}

	h, pStr, err := net.SplitHostPort(s)
	if err != nil {
		h = strings.Trim(s, "[]")
	} else {
		port, _ = strconv.Atoi(pStr)
	}

	h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
	if h == "" {
		return "", "", port, false
	}
	if ip := net.ParseIP(h); ip != nil {
		norm := ip.String()
		return norm, norm, port, true
	}

	root = h
	if eTLD, err := publicsuffix.EffectiveTLDPlusOne(h); err == nil && eTLD != "" {
		root = eTLD
	}
	return h, root, port, false
}

func parseAuditSource(raw string) (srcIP, subnet string) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "tcp:")
	s = strings.TrimPrefix(s, "udp:")
	s = strings.TrimLeft(s, "/")
	if s == "" {
		return "", ""
	}
	h, _, err := net.SplitHostPort(s)
	if err != nil {
		h = strings.Trim(s, "[]")
	}
	ip := net.ParseIP(strings.TrimSpace(h))
	if ip == nil {
		return h, h
	}
	if v4 := ip.To4(); v4 != nil {
		masked := v4.Mask(net.CIDRMask(24, 32))
		return v4.String(), masked.String() + "/24"
	}
	masked := ip.Mask(net.CIDRMask(56, 128))
	return ip.String(), masked.String() + "/56"
}

var builtInNoiseRoots = map[string]struct{}{
	"icloud.com":            {},
	"apple.com":             {},
	"mzstatic.com":          {},
	"aaplimg.com":           {},
	"cdn-apple.com":         {},
	"googleapis.com":        {},
	"gstatic.com":           {},
	"gvt1.com":              {},
	"gvt2.com":              {},
	"google-analytics.com":  {},
	"googletagmanager.com":  {},
	"doubleclick.net":       {},
	"googlesyndication.com": {},
	"crashlytics.com":       {},
	"app-measurement.com":   {},
	"msftconnecttest.com":   {},
	"windowsupdate.com":     {},
	"microsoft.com":         {},
	"cloudflare-dns.com":    {},
	"digicert.com":          {},
	"letsencrypt.org":       {},
	"ocsp.apple.com":        {},
	"sentry.io":             {},
	"datadoghq.com":         {},
	"akamaihd.net":          {},
	"akamaized.net":         {},
	"fastly.net":            {},
	"cloudfront.net":        {},
	"edgesuite.net":         {},
	"edgekey.net":           {},
	"amazonaws.com":         {},
	"azureedge.net":         {},
	"trafficmanager.net":    {},
	"push.apple.com":        {},
	"mtalk.google.com":      {},
}

var builtInNoiseSubstrings = []string{
	"telemetry.", "metrics.", "analytics.", "beacon.", "ocsp.", "crl.", "safebrowsing.",
	"connectivitycheck.", "captive.", "push.", "mtalk.", "update.", "updates.",
}

var builtInCategoryRoots = map[string]string{
	"github.com":            AuditCatAIDev,
	"githubusercontent.com": AuditCatAIDev,
	"gitlab.com":            AuditCatAIDev,
	"openai.com":            AuditCatAIDev,
	"chatgpt.com":           AuditCatAIDev,
	"anthropic.com":         AuditCatAIDev,
	"claude.ai":             AuditCatAIDev,
	"cursor.sh":             AuditCatAIDev,
	"gemini.google.com":     AuditCatAIDev,
	"huggingface.co":        AuditCatAIDev,
	"stackoverflow.com":     AuditCatAIDev,
	"npmjs.org":             AuditCatAIDev,
	"pypi.org":              AuditCatAIDev,
	"docker.io":             AuditCatAIDev,
	"youtube.com":           AuditCatVideo,
	"googlevideo.com":       AuditCatVideo,
	"ytimg.com":             AuditCatVideo,
	"netflix.com":           AuditCatVideo,
	"nflxvideo.net":         AuditCatVideo,
	"twitch.tv":             AuditCatVideo,
	"bilibili.com":          AuditCatVideo,
	"disneyplus.com":        AuditCatVideo,
	"spotify.com":           AuditCatVideo,
	"tiktok.com":            AuditCatVideo,
	"x.com":                 AuditCatSocial,
	"twitter.com":           AuditCatSocial,
	"twimg.com":             AuditCatSocial,
	"t.me":                  AuditCatSocial,
	"telegram.org":          AuditCatSocial,
	"instagram.com":         AuditCatSocial,
	"facebook.com":          AuditCatSocial,
	"fbcdn.net":             AuditCatSocial,
	"reddit.com":            AuditCatSocial,
	"redd.it":               AuditCatSocial,
	"discord.com":           AuditCatSocial,
	"discordapp.com":        AuditCatSocial,
	"whatsapp.net":          AuditCatSocial,
	"threads.net":           AuditCatSocial,
	"steampowered.com":      AuditCatGaming,
	"steamcommunity.com":    AuditCatGaming,
	"epicgames.com":         AuditCatGaming,
	"riotgames.com":         AuditCatGaming,
	"playstation.net":       AuditCatGaming,
	"xboxlive.com":          AuditCatGaming,
	"nintendo.net":          AuditCatGaming,
	"amazon.com":            AuditCatShopping,
	"ebay.com":              AuditCatShopping,
	"paypal.com":            AuditCatShopping,
	"stripe.com":            AuditCatShopping,
	"binance.com":           AuditCatShopping,
	"okx.com":               AuditCatShopping,
	"coinbase.com":          AuditCatShopping,
}

var highRiskPorts = map[int]string{
	25:    "port:smtp",
	465:   "port:smtps",
	587:   "port:smtp-submission",
	3333:  "port:stratum-miner",
	4444:  "port:stratum-miner",
	14444: "port:stratum-miner",
	22:    "port:ssh",
	3389:  "port:rdp",
	445:   "port:smb",
	3306:  "port:mysql",
	6379:  "port:redis",
}

// hasLabelPrefix reports whether a non-final DNS label of domain starts with
// prefix, so "tracker1.example.org" matches "tracker" but "reading-tracker.dev" does not.
func hasLabelPrefix(domain, prefix string) bool {
	labels := strings.Split(domain, ".")
	for _, label := range labels[:len(labels)-1] {
		if strings.HasPrefix(label, prefix) {
			return true
		}
	}
	return false
}

func domainMatchesSuffix(domain, rule string) bool {
	return domain == rule || strings.HasSuffix(domain, "."+rule)
}

func classifyAuditTarget(domain, rootDomain string, port int, cfg AuditConfig) (category string, riskLevel int, ruleHit string) {
	// 1. User-configured sensitive domains & keywords take highest priority.
	for _, rule := range cfg.SensitiveDomains {
		if domainMatchesSuffix(domain, rule) || domainMatchesSuffix(rootDomain, rule) {
			return AuditCatSensitive, AuditRiskHigh, "domain:" + rule
		}
	}
	for _, kw := range cfg.SensitiveKeywords {
		if strings.Contains(domain, kw) {
			return AuditCatSensitive, AuditRiskHigh, "keyword:" + kw
		}
	}

	// 2. Built-in high-risk signals (darknet gateways, mining/BT markers, abuse ports).
	if strings.HasSuffix(domain, ".onion") || strings.Contains(domain, "tor2web") {
		return AuditCatRisk, AuditRiskHigh, "builtin:darknet"
	}
	if hasLabelPrefix(domain, "stratum") || strings.Contains(domain, "miningpool") || strings.Contains(domain, "hashpool") {
		return AuditCatRisk, AuditRiskHigh, "builtin:miner"
	}
	if hasLabelPrefix(domain, "tracker") || strings.Contains(domain, "bittorrent") || strings.Contains(domain, ".torrent") {
		return AuditCatRisk, AuditRiskWarn, "builtin:bt-tracker"
	}
	if tag, ok := highRiskPorts[port]; ok {
		lvl := AuditRiskWarn
		if port == 25 || port == 3333 || port == 4444 || port == 14444 {
			lvl = AuditRiskHigh
		}
		return AuditCatRisk, lvl, tag
	}

	// 3. Noise suppression (custom ignore list + built-in telemetry/CDN roots).
	for _, rule := range cfg.IgnoreNoiseDomains {
		if domainMatchesSuffix(domain, rule) || domainMatchesSuffix(rootDomain, rule) {
			return AuditCatNoise, AuditRiskNone, ""
		}
	}
	if _, ok := builtInNoiseRoots[domain]; ok {
		return AuditCatNoise, AuditRiskNone, ""
	}
	if _, ok := builtInNoiseRoots[rootDomain]; ok {
		return AuditCatNoise, AuditRiskNone, ""
	}
	for _, sub := range builtInNoiseSubstrings {
		if strings.Contains(domain, sub) {
			return AuditCatNoise, AuditRiskNone, ""
		}
	}

	// 4. General traffic categories for readable daily summaries.
	if cat, ok := builtInCategoryRoots[domain]; ok {
		return cat, AuditRiskNone, ""
	}
	if cat, ok := builtInCategoryRoots[rootDomain]; ok {
		return cat, AuditRiskNone, ""
	}
	return AuditCatOther, AuditRiskNone, ""
}

// IngestNow incrementally parses the configured Xray access log into audit tables.
func (s *AccessAuditService) IngestNow() (AuditIngestResult, error) {
	path, err := xray.GetAccessLogPath()
	if err != nil || path == "" || path == "none" {
		return AuditIngestResult{}, nil
	}
	return s.IngestFromPath(path)
}

func hashFirstLine(file *os.File) string {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	r := bufio.NewReaderSize(file, 4096)
	line, err := readAccessLogLine(r)
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:8])
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func endsWithNewline(file *os.File, size int64) bool {
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, size-1); err != nil {
		return false
	}
	return last[0] == '\n'
}

type dailyAggKey struct {
	date   string
	email  string
	domain string
	port   int
}

type dailyAggVal struct {
	rootDomain string
	outbound   string
	category   string
	riskLevel  int
	ruleHit    string
	hits       int64
	firstSeen  int64
	lastSeen   int64
	lastSrcIP  string
}

type ipAggKey struct {
	date  string
	email string
	srcIP string
}

type ipAggVal struct {
	subnet      string
	hits        int64
	firstSeen   int64
	lastSeen    int64
	activeHours uint32
}

// IngestFromPath reads only bytes appended since the saved cursor and updates audit aggregates.
func (s *AccessAuditService) IngestFromPath(logPath string) (AuditIngestResult, error) {
	auditIngestMu.Lock()
	defer auditIngestMu.Unlock()

	db := database.GetDB()
	if db == nil {
		return AuditIngestResult{}, errors.New("database is not initialized")
	}

	cfg := s.GetConfig()
	if !cfg.Enabled {
		return AuditIngestResult{}, nil
	}

	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return AuditIngestResult{}, nil
		}
		return AuditIngestResult{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return AuditIngestResult{}, err
	}
	if info.Size() == 0 {
		return AuditIngestResult{}, nil
	}

	cur := loadAuditCursor(db)
	firstHash := hashFirstLine(file)
	startOffset := cur.Offset
	if cur.Path != logPath || info.Size() < startOffset || (cur.FirstLineHash != "" && firstHash != cur.FirstLineHash) {
		startOffset = 0
	}
	if startOffset >= info.Size() {
		return AuditIngestResult{}, nil
	}

	if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
		return AuditIngestResult{}, err
	}

	// Bound the read to the stat'd size so lines Xray appends meanwhile wait for the next cursor.
	counter := &countingReader{r: io.LimitReader(file, info.Size()-startOffset)}
	reader := bufio.NewReaderSize(counter, maxAccessLogLineBytes)
	tailComplete := endsWithNewline(file, info.Size())
	dailyMap := make(map[dailyAggKey]*dailyAggVal)
	ipMap := make(map[ipAggKey]*ipAggVal)
	touchedClients := make(map[string]map[string]struct{})
	linesProcessed := 0
	consumed := startOffset
	loc := auditLocation()

	for {
		line, readErr := readAccessLogLine(reader)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return AuditIngestResult{}, readErr
		}
		pos := startOffset + counter.n - int64(reader.Buffered())
		if pos == info.Size() && !tailComplete {
			break
		}
		consumed = pos
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.Contains(trimmed, "api -> api") {
			continue
		}
		entry := parseAccessLogFields(trimmed)
		if entry.Email == "" || entry.ToAddress == "" || entry.DateTime.IsZero() {
			continue
		}

		host, root, port, _ := parseAuditDestination(entry.ToAddress)
		if host == "" {
			continue
		}
		srcIP, subnet := parseAuditSource(entry.FromAddress)
		localTime := entry.DateTime.In(loc)
		dateStr := localTime.Format("2006-01-02")
		ts := localTime.Unix()
		hourBit := uint32(1) << uint(localTime.Hour())

		cat, riskLvl, ruleHit := classifyAuditTarget(host, root, port, cfg)
		dk := dailyAggKey{date: dateStr, email: entry.Email, domain: host, port: port}
		if existing, ok := dailyMap[dk]; ok {
			existing.hits++
			existing.outbound = entry.Outbound
			if ts < existing.firstSeen {
				existing.firstSeen = ts
			}
			if ts > existing.lastSeen {
				existing.lastSeen = ts
			}
			if srcIP != "" {
				existing.lastSrcIP = srcIP
			}
		} else {
			dailyMap[dk] = &dailyAggVal{
				rootDomain: root,
				outbound:   entry.Outbound,
				category:   cat,
				riskLevel:  riskLvl,
				ruleHit:    ruleHit,
				hits:       1,
				firstSeen:  ts,
				lastSeen:   ts,
				lastSrcIP:  srcIP,
			}
		}

		if srcIP != "" {
			ik := ipAggKey{date: dateStr, email: entry.Email, srcIP: srcIP}
			if existing, ok := ipMap[ik]; ok {
				existing.hits++
				existing.activeHours |= hourBit
				if ts < existing.firstSeen {
					existing.firstSeen = ts
				}
				if ts > existing.lastSeen {
					existing.lastSeen = ts
				}
			} else {
				ipMap[ik] = &ipAggVal{
					subnet:      subnet,
					hits:        1,
					firstSeen:   ts,
					lastSeen:    ts,
					activeHours: hourBit,
				}
			}
		}

		if touchedClients[dateStr] == nil {
			touchedClients[dateStr] = make(map[string]struct{})
		}
		touchedClients[dateStr][entry.Email] = struct{}{}
		linesProcessed++
	}

	newOffset := consumed
	if newOffset == startOffset {
		return AuditIngestResult{}, nil
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := upsertDailyRows(tx, dailyMap); err != nil {
			return err
		}
		if err := upsertClientIPRows(tx, ipMap); err != nil {
			return err
		}
		pairs := make(map[auditPair]struct{})
		for dateStr, emails := range touchedClients {
			for email := range emails {
				pairs[auditPair{date: dateStr, email: email}] = struct{}{}
			}
		}
		srcHints := make(map[dailyAggKey]string, len(dailyMap))
		for k, v := range dailyMap {
			if v.lastSrcIP != "" {
				srcHints[k] = v.lastSrcIP
			}
		}
		if _, err := refreshAuditAlerts(tx, pairs, cfg, srcHints); err != nil {
			return err
		}
		return saveAuditCursor(tx, auditCursor{
			Path:          logPath,
			Offset:        newOffset,
			FirstLineHash: firstHash,
		})
	}); err != nil {
		return AuditIngestResult{}, err
	}

	return AuditIngestResult{
		LinesProcessed: linesProcessed,
		BytesRead:      newOffset - startOffset,
	}, nil
}

func upsertDailyRows(tx *gorm.DB, dailyMap map[dailyAggKey]*dailyAggVal) error {
	for k, v := range dailyMap {
		row := model.AccessAuditDaily{
			Date:       k.date,
			Email:      k.email,
			RootDomain: v.rootDomain,
			Domain:     k.domain,
			Port:       k.port,
			Outbound:   v.outbound,
			Category:   v.category,
			RiskLevel:  v.riskLevel,
			RuleHit:    v.ruleHit,
			HitCount:   v.hits,
			FirstSeen:  v.firstSeen,
			LastSeen:   v.lastSeen,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_id"}, {Name: "date"}, {Name: "email"}, {Name: "domain"}, {Name: "port"}},
			DoUpdates: clause.Assignments(map[string]any{
				"root_domain": v.rootDomain,
				"outbound":    v.outbound,
				"category":    v.category,
				"risk_level":  v.riskLevel,
				"rule_hit":    v.ruleHit,
				"hit_count":   gorm.Expr("hit_count + ?", v.hits),
				"first_seen":  gorm.Expr("CASE WHEN first_seen = 0 OR ? < first_seen THEN ? ELSE first_seen END", v.firstSeen, v.firstSeen),
				"last_seen":   gorm.Expr("CASE WHEN ? > last_seen THEN ? ELSE last_seen END", v.lastSeen, v.lastSeen),
			}),
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func upsertClientIPRows(tx *gorm.DB, ipMap map[ipAggKey]*ipAggVal) error {
	for k, v := range ipMap {
		row := model.AccessAuditClientIP{
			Date:        k.date,
			Email:       k.email,
			SrcIP:       k.srcIP,
			Subnet:      v.subnet,
			HitCount:    v.hits,
			FirstSeen:   v.firstSeen,
			LastSeen:    v.lastSeen,
			ActiveHours: v.activeHours,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_id"}, {Name: "date"}, {Name: "email"}, {Name: "src_ip"}},
			DoUpdates: clause.Assignments(map[string]any{
				"subnet":       v.subnet,
				"hit_count":    gorm.Expr("hit_count + ?", v.hits),
				"first_seen":   gorm.Expr("CASE WHEN first_seen = 0 OR ? < first_seen THEN ? ELSE first_seen END", v.firstSeen, v.firstSeen),
				"last_seen":    gorm.Expr("CASE WHEN ? > last_seen THEN ? ELSE last_seen END", v.lastSeen, v.lastSeen),
				"active_hours": gorm.Expr("active_hours | ?", v.activeHours),
			}),
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

type auditAlertKey struct {
	date, email, alertType, ruleName, target string
}

func keyOfAlert(a model.AccessAuditAlert) auditAlertKey {
	return auditAlertKey{a.Date, a.Email, a.AlertType, a.RuleName, a.Target}
}

// upsertAuditAlert stores alert.HitCount as the total derived from the aggregate
// tables; reopenOnGrowth re-raises an acknowledged alert whose count went up.
func upsertAuditAlert(tx *gorm.DB, alert model.AccessAuditAlert, reopenOnGrowth bool) error {
	updates := map[string]any{
		"created_at": alert.CreatedAt,
		"severity":   alert.Severity,
		"hit_count":  alert.HitCount,
		"detail":     alert.Detail,
	}
	if reopenOnGrowth {
		updates["acked"] = gorm.Expr("CASE WHEN ? > hit_count THEN ? ELSE acked END", alert.HitCount, false)
	}
	if alert.SrcIP != "" {
		updates["src_ip"] = alert.SrcIP
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "date"}, {Name: "email"}, {Name: "alert_type"}, {Name: "rule_name"}, {Name: "target"},
		},
		DoUpdates: clause.Assignments(updates),
	}).Create(&alert).Error
}

func newTargetAlert(date, email, domain string, port int, category string, riskLevel int, ruleHit string, hits, lastSeen int64, srcIP string) model.AccessAuditAlert {
	alertType := AuditAlertHighRiskPort
	if category == AuditCatSensitive {
		alertType = AuditAlertSensitiveDomain
	}
	target := domain
	if port > 0 {
		target = fmt.Sprintf("%s:%d", domain, port)
	}
	return model.AccessAuditAlert{
		CreatedAt: lastSeen,
		Date:      date,
		Email:     email,
		AlertType: alertType,
		Severity:  riskLevel,
		RuleName:  ruleHit,
		Target:    target,
		SrcIP:     srcIP,
		HitCount:  hits,
		Detail:    fmt.Sprintf("Matched rule %s on %s", ruleHit, target),
	}
}

func computeSubnetConcurrency(rows []model.AccessAuditClientIP) (uniqueSubnets int, peakHour int, peakSubnets []string, allSubnets []string) {
	subnetSet := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r.Subnet != "" {
			subnetSet[r.Subnet] = struct{}{}
		}
	}
	allSubnets = make([]string, 0, len(subnetSet))
	for s := range subnetSet {
		allSubnets = append(allSubnets, s)
	}
	sort.Strings(allSubnets)
	uniqueSubnets = len(allSubnets)

	peakHour = -1
	for h := range 24 {
		bit := uint32(1) << uint(h)
		hourSet := make(map[string]struct{})
		for _, r := range rows {
			if r.ActiveHours&bit != 0 && r.Subnet != "" {
				hourSet[r.Subnet] = struct{}{}
			}
		}
		if len(hourSet) > len(peakSubnets) {
			peakHour = h
			peakSubnets = make([]string, 0, len(hourSet))
			for s := range hourSet {
				peakSubnets = append(peakSubnets, s)
			}
			sort.Strings(peakSubnets)
		}
	}
	return uniqueSubnets, peakHour, peakSubnets, allSubnets
}

// evaluateAccountSharing upserts the day's sharing alerts and returns them; Target stays
// fixed per rule so a rising subnet count updates one row instead of adding another.
func evaluateAccountSharing(tx *gorm.DB, dateStr, email string, cfg AuditConfig) ([]model.AccessAuditAlert, error) {
	var ipRows []model.AccessAuditClientIP
	if err := tx.Where("date = ? AND email = ?", dateStr, email).Find(&ipRows).Error; err != nil {
		return nil, err
	}
	if len(ipRows) == 0 {
		return nil, nil
	}

	uniqueSubnets, peakHour, peakSubnets, allSubnets := computeSubnetConcurrency(ipRows)
	latestSeen := int64(0)
	latestIP := ""
	for _, r := range ipRows {
		if r.LastSeen >= latestSeen {
			latestSeen = r.LastSeen
			latestIP = r.SrcIP
		}
	}
	if latestSeen == 0 {
		latestSeen = time.Now().Unix()
	}

	var emitted []model.AccessAuditAlert
	if len(peakSubnets) >= cfg.MaxConcurrentSubnets {
		emitted = append(emitted, model.AccessAuditAlert{
			CreatedAt: latestSeen,
			Date:      dateStr,
			Email:     email,
			AlertType: AuditAlertAccountSharing,
			Severity:  AuditRiskHigh,
			RuleName:  auditRuleConcurrentSubnets,
			Target:    "same-hour subnets",
			SrcIP:     latestIP,
			HitCount:  int64(len(peakSubnets)),
			Detail:    fmt.Sprintf("Hour %02d:00 saw %d distinct subnets concurrently: %s", peakHour, len(peakSubnets), strings.Join(peakSubnets, ", ")),
		})
	}
	if uniqueSubnets >= cfg.MaxDailySubnets {
		emitted = append(emitted, model.AccessAuditAlert{
			CreatedAt: latestSeen,
			Date:      dateStr,
			Email:     email,
			AlertType: AuditAlertAccountSharing,
			Severity:  AuditRiskWarn,
			RuleName:  auditRuleDailySubnets,
			Target:    "daily subnets",
			SrcIP:     latestIP,
			HitCount:  int64(uniqueSubnets),
			Detail:    fmt.Sprintf("Daily source subnets reached %d: %s", uniqueSubnets, strings.Join(allSubnets, ", ")),
		})
	}
	for _, alert := range emitted {
		if err := upsertAuditAlert(tx, alert, true); err != nil {
			return nil, err
		}
	}
	return emitted, nil
}

// ReevaluateRules re-classifies stored daily rows against the current AuditConfig, refreshes
// alerts, and drops unacknowledged alerts the current rules no longer produce.
func (s *AccessAuditService) ReevaluateRules() error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	cfg := s.GetConfig()

	var rows []model.AccessAuditDaily
	if err := db.Find(&rows).Error; err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		for _, r := range rows {
			cat, riskLvl, ruleHit := classifyAuditTarget(r.Domain, r.RootDomain, r.Port, cfg)
			if cat == r.Category && riskLvl == r.RiskLevel && ruleHit == r.RuleHit {
				continue
			}
			if err := tx.Model(&model.AccessAuditDaily{}).Where("id = ?", r.Id).Updates(map[string]any{
				"category":   cat,
				"risk_level": riskLvl,
				"rule_hit":   ruleHit,
			}).Error; err != nil {
				return err
			}
		}
		pairs, err := allAuditPairs(tx)
		if err != nil {
			return err
		}
		produced, err := refreshAuditAlerts(tx, pairs, cfg, nil)
		if err != nil {
			return err
		}
		return deleteUnackedAlertsExcept(tx, produced)
	})
}

func resolveDateRange(dateStr string, days int) (endDate, startDate string, clampedDays int) {
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}
	loc := auditLocation()
	end := time.Now().In(loc)
	if dateStr != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", dateStr, loc); err == nil {
			end = parsed
		}
	}
	start := end.AddDate(0, 0, -(days - 1))
	return end.Format("2006-01-02"), start.Format("2006-01-02"), days
}

// GetSummary returns an overview across all active clients for the chosen date
// window; nodeID narrows clients to one source (AuditAllSources for every source).
func (s *AccessAuditService) GetSummary(dateStr string, days int, nodeID int) (AuditSummaryResponse, error) {
	endDate, startDate, clampedDays := resolveDateRange(dateStr, days)
	resp := AuditSummaryResponse{
		Date:    endDate,
		Days:    clampedDays,
		Clients: []AuditClientSummary{},
		Sources: []AuditSourceSummary{},
	}
	db := database.GetDB()
	if db == nil {
		return resp, nil
	}

	var dailyRows []model.AccessAuditDaily
	if err := db.Where("date >= ? AND date <= ?", startDate, endDate).Find(&dailyRows).Error; err != nil {
		return resp, err
	}

	var ipRows []model.AccessAuditClientIP
	if err := db.Where("date >= ? AND date <= ?", startDate, endDate).Find(&ipRows).Error; err != nil {
		return resp, err
	}

	var alertRows []model.AccessAuditAlert
	if err := db.Where("date >= ? AND date <= ?", startDate, endDate).Find(&alertRows).Error; err != nil {
		return resp, err
	}

	sources, err := summarizeAuditSources(db, dailyRows)
	if err != nil {
		return resp, err
	}
	resp.Sources = sources
	if nodeID != AuditAllSources {
		dailyRows = filterAuditRows(dailyRows, func(r model.AccessAuditDaily) bool { return r.NodeId == nodeID })
		ipRows = filterAuditRows(ipRows, func(r model.AccessAuditClientIP) bool { return r.NodeId == nodeID })
		inSource := make(map[string]struct{})
		for _, r := range dailyRows {
			inSource[r.Email] = struct{}{}
		}
		for _, r := range ipRows {
			inSource[r.Email] = struct{}{}
		}
		alertRows = filterAuditRows(alertRows, func(a model.AccessAuditAlert) bool {
			_, ok := inSource[a.Email]
			return ok
		})
	}

	type clientAcc struct {
		summary       AuditClientSummary
		rootHits      map[string]int64
		nonNoiseRoots map[string]int64
		nodes         map[int]struct{}
	}
	byClient := make(map[string]*clientAcc)
	getAcc := func(email string) *clientAcc {
		if a, ok := byClient[email]; ok {
			return a
		}
		a := &clientAcc{
			summary:       AuditClientSummary{Email: email, TopDomains: []string{}},
			rootHits:      make(map[string]int64),
			nonNoiseRoots: make(map[string]int64),
			nodes:         make(map[int]struct{}),
		}
		byClient[email] = a
		return a
	}

	for _, r := range dailyRows {
		a := getAcc(r.Email)
		a.nodes[r.NodeId] = struct{}{}
		a.summary.TotalHits += r.HitCount
		resp.TotalHits += r.HitCount
		a.rootHits[r.RootDomain] += r.HitCount
		if r.Category != AuditCatNoise {
			a.nonNoiseRoots[r.RootDomain] += r.HitCount
		}
		if r.RiskLevel >= AuditRiskHigh {
			a.summary.HighRiskHits += r.HitCount
		} else if r.RiskLevel == AuditRiskWarn {
			a.summary.WarnRiskHits += r.HitCount
		}
		if r.LastSeen > a.summary.LastSeen {
			a.summary.LastSeen = r.LastSeen
		}
	}

	ipsByClient := make(map[string][]model.AccessAuditClientIP)
	for _, r := range ipRows {
		getAcc(r.Email).nodes[r.NodeId] = struct{}{}
		ipsByClient[r.Email] = append(ipsByClient[r.Email], r)
	}

	sharingSet := make(map[string]struct{})
	for _, a := range alertRows {
		acc := getAcc(a.Email)
		if a.Severity >= AuditRiskHigh {
			resp.HighRiskAlerts++
		} else {
			resp.WarnAlerts++
		}
		if !a.Acked {
			resp.UnackedAlerts++
			acc.summary.UnackedAlerts++
		}
		if a.AlertType == AuditAlertAccountSharing {
			sharingSet[a.Email] = struct{}{}
		}
	}
	resp.SharingClients = len(sharingSet)

	for email, acc := range byClient {
		acc.summary.UniqueRootDomains = len(acc.rootHits)
		acc.summary.NodeIds = sortedNodeIDs(acc.nodes)
		acc.summary.TopDomains = topKeysByCount(acc.nonNoiseRoots, 5)
		if len(acc.summary.TopDomains) == 0 {
			acc.summary.TopDomains = topKeysByCount(acc.rootHits, 5)
		}

		ipList := ipsByClient[email]
		ipSet := make(map[string]struct{}, len(ipList))
		subnetSet := make(map[string]struct{}, len(ipList))
		byDate := make(map[string][]model.AccessAuditClientIP)
		for _, ip := range ipList {
			ipSet[ip.SrcIP] = struct{}{}
			if ip.Subnet != "" {
				subnetSet[ip.Subnet] = struct{}{}
			}
			byDate[ip.Date] = append(byDate[ip.Date], ip)
		}
		acc.summary.UniqueIPs = len(ipSet)
		acc.summary.UniqueSubnets = len(subnetSet)
		maxConc := 0
		for _, dayRows := range byDate {
			_, _, peakSubnets, _ := computeSubnetConcurrency(dayRows)
			if len(peakSubnets) > maxConc {
				maxConc = len(peakSubnets)
			}
		}
		acc.summary.MaxConcurrentSubnets = maxConc
		resp.Clients = append(resp.Clients, acc.summary)
	}

	sort.Slice(resp.Clients, func(i, j int) bool {
		ci, cj := resp.Clients[i], resp.Clients[j]
		if ci.HighRiskHits != cj.HighRiskHits {
			return ci.HighRiskHits > cj.HighRiskHits
		}
		if ci.UnackedAlerts != cj.UnackedAlerts {
			return ci.UnackedAlerts > cj.UnackedAlerts
		}
		if ci.TotalHits != cj.TotalHits {
			return ci.TotalHits > cj.TotalHits
		}
		return ci.Email < cj.Email
	})
	resp.ActiveClients = len(resp.Clients)
	return resp, nil
}

func topKeysByCount(m map[string]int64, limit int) []string {
	type kv struct {
		k string
		v int64
	}
	list := make([]kv, 0, len(m))
	for k, v := range m {
		list = append(list, kv{k: k, v: v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.k)
	}
	return out
}

// GetClientDetail returns categorized root domains, source IPs, and alerts for one
// client; nodeID narrows domains and IPs to one source (AuditAllSources for all).
func (s *AccessAuditService) GetClientDetail(email, dateStr string, days int, hideNoise bool, nodeID int) (AuditClientDetailResponse, error) {
	endDate, startDate, clampedDays := resolveDateRange(dateStr, days)
	resp := AuditClientDetailResponse{
		Email:      email,
		Date:       endDate,
		Days:       clampedDays,
		HideNoise:  hideNoise,
		Categories: []AuditCategoryStat{},
		Domains:    []AuditDomainGroup{},
		SourceIPs:  []AuditClientIPView{},
		Alerts:     []model.AccessAuditAlert{},
	}
	db := database.GetDB()
	if db == nil || email == "" {
		return resp, nil
	}

	var dailyRows []model.AccessAuditDaily
	if err := db.Where("email = ? AND date >= ? AND date <= ?", email, startDate, endDate).Find(&dailyRows).Error; err != nil {
		return resp, err
	}
	if nodeID != AuditAllSources {
		dailyRows = filterAuditRows(dailyRows, func(r model.AccessAuditDaily) bool { return r.NodeId == nodeID })
	}
	subNodes := make(map[string]map[int]struct{})

	catHits := make(map[string]int64)
	catDomains := make(map[string]map[string]struct{})
	groups := make(map[string]*AuditDomainGroup)
	subByGroup := make(map[string]map[string]*AuditSubdomainItem)

	for _, r := range dailyRows {
		resp.TotalHits += r.HitCount
		if r.Category == AuditCatNoise {
			resp.NoiseHits += r.HitCount
		}
		catHits[r.Category] += r.HitCount
		if catDomains[r.Category] == nil {
			catDomains[r.Category] = make(map[string]struct{})
		}
		catDomains[r.Category][r.RootDomain] = struct{}{}

		if hideNoise && r.Category == AuditCatNoise && r.RiskLevel == AuditRiskNone {
			continue
		}

		g, ok := groups[r.RootDomain]
		if !ok {
			g = &AuditDomainGroup{
				RootDomain: r.RootDomain,
				Category:   r.Category,
				RiskLevel:  r.RiskLevel,
				RuleHit:    r.RuleHit,
				FirstSeen:  r.FirstSeen,
				LastSeen:   r.LastSeen,
				Subdomains: []AuditSubdomainItem{},
			}
			groups[r.RootDomain] = g
			subByGroup[r.RootDomain] = make(map[string]*AuditSubdomainItem)
		}
		g.HitCount += r.HitCount
		if r.RiskLevel > g.RiskLevel {
			g.RiskLevel = r.RiskLevel
			g.RuleHit = r.RuleHit
			g.Category = r.Category
		}
		if r.FirstSeen > 0 && (g.FirstSeen == 0 || r.FirstSeen < g.FirstSeen) {
			g.FirstSeen = r.FirstSeen
		}
		if r.LastSeen > g.LastSeen {
			g.LastSeen = r.LastSeen
		}

		subKey := fmt.Sprintf("%s:%d", r.Domain, r.Port)
		if subNodes[subKey] == nil {
			subNodes[subKey] = make(map[int]struct{})
		}
		subNodes[subKey][r.NodeId] = struct{}{}
		if sub, exists := subByGroup[r.RootDomain][subKey]; exists {
			sub.HitCount += r.HitCount
			if r.RiskLevel > sub.RiskLevel {
				sub.RiskLevel = r.RiskLevel
				sub.RuleHit = r.RuleHit
			}
			if r.FirstSeen > 0 && (sub.FirstSeen == 0 || r.FirstSeen < sub.FirstSeen) {
				sub.FirstSeen = r.FirstSeen
			}
			if r.LastSeen > sub.LastSeen {
				sub.LastSeen = r.LastSeen
			}
		} else {
			subByGroup[r.RootDomain][subKey] = &AuditSubdomainItem{
				Domain:    r.Domain,
				Port:      r.Port,
				Outbound:  r.Outbound,
				Category:  r.Category,
				RiskLevel: r.RiskLevel,
				RuleHit:   r.RuleHit,
				HitCount:  r.HitCount,
				FirstSeen: r.FirstSeen,
				LastSeen:  r.LastSeen,
			}
		}
	}

	for cat, hits := range catHits {
		resp.Categories = append(resp.Categories, AuditCategoryStat{
			Category:    cat,
			HitCount:    hits,
			DomainCount: len(catDomains[cat]),
		})
	}
	sort.Slice(resp.Categories, func(i, j int) bool {
		return resp.Categories[i].HitCount > resp.Categories[j].HitCount
	})

	for root, g := range groups {
		for _, sub := range subByGroup[root] {
			sub.NodeIds = sortedNodeIDs(subNodes[fmt.Sprintf("%s:%d", sub.Domain, sub.Port)])
			g.Subdomains = append(g.Subdomains, *sub)
		}
		sort.Slice(g.Subdomains, func(i, j int) bool {
			if g.Subdomains[i].RiskLevel != g.Subdomains[j].RiskLevel {
				return g.Subdomains[i].RiskLevel > g.Subdomains[j].RiskLevel
			}
			return g.Subdomains[i].HitCount > g.Subdomains[j].HitCount
		})
		resp.Domains = append(resp.Domains, *g)
	}
	sort.Slice(resp.Domains, func(i, j int) bool {
		if resp.Domains[i].RiskLevel != resp.Domains[j].RiskLevel {
			return resp.Domains[i].RiskLevel > resp.Domains[j].RiskLevel
		}
		if resp.Domains[i].HitCount != resp.Domains[j].HitCount {
			return resp.Domains[i].HitCount > resp.Domains[j].HitCount
		}
		return resp.Domains[i].RootDomain < resp.Domains[j].RootDomain
	})

	var ipRows []model.AccessAuditClientIP
	if err := db.Where("email = ? AND date >= ? AND date <= ?", email, startDate, endDate).Find(&ipRows).Error; err != nil {
		return resp, err
	}
	if nodeID != AuditAllSources {
		ipRows = filterAuditRows(ipRows, func(r model.AccessAuditClientIP) bool { return r.NodeId == nodeID })
	}
	type ipSourceKey struct {
		srcIP  string
		nodeID int
	}
	mergedIPs := make(map[ipSourceKey]*AuditClientIPView)
	subnetSet := make(map[string]struct{})
	byDate := make(map[string][]model.AccessAuditClientIP)
	for _, r := range ipRows {
		if r.Subnet != "" {
			subnetSet[r.Subnet] = struct{}{}
		}
		byDate[r.Date] = append(byDate[r.Date], r)
		if existing, ok := mergedIPs[ipSourceKey{srcIP: r.SrcIP, nodeID: r.NodeId}]; ok {
			existing.HitCount += r.HitCount
			existing.ActiveHours |= r.ActiveHours
			if r.FirstSeen > 0 && (existing.FirstSeen == 0 || r.FirstSeen < existing.FirstSeen) {
				existing.FirstSeen = r.FirstSeen
			}
			if r.LastSeen > existing.LastSeen {
				existing.LastSeen = r.LastSeen
			}
		} else {
			mergedIPs[ipSourceKey{srcIP: r.SrcIP, nodeID: r.NodeId}] = &AuditClientIPView{
				NodeId:      r.NodeId,
				SrcIP:       r.SrcIP,
				Subnet:      r.Subnet,
				HitCount:    r.HitCount,
				FirstSeen:   r.FirstSeen,
				LastSeen:    r.LastSeen,
				ActiveHours: r.ActiveHours,
			}
		}
	}
	for _, v := range mergedIPs {
		v.ActiveCount = bits.OnesCount32(v.ActiveHours)
		resp.SourceIPs = append(resp.SourceIPs, *v)
	}
	sort.Slice(resp.SourceIPs, func(i, j int) bool {
		return resp.SourceIPs[i].HitCount > resp.SourceIPs[j].HitCount
	})
	resp.UniqueSubnets = len(subnetSet)
	for _, dayRows := range byDate {
		_, _, peakSubnets, _ := computeSubnetConcurrency(dayRows)
		if len(peakSubnets) > resp.MaxConcurrentSubnets {
			resp.MaxConcurrentSubnets = len(peakSubnets)
		}
	}

	if err := db.Where("email = ? AND date >= ? AND date <= ?", email, startDate, endDate).
		Order("acked ASC, severity DESC, created_at DESC").
		Limit(200).
		Find(&resp.Alerts).Error; err != nil {
		return resp, err
	}
	return resp, nil
}

// GetAlerts queries recent audit alerts with optional date, client, and unacked filters.
func (s *AccessAuditService) GetAlerts(dateStr, email string, onlyUnacked bool, limit int) ([]model.AccessAuditAlert, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	db := database.GetDB()
	if db == nil {
		return []model.AccessAuditAlert{}, nil
	}
	q := db.Model(&model.AccessAuditAlert{})
	if dateStr != "" {
		q = q.Where("date = ?", dateStr)
	}
	if email != "" {
		q = q.Where("email = ?", email)
	}
	if onlyUnacked {
		q = q.Where("acked = ?", false)
	}
	var alerts []model.AccessAuditAlert
	if err := q.Order("acked ASC, severity DESC, created_at DESC").Limit(limit).Find(&alerts).Error; err != nil {
		return nil, err
	}
	return slices.Clip(alerts), nil
}

func (s *AccessAuditService) AckAlert(id uint) error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	return db.Model(&model.AccessAuditAlert{}).Where("id = ?", id).Update("acked", true).Error
}

func (s *AccessAuditService) AckAllAlerts() error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	return db.Model(&model.AccessAuditAlert{}).Where("acked = ?", false).Update("acked", true).Error
}

// PruneOldData deletes aggregated audit records and alerts older than retentionDays.
func (s *AccessAuditService) PruneOldData(retentionDays int) error {
	if retentionDays < 1 {
		retentionDays = 30
	}
	db := database.GetDB()
	if db == nil {
		return nil
	}
	cutoff := time.Now().In(auditLocation()).AddDate(0, 0, -retentionDays).Format("2006-01-02")
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("date < ?", cutoff).Delete(&model.AccessAuditDaily{}).Error; err != nil {
			return err
		}
		if err := tx.Where("date < ?", cutoff).Delete(&model.AccessAuditClientIP{}).Error; err != nil {
			return err
		}
		return tx.Where("date < ?", cutoff).Delete(&model.AccessAuditAlert{}).Error
	})
}

func filterAuditRows[T any](rows []T, keep func(T) bool) []T {
	out := rows[:0:0]
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func sortedNodeIDs(set map[int]struct{}) []int {
	ids := make([]int, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// summarizeAuditSources lists this panel plus every node that is enabled or has
// rows in the window, with hit totals and the latest pull outcome.
func summarizeAuditSources(db *gorm.DB, dailyRows []model.AccessAuditDaily) ([]AuditSourceSummary, error) {
	hits := make(map[int]int64)
	clients := make(map[int]map[string]struct{})
	for _, r := range dailyRows {
		hits[r.NodeId] += r.HitCount
		if clients[r.NodeId] == nil {
			clients[r.NodeId] = make(map[string]struct{})
		}
		clients[r.NodeId][r.Email] = struct{}{}
	}
	var nodes []struct {
		Id     int
		Name   string
		Enable bool
	}
	if err := db.Model(&model.Node{}).Select("id", "name", "enable").Order("id").Scan(&nodes).Error; err != nil {
		return nil, err
	}
	statuses := loadAuditNodeSyncStatus(db)
	out := []AuditSourceSummary{{
		NodeId: AuditLocalSource, Name: auditLocalSourceName,
		TotalHits: hits[AuditLocalSource], ActiveClients: len(clients[AuditLocalSource]),
	}}
	for _, n := range nodes {
		if !n.Enable && hits[n.Id] == 0 {
			continue
		}
		st := statuses[n.Id]
		out = append(out, AuditSourceSummary{
			NodeId: n.Id, Name: n.Name, TotalHits: hits[n.Id], ActiveClients: len(clients[n.Id]),
			LastSyncAt: st.LastSyncAt, SyncError: st.LastError,
		})
	}
	return out, nil
}
