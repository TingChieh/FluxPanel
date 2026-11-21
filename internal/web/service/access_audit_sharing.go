package service

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database/model"

	"gorm.io/gorm"
)

const (
	auditSlotMinutes = 10
	auditSlotsPerHr  = 60 / auditSlotMinutes
	auditSlotCount   = 24 * auditSlotsPerHr
	auditSlotBytes   = auditSlotCount / 8

	auditTargetConcurrentSubnets       = "concurrent subnets"
	auditTargetLegacyConcurrentSubnets = "same-hour subnets"
)

// auditSlotMask is a 144-bit daily activity bitmap (one bit per 10-minute slot).
type auditSlotMask struct {
	bits [auditSlotBytes]byte
}

func auditSlotIndex(t time.Time) int {
	return t.Hour()*auditSlotsPerHr + t.Minute()/auditSlotMinutes
}

func (m *auditSlotMask) set(slot int) {
	if slot < 0 || slot >= auditSlotCount {
		return
	}
	m.bits[slot/8] |= 1 << uint(slot%8)
}

func (m auditSlotMask) has(slot int) bool {
	if slot < 0 || slot >= auditSlotCount {
		return false
	}
	return m.bits[slot/8]&(1<<uint(slot%8)) != 0
}

func (m auditSlotMask) or(other auditSlotMask) auditSlotMask {
	var out auditSlotMask
	for i := range auditSlotBytes {
		out.bits[i] = m.bits[i] | other.bits[i]
	}
	return out
}

func (m auditSlotMask) isZero() bool {
	for _, b := range m.bits {
		if b != 0 {
			return false
		}
	}
	return true
}

func (m auditSlotMask) encode() string {
	if m.isZero() {
		return ""
	}
	return hex.EncodeToString(m.bits[:])
}

func decodeAuditSlotMask(raw string) (auditSlotMask, bool) {
	var m auditSlotMask
	s := strings.TrimSpace(raw)
	if len(s) != auditSlotBytes*2 {
		return m, false
	}
	decoded, err := hex.DecodeString(s)
	if err != nil || len(decoded) != auditSlotBytes {
		return m, false
	}
	copy(m.bits[:], decoded)
	return m, !m.isZero()
}

// mergeClientIPRowSlots combines two rows' slot bitmaps without losing legacy
// ActiveHours activity when one row predates ActiveSlots and the other does not.
func mergeClientIPRowSlots(a, b model.AccessAuditClientIP, loc *time.Location) string {
	_, okA := decodeAuditSlotMask(a.ActiveSlots)
	_, okB := decodeAuditSlotMask(b.ActiveSlots)
	if !okA && !okB {
		return ""
	}
	return slotsForClientIPRow(a, loc).or(slotsForClientIPRow(b, loc)).encode()
}

// isNonPublicSharingIP excludes loopback, RFC 1918, link-local, ULA, and RFC 6598
// CGNAT/relay space so local proxies and overlays never count as external networks.
func isNonPublicSharingIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
	}
	return false
}

// sharingNetworkOf maps a public client IP to its sharing network bucket
// (IPv4 /16, IPv6 /48) so carrier CGNAT and dynamic IPv6 PD rotations collapse.
func sharingNetworkOf(srcIP, fallbackSubnet string) (network string, isV6 bool, ok bool) {
	ip := net.ParseIP(strings.TrimSpace(srcIP))
	if ip == nil && fallbackSubnet != "" {
		if parsed, _, err := net.ParseCIDR(strings.TrimSpace(fallbackSubnet)); err == nil {
			ip = parsed
		} else {
			ip = net.ParseIP(strings.TrimSpace(fallbackSubnet))
		}
	}
	if isNonPublicSharingIP(ip) {
		return "", false, false
	}
	if v4 := ip.To4(); v4 != nil {
		masked := v4.Mask(net.CIDRMask(16, 32))
		return masked.String() + "/16", false, true
	}
	masked := ip.Mask(net.CIDRMask(48, 128))
	return masked.String() + "/48", true, true
}

// slotsForClientIPRow uses the exact 10-minute bitmap when present and falls back
// to ActiveHours clamped by [FirstSeen, LastSeen] for legacy rows and older nodes.
func slotsForClientIPRow(r model.AccessAuditClientIP, loc *time.Location) auditSlotMask {
	if m, ok := decodeAuditSlotMask(r.ActiveSlots); ok {
		return m
	}
	var m auditSlotMask
	if r.ActiveHours == 0 {
		return m
	}
	dayStart, err := time.ParseInLocation(auditDateLayout, r.Date, loc)
	if err != nil {
		for h := range 24 {
			if r.ActiveHours&(1<<uint(h)) == 0 {
				continue
			}
			for s := range auditSlotsPerHr {
				m.set(h*auditSlotsPerHr + s)
			}
		}
		return m
	}
	hasBounds := r.FirstSeen > 0 && r.LastSeen >= r.FirstSeen
	if !hasBounds {
		for h := range 24 {
			if r.ActiveHours&(1<<uint(h)) == 0 {
				continue
			}
			for s := range auditSlotsPerHr {
				m.set(h*auditSlotsPerHr + s)
			}
		}
		return m
	}

	// Step actual UTC 10-minute intervals across the local calendar day so DST
	// fall-back (25h) and spring-forward (23h) transitions match wall-clock hours.
	y, mo, d := dayStart.Date()
	dayBegin := time.Date(y, mo, d, 0, 0, 0, 0, loc).Unix()
	nextDayBegin := time.Date(y, mo, d+1, 0, 0, 0, 0, loc).Unix()
	spanStart := max(dayBegin, r.FirstSeen)
	spanEnd := min(nextDayBegin-1, r.LastSeen)
	if spanStart > spanEnd {
		return m
	}
	const stepSec = int64(auditSlotMinutes * 60)
	for t0 := dayBegin; t0 < nextDayBegin; t0 += stepSec {
		if t0 > spanEnd {
			break
		}
		if t0+stepSec-1 < spanStart {
			continue
		}
		lt := time.Unix(t0, 0).In(loc)
		if r.ActiveHours&(1<<uint(lt.Hour())) != 0 {
			m.set(auditSlotIndex(lt))
		}
	}
	return m
}

func formatZoneOffset(t time.Time) string {
	zone, offsetSec := t.Zone()
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hh := offsetSec / 3600
	mm := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s %s%02d:%02d", zone, sign, hh, mm)
}

// formatSlotRange formats a 10-minute wall-clock slot and appends zone/offset
// labels when a DST fall-back day repeats that wall-clock hour.
func formatSlotRange(dateStr string, slot int, loc *time.Location, rows []model.AccessAuditClientIP) string {
	if slot < 0 || slot >= auditSlotCount {
		return "00:00-00:09"
	}
	h := slot / auditSlotsPerHr
	m := (slot % auditSlotsPerHr) * auditSlotMinutes
	base := fmt.Sprintf("%02d:%02d-%02d:%02d", h, m, h, m+auditSlotMinutes-1)
	if loc == nil || dateStr == "" {
		return base
	}
	dayStart, err := time.ParseInLocation(auditDateLayout, dateStr, loc)
	if err != nil {
		return base
	}
	y, mo, d := dayStart.Date()
	dayBegin := time.Date(y, mo, d, 0, 0, 0, 0, loc).Unix()
	nextDayBegin := time.Date(y, mo, d+1, 0, 0, 0, 0, loc).Unix()
	const stepSec = int64(auditSlotMinutes * 60)

	var matches []int64
	for t0 := dayBegin; t0 < nextDayBegin; t0 += stepSec {
		if auditSlotIndex(time.Unix(t0, 0).In(loc)) == slot {
			matches = append(matches, t0)
		}
	}
	if len(matches) <= 1 {
		return base
	}

	var active []int64
	for _, t0 := range matches {
		t1 := t0 + stepSec - 1
		for _, r := range rows {
			if r.FirstSeen > 0 && r.LastSeen >= r.FirstSeen && r.FirstSeen <= t1 && r.LastSeen >= t0 {
				active = append(active, t0)
				break
			}
		}
	}
	if len(active) == 0 {
		active = matches
	}
	labels := make([]string, 0, len(active))
	for _, t0 := range active {
		labels = append(labels, formatZoneOffset(time.Unix(t0, 0).In(loc)))
	}
	return fmt.Sprintf("%s (%s)", base, strings.Join(labels, " / "))
}

// upgradeLegacyConcurrentAlert migrates an existing pre-upgrade "same-hour subnets"
// row to "concurrent subnets" in place so ack state is kept without duplicates.
func upgradeLegacyConcurrentAlert(tx *gorm.DB, dateStr, email string) error {
	var legacy model.AccessAuditAlert
	err := tx.Where("date = ? AND email = ? AND alert_type = ? AND rule_name = ? AND target = ?",
		dateStr, email, AuditAlertAccountSharing, auditRuleConcurrentSubnets, auditTargetLegacyConcurrentSubnets).
		First(&legacy).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var currentCount int64
	if err := tx.Model(&model.AccessAuditAlert{}).
		Where("date = ? AND email = ? AND alert_type = ? AND rule_name = ? AND target = ?",
			dateStr, email, AuditAlertAccountSharing, auditRuleConcurrentSubnets, auditTargetConcurrentSubnets).
		Count(&currentCount).Error; err != nil {
		return err
	}
	if currentCount > 0 {
		return tx.Delete(&model.AccessAuditAlert{}, legacy.Id).Error
	}
	return tx.Model(&model.AccessAuditAlert{}).Where("id = ?", legacy.Id).
		Update("target", auditTargetConcurrentSubnets).Error
}

// dedupeLegacySharingAlerts hides a legacy "same-hour subnets" alert when a newer
// "concurrent subnets" row exists for the same day/client, keeping untouched history.
func dedupeLegacySharingAlerts(alerts []model.AccessAuditAlert) []model.AccessAuditAlert {
	hasCurrent := make(map[auditPair]struct{})
	for _, a := range alerts {
		if a.AlertType == AuditAlertAccountSharing && a.RuleName == auditRuleConcurrentSubnets && a.Target == auditTargetConcurrentSubnets {
			hasCurrent[auditPair{date: a.Date, email: a.Email}] = struct{}{}
		}
	}
	if len(hasCurrent) == 0 {
		return alerts
	}
	out := make([]model.AccessAuditAlert, 0, len(alerts))
	for _, a := range alerts {
		if a.AlertType == AuditAlertAccountSharing && a.RuleName == auditRuleConcurrentSubnets && a.Target == auditTargetLegacyConcurrentSubnets {
			if _, dup := hasCurrent[auditPair{date: a.Date, email: a.Email}]; dup {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// summarizeEffectiveNetworks deduplicates dual-stack activity with max(v4, v6)
// and formats an explainable sorted summary of the dominant family's networks.
func summarizeEffectiveNetworks(nets map[string]bool) (count int, summary string) {
	var v4, v6 []string
	for netKey, isV6 := range nets {
		if isV6 {
			v6 = append(v6, netKey)
		} else {
			v4 = append(v4, netKey)
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)
	if len(v4) >= len(v6) {
		count = len(v4)
		summary = strings.Join(v4, ", ")
		if len(v6) > 0 {
			summary += fmt.Sprintf(" (+%d IPv6)", len(v6))
		}
		return count, summary
	}
	count = len(v6)
	summary = strings.Join(v6, ", ")
	if len(v4) > 0 {
		summary += fmt.Sprintf(" (+%d IPv4)", len(v4))
	}
	return count, summary
}

func effectivePublicNetworkCount(rows []model.AccessAuditClientIP) int {
	nets := make(map[string]bool)
	for _, r := range rows {
		if netKey, isV6, ok := sharingNetworkOf(r.SrcIP, r.Subnet); ok {
			nets[netKey] = isV6
		}
	}
	count, _ := summarizeEffectiveNetworks(nets)
	return count
}

type sharingDayMetrics struct {
	peakConcurrent int
	peakSlot       int
	peakSummary    string
	exceedingSlots int
	dailyQualified int
	dailySummary   string
}

type sharingNetworkAgg struct {
	isV6  bool
	hits  int64
	slots auditSlotMask
}

// evaluateSharingDay merges source IPs into normalized public networks, filters
// low-hit noise, and measures sustained 10-minute concurrency and daily breadth.
func evaluateSharingDay(rows []model.AccessAuditClientIP, cfg AuditConfig, loc *time.Location) sharingDayMetrics {
	byNet := make(map[string]*sharingNetworkAgg)
	for _, r := range rows {
		netKey, isV6, ok := sharingNetworkOf(r.SrcIP, r.Subnet)
		if !ok {
			continue
		}
		agg, exists := byNet[netKey]
		if !exists {
			agg = &sharingNetworkAgg{isV6: isV6}
			byNet[netKey] = agg
		}
		agg.hits += r.HitCount
		agg.slots = agg.slots.or(slotsForClientIPRow(r, loc))
	}

	minHits := int64(max(1, cfg.MinSharingHits))
	qualified := make(map[string]*sharingNetworkAgg, len(byNet))
	qualifiedSet := make(map[string]bool, len(byNet))
	for netKey, agg := range byNet {
		if agg.hits < minHits {
			continue
		}
		qualified[netKey] = agg
		qualifiedSet[netKey] = agg.isV6
	}

	res := sharingDayMetrics{peakSlot: -1}
	res.dailyQualified, res.dailySummary = summarizeEffectiveNetworks(qualifiedSet)
	if len(qualified) == 0 {
		return res
	}

	for slot := range auditSlotCount {
		slotNets := make(map[string]bool)
		for netKey, agg := range qualified {
			if agg.slots.has(slot) {
				slotNets[netKey] = agg.isV6
			}
		}
		if len(slotNets) == 0 {
			continue
		}
		count, summary := summarizeEffectiveNetworks(slotNets)
		if count >= cfg.MaxConcurrentSubnets {
			res.exceedingSlots++
		}
		if count > res.peakConcurrent {
			res.peakConcurrent = count
			res.peakSlot = slot
			res.peakSummary = summary
		}
	}
	return res
}
