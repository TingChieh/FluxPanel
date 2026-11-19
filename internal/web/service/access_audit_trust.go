package service

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

const (
	// AuditTrustTarget trusts exactly the alert's host and port.
	AuditTrustTarget = "target"
	// AuditTrustHost trusts the alert's host on any port.
	AuditTrustHost = "host"
	// AuditTrustClient exempts the alert's client from account-sharing checks.
	AuditTrustClient = "client"
)

// ErrAuditTrustScope means the requested trust does not fit the alert, such as
// trusting the "host" of an account-sharing alert.
var ErrAuditTrustScope = errors.New("this trust scope does not apply to the alert")

// AuditTrustRequest picks how an alert is trusted: target, host or client.
type AuditTrustRequest struct {
	Scope string `json:"scope" example:"host"`
}

// trustedTarget is one parsed trusted-target entry: a domain suffix, an IP or a
// CIDR, optionally pinned to one port.
type trustedTarget struct {
	host    string
	ip      net.IP
	network *net.IPNet
	port    int
}

// parseTrustedTarget accepts "host", "host:port", "[ipv6]:port", a bare IPv6
// address and CIDRs in any of those positions.
func parseTrustedTarget(entry string) (trustedTarget, bool) {
	var t trustedTarget
	host, portStr := entry, ""
	switch {
	case strings.HasPrefix(entry, "["):
		end := strings.Index(entry, "]")
		if end < 0 {
			return t, false
		}
		host, portStr = entry[1:end], strings.TrimPrefix(entry[end+1:], ":")
		if rest := entry[end+1:]; rest != "" && !strings.HasPrefix(rest, ":") {
			return t, false
		}
	case strings.Count(entry, ":") == 1:
		host, portStr, _ = strings.Cut(entry, ":")
	}
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil || p < 1 || p > 65535 {
			return t, false
		}
		t.port = p
	}
	if host == "" {
		return t, false
	}
	if _, network, err := net.ParseCIDR(host); err == nil {
		t.network = network
	} else if ip := net.ParseIP(host); ip != nil {
		t.ip = ip
	} else {
		t.host = host
	}
	return t, true
}

func (t trustedTarget) matches(domain string, port int) bool {
	if t.port > 0 && t.port != port {
		return false
	}
	switch {
	case t.network != nil:
		ip := net.ParseIP(domain)
		return ip != nil && t.network.Contains(ip)
	case t.ip != nil:
		ip := net.ParseIP(domain)
		return ip != nil && ip.Equal(t.ip)
	default:
		return domainMatchesSuffix(domain, t.host)
	}
}

func isTrustedTarget(cfg AuditConfig, domain string, port int) bool {
	for _, entry := range cfg.TrustedTargets {
		if t, ok := parseTrustedTarget(entry); ok && t.matches(domain, port) {
			return true
		}
	}
	return false
}

func isTrustedClient(cfg AuditConfig, email string) bool {
	for _, c := range cfg.TrustedClients {
		if strings.EqualFold(c, email) {
			return true
		}
	}
	return false
}

// cleanClientList trims and de-duplicates client emails case-insensitively but
// keeps the spelling the admin typed.
func cleanClientList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, raw := range items {
		v := strings.TrimSpace(raw)
		key := strings.ToLower(v)
		if v == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

// splitAlertTarget undoes newTargetAlert's "host:port", where host may itself
// be an IPv6 address and port is always the last segment.
func splitAlertTarget(target string) (host string, port int) {
	if i := strings.LastIndex(target, ":"); i > 0 {
		if p, err := strconv.Atoi(target[i+1:]); err == nil {
			return target[:i], p
		}
	}
	return target, 0
}

// TrustAlert turns an alert into a trust rule and re-evaluates history, so the
// alert and every future one like it disappear.
func (s *AccessAuditService) TrustAlert(id uint, scope string) error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	var alert model.AccessAuditAlert
	if err := db.First(&alert, id).Error; err != nil {
		return err
	}
	cfg := s.GetConfig()
	sharing := alert.AlertType == AuditAlertAccountSharing
	switch scope {
	case AuditTrustClient:
		if !sharing {
			return ErrAuditTrustScope
		}
		cfg.TrustedClients = append(cfg.TrustedClients, alert.Email)
	case AuditTrustTarget, AuditTrustHost:
		if sharing {
			return ErrAuditTrustScope
		}
		host, port := splitAlertTarget(alert.Target)
		entry := host
		if scope == AuditTrustTarget && port > 0 {
			if strings.Contains(host, ":") {
				entry = fmt.Sprintf("[%s]:%d", host, port)
			} else {
				entry = fmt.Sprintf("%s:%d", host, port)
			}
		}
		cfg.TrustedTargets = append(cfg.TrustedTargets, entry)
	default:
		return fmt.Errorf("%w: %q", ErrAuditTrustScope, scope)
	}
	return s.SaveConfig(cfg, true)
}
