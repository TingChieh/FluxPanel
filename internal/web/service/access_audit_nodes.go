package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/logger"
	"github.com/TingChieh/FluxPanel/internal/web/entity"
	"github.com/TingChieh/FluxPanel/internal/web/runtime"

	"gorm.io/gorm"
)

const (
	// AuditAllSources selects rows from this panel and every node.
	AuditAllSources = -1
	// AuditLocalSource is the NodeId of rows ingested from this panel's own Xray.
	AuditLocalSource = 0

	auditDateLayout          = "2006-01-02"
	auditNodeSyncSettingKey  = "accessAuditNodeSync"
	auditLocalSourceName     = "local"
	auditImportInsertBatches = 200
	auditNodePullTimeout     = 30 * time.Second
)

// ErrAuditTimezoneMismatch means a node buckets days in another zone, so its
// rows cannot be merged into this panel's days.
var ErrAuditTimezoneMismatch = errors.New("node audit time zone differs from this panel")

type auditPair struct {
	date, email string
}

// AuditNodeSyncStatus is the outcome of the last audit pull from one node.
type AuditNodeSyncStatus struct {
	LastSyncAt int64  `json:"lastSyncAt"`
	LastError  string `json:"lastError"`
}

func utcOffsetNow(loc *time.Location) int {
	_, off := time.Now().In(loc).Zone()
	return off
}

func newAuditExportHeader(loc *time.Location, from, to string) entity.AuditExport {
	return entity.AuditExport{
		TimeLocation: loc.String(),
		UTCOffset:    utcOffsetNow(loc),
		From:         from,
		To:           to,
		Daily:        []entity.AuditExportDaily{},
		IPs:          []entity.AuditExportIP{},
	}
}

func validateAuditWindow(from, to string) error {
	f, err := time.Parse(auditDateLayout, from)
	if err != nil {
		return fmt.Errorf("invalid from date %q", from)
	}
	t, err := time.Parse(auditDateLayout, to)
	if err != nil {
		return fmt.Errorf("invalid to date %q", to)
	}
	if t.Before(f) || t.Sub(f) > 31*24*time.Hour {
		return fmt.Errorf("invalid audit window %s..%s", from, to)
	}
	return nil
}

// ExportLocal returns this panel's own audit aggregates for [from, to] so a
// master can merge them; rows imported from this panel's nodes are excluded.
func (s *AccessAuditService) ExportLocal(from, to string) (entity.AuditExport, error) {
	if err := validateAuditWindow(from, to); err != nil {
		return entity.AuditExport{}, err
	}
	exp := newAuditExportHeader(auditLocation(), from, to)
	db := database.GetDB()
	if db == nil {
		return exp, nil
	}
	var daily []model.AccessAuditDaily
	if err := db.Where("node_id = ? AND date >= ? AND date <= ?", AuditLocalSource, from, to).Find(&daily).Error; err != nil {
		return exp, err
	}
	for _, r := range daily {
		exp.Daily = append(exp.Daily, entity.AuditExportDaily{
			Date: r.Date, Email: r.Email, RootDomain: r.RootDomain, Domain: r.Domain, Port: r.Port,
			Outbound: r.Outbound, HitCount: r.HitCount, FirstSeen: r.FirstSeen, LastSeen: r.LastSeen,
		})
	}
	var ips []model.AccessAuditClientIP
	if err := db.Where("node_id = ? AND date >= ? AND date <= ?", AuditLocalSource, from, to).Find(&ips).Error; err != nil {
		return exp, err
	}
	for _, r := range ips {
		exp.IPs = append(exp.IPs, entity.AuditExportIP{
			Date: r.Date, Email: r.Email, SrcIP: r.SrcIP, Subnet: r.Subnet, HitCount: r.HitCount,
			FirstSeen: r.FirstSeen, LastSeen: r.LastSeen, ActiveHours: r.ActiveHours, ActiveSlots: r.ActiveSlots,
		})
	}
	return exp, nil
}

// ImportNodeExport replaces nodeID's rows for [from, to] with the node's snapshot,
// scores them with this panel's rules and re-derives alerts for affected clients.
func (s *AccessAuditService) ImportNodeExport(nodeID int, from, to string, exp entity.AuditExport) error {
	if nodeID <= 0 {
		return fmt.Errorf("invalid audit source node %d", nodeID)
	}
	if err := validateAuditWindow(from, to); err != nil {
		return err
	}
	loc := auditLocation()
	if exp.TimeLocation != loc.String() || exp.UTCOffset != utcOffsetNow(loc) {
		return fmt.Errorf("%w: node uses %s (UTC%+.1f), this panel %s", ErrAuditTimezoneMismatch,
			exp.TimeLocation, float64(exp.UTCOffset)/3600, loc.String())
	}

	auditIngestMu.Lock()
	defer auditIngestMu.Unlock()
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	cfg := s.GetConfig()
	if !cfg.Enabled {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		pairs := make(map[auditPair]struct{})
		for _, table := range []any{&model.AccessAuditDaily{}, &model.AccessAuditClientIP{}} {
			var old []struct{ Date, Email string }
			if err := tx.Model(table).Distinct("date", "email").
				Where("node_id = ? AND date >= ? AND date <= ?", nodeID, from, to).Scan(&old).Error; err != nil {
				return err
			}
			for _, p := range old {
				pairs[auditPair{date: p.Date, email: p.Email}] = struct{}{}
			}
			if err := tx.Where("node_id = ? AND date >= ? AND date <= ?", nodeID, from, to).Delete(table).Error; err != nil {
				return err
			}
		}

		daily := make(map[dailyAggKey]*model.AccessAuditDaily)
		for _, d := range exp.Daily {
			domain := strings.ToLower(strings.TrimSpace(d.Domain))
			if d.Date < from || d.Date > to || d.Email == "" || domain == "" {
				continue
			}
			k := dailyAggKey{date: d.Date, email: d.Email, domain: domain, port: d.Port}
			if row, ok := daily[k]; ok {
				row.HitCount += d.HitCount
				row.FirstSeen = min(row.FirstSeen, d.FirstSeen)
				row.LastSeen = max(row.LastSeen, d.LastSeen)
				continue
			}
			root := strings.ToLower(strings.TrimSpace(d.RootDomain))
			if root == "" {
				root = domain
			}
			cat, risk, rule := classifyAuditTarget(domain, root, d.Port, cfg)
			daily[k] = &model.AccessAuditDaily{
				NodeId: nodeID, Date: d.Date, Email: d.Email, RootDomain: root, Domain: domain, Port: d.Port,
				Outbound: d.Outbound, Category: cat, RiskLevel: risk, RuleHit: rule,
				HitCount: d.HitCount, FirstSeen: d.FirstSeen, LastSeen: d.LastSeen,
			}
			pairs[auditPair{date: d.Date, email: d.Email}] = struct{}{}
		}
		dailyRows := make([]model.AccessAuditDaily, 0, len(daily))
		for _, row := range daily {
			dailyRows = append(dailyRows, *row)
		}
		if len(dailyRows) > 0 {
			if err := tx.CreateInBatches(dailyRows, auditImportInsertBatches).Error; err != nil {
				return err
			}
		}

		type ipKey struct{ date, email, srcIP string }
		ips := make(map[ipKey]*model.AccessAuditClientIP)
		for _, ip := range exp.IPs {
			if ip.Date < from || ip.Date > to || ip.Email == "" || ip.SrcIP == "" {
				continue
			}
			k := ipKey{date: ip.Date, email: ip.Email, srcIP: ip.SrcIP}
			if row, ok := ips[k]; ok {
				row.HitCount += ip.HitCount
				row.ActiveHours |= ip.ActiveHours
				row.ActiveSlots = mergeAuditSlotStrings(row.ActiveSlots, ip.ActiveSlots)
				row.FirstSeen = min(row.FirstSeen, ip.FirstSeen)
				row.LastSeen = max(row.LastSeen, ip.LastSeen)
				continue
			}
			subnet := ip.Subnet
			if subnet == "" {
				_, subnet = parseAuditSource(ip.SrcIP)
			}
			ips[k] = &model.AccessAuditClientIP{
				NodeId: nodeID, Date: ip.Date, Email: ip.Email, SrcIP: ip.SrcIP, Subnet: subnet,
				HitCount: ip.HitCount, FirstSeen: ip.FirstSeen, LastSeen: ip.LastSeen,
				ActiveHours: ip.ActiveHours, ActiveSlots: ip.ActiveSlots,
			}
			pairs[auditPair{date: ip.Date, email: ip.Email}] = struct{}{}
		}
		ipRows := make([]model.AccessAuditClientIP, 0, len(ips))
		for _, row := range ips {
			ipRows = append(ipRows, *row)
		}
		if len(ipRows) > 0 {
			if err := tx.CreateInBatches(ipRows, auditImportInsertBatches).Error; err != nil {
				return err
			}
		}

		_, err := refreshAuditAlerts(tx, pairs, cfg, nil)
		return err
	})
}

func auditSourceNames(tx *gorm.DB) (map[int]string, error) {
	names := map[int]string{AuditLocalSource: auditLocalSourceName}
	var nodes []struct {
		Id   int
		Name string
	}
	if err := tx.Model(&model.Node{}).Select("id", "name").Scan(&nodes).Error; err != nil {
		return nil, err
	}
	for _, n := range nodes {
		names[n.Id] = n.Name
	}
	return names, nil
}

func describeAuditSources(sources map[int]struct{}, names map[int]string) string {
	ids := make([]int, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := names[id]; ok && name != "" {
			labels = append(labels, name)
		} else {
			labels = append(labels, fmt.Sprintf("node %d", id))
		}
	}
	return strings.Join(labels, ", ")
}

// refreshAuditAlerts derives each client/day's alerts from the aggregate tables
// across every source and drops its unacknowledged alerts that no longer hold.
func refreshAuditAlerts(tx *gorm.DB, pairs map[auditPair]struct{}, cfg AuditConfig, srcHints map[dailyAggKey]string) (map[auditAlertKey]struct{}, error) {
	produced := make(map[auditAlertKey]struct{})
	if len(pairs) == 0 {
		return produced, nil
	}
	names, err := auditSourceNames(tx)
	if err != nil {
		return nil, err
	}
	for p := range pairs {
		var rows []model.AccessAuditDaily
		if err := tx.Where("date = ? AND email = ? AND risk_level > ? AND rule_hit <> ''", p.date, p.email, AuditRiskNone).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		type targetAgg struct {
			sample  model.AccessAuditDaily
			hits    int64
			last    int64
			sources map[int]struct{}
		}
		groups := make(map[dailyAggKey]*targetAgg)
		for _, r := range rows {
			k := dailyAggKey{date: r.Date, email: r.Email, domain: r.Domain, port: r.Port}
			g, ok := groups[k]
			if !ok {
				g = &targetAgg{sample: r, sources: make(map[int]struct{})}
				groups[k] = g
			}
			g.hits += r.HitCount
			g.last = max(g.last, r.LastSeen)
			g.sources[r.NodeId] = struct{}{}
		}
		for k, g := range groups {
			alert := newTargetAlert(k.date, k.email, k.domain, k.port, g.sample.Category, g.sample.RiskLevel,
				g.sample.RuleHit, g.hits, g.last, srcHints[k])
			alert.Detail += " via " + describeAuditSources(g.sources, names)
			if err := upsertAuditAlert(tx, alert, false); err != nil {
				return nil, err
			}
			produced[keyOfAlert(alert)] = struct{}{}
		}

		emitted, err := evaluateAccountSharing(tx, p.date, p.email, cfg)
		if err != nil {
			return nil, err
		}
		for _, a := range emitted {
			produced[keyOfAlert(a)] = struct{}{}
		}

		var unacked []model.AccessAuditAlert
		if err := tx.Where("date = ? AND email = ? AND acked = ?", p.date, p.email, false).Find(&unacked).Error; err != nil {
			return nil, err
		}
		var stale []uint
		for _, a := range unacked {
			if _, ok := produced[keyOfAlert(a)]; !ok {
				stale = append(stale, a.Id)
			}
		}
		if len(stale) > 0 {
			if err := tx.Where("id IN ?", stale).Delete(&model.AccessAuditAlert{}).Error; err != nil {
				return nil, err
			}
		}
	}
	return produced, nil
}

func allAuditPairs(tx *gorm.DB) (map[auditPair]struct{}, error) {
	pairs := make(map[auditPair]struct{})
	for _, table := range []any{&model.AccessAuditDaily{}, &model.AccessAuditClientIP{}} {
		var rows []struct{ Date, Email string }
		if err := tx.Model(table).Distinct("date", "email").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			pairs[auditPair{date: r.Date, email: r.Email}] = struct{}{}
		}
	}
	return pairs, nil
}

func deleteAlertsExcept(tx *gorm.DB, keep map[auditAlertKey]struct{}) error {
	var all []model.AccessAuditAlert
	if err := tx.Find(&all).Error; err != nil {
		return err
	}
	var stale []uint
	for _, a := range all {
		if _, ok := keep[keyOfAlert(a)]; !ok {
			stale = append(stale, a.Id)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return tx.Where("id IN ?", stale).Delete(&model.AccessAuditAlert{}).Error
}

func loadAuditNodeSyncStatus(db *gorm.DB) map[int]AuditNodeSyncStatus {
	out := make(map[int]AuditNodeSyncStatus)
	var row model.Setting
	if err := db.Where("`key` = ?", auditNodeSyncSettingKey).First(&row).Error; err != nil {
		return out
	}
	_ = json.Unmarshal([]byte(row.Value), &out)
	return out
}

// RecordNodeSyncResult remembers the outcome of the latest audit pull from nodeID.
func (s *AccessAuditService) RecordNodeSyncResult(nodeID int, syncErr error) error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	auditIngestMu.Lock()
	defer auditIngestMu.Unlock()
	statuses := loadAuditNodeSyncStatus(db)
	st := statuses[nodeID]
	if syncErr != nil {
		st.LastError = syncErr.Error()
	} else {
		st.LastSyncAt = time.Now().Unix()
		st.LastError = ""
	}
	statuses[nodeID] = st
	raw, err := json.Marshal(statuses)
	if err != nil {
		return err
	}
	return upsertSettingKV(db, auditNodeSyncSettingKey, string(raw))
}

// AuditNodePullResult counts the nodes whose audit snapshot was merged or failed.
type AuditNodePullResult struct {
	Synced int
	Failed int
}

// NodeSyncWindow is the span pulled from nodes: yesterday and today in this
// panel's zone, so a day keeps converging until it is over.
func (s *AccessAuditService) NodeSyncWindow() (from, to string) {
	now := time.Now().In(auditLocation())
	return now.AddDate(0, 0, -1).Format(auditDateLayout), now.Format(auditDateLayout)
}

// PullNodeAudits merges every enabled, online node's audit snapshot for the
// sync window and records each node's outcome.
func (s *AccessAuditService) PullNodeAudits(ctx context.Context) AuditNodePullResult {
	var res AuditNodePullResult
	mgr := runtime.GetManager()
	if mgr == nil || database.GetDB() == nil || !s.GetConfig().Enabled {
		return res
	}
	nodes, err := (&NodeService{}).GetAll()
	if err != nil {
		logger.Warning("access audit: load nodes failed:", err)
		return res
	}
	from, to := s.NodeSyncWindow()
	for _, n := range nodes {
		if !n.Enable || n.Status != "online" {
			continue
		}
		pullErr := s.pullNodeAudit(ctx, mgr, n, from, to)
		if err := s.RecordNodeSyncResult(n.Id, pullErr); err != nil {
			logger.Warning("access audit: record node sync result failed:", err)
		}
		if pullErr != nil {
			res.Failed++
			logger.Warning("access audit: pull from node", n.Name, "failed:", pullErr)
			continue
		}
		res.Synced++
	}
	return res
}

func (s *AccessAuditService) pullNodeAudit(ctx context.Context, mgr *runtime.Manager, n *model.Node, from, to string) error {
	remote, err := mgr.RemoteFor(n)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, auditNodePullTimeout)
	defer cancel()
	exp, err := remote.FetchAuditExport(cctx, from, to)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return errors.New("the node panel has no audit export; upgrade it to this version")
		}
		return err
	}
	return s.ImportNodeExport(n.Id, from, to, *exp)
}
