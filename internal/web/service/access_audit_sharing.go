package service

import (
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database/model"
)

const (
	auditSlotMinutes = 10
	auditSlotsPerHr  = 60 / auditSlotMinutes
	auditSlotCount   = 24 * auditSlotsPerHr
	auditSlotBytes   = auditSlotCount / 8
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

func mergeAuditSlotStrings(a, b string) string {
	ma, okA := decodeAuditSlotMask(a)
	mb, okB := decodeAuditSlotMask(b)
	switch {
	case okA && okB:
		return ma.or(mb).encode()
	case okA:
		return ma.encode()
	case okB:
		return mb.encode()
	default:
		return ""
	}
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
	for h := range 24 {
		if r.ActiveHours&(1<<uint(h)) == 0 {
			continue
		}
		hStart := dayStart.Add(time.Duration(h) * time.Hour).Unix()
		hEnd := hStart + 3600
		if !hasBounds {
			for s := range auditSlotsPerHr {
				m.set(h*auditSlotsPerHr + s)
			}
			continue
		}
		spanStart := max(hStart, r.FirstSeen)
		spanEnd := min(hEnd-1, r.LastSeen)
		if spanStart > spanEnd {
			continue
		}
		for s := range auditSlotsPerHr {
			slotStart := hStart + int64(s*auditSlotMinutes*60)
			slotEnd := slotStart + int64(auditSlotMinutes*60) - 1
			if slotStart <= spanEnd && slotEnd >= spanStart {
				m.set(h*auditSlotsPerHr + s)
			}
		}
	}
	return m
}

func formatSlotRange(slot int) string {
	if slot < 0 || slot >= auditSlotCount {
		return "00:00-00:09"
	}
	h := slot / auditSlotsPerHr
	m := (slot % auditSlotsPerHr) * auditSlotMinutes
	return fmt.Sprintf("%02d:%02d-%02d:%02d", h, m, h, m+auditSlotMinutes-1)
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
