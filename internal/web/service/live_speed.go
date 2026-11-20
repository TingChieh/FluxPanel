package service

import (
	"cmp"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/TingChieh/FluxPanel/internal/xray"
)

const liveSpeedStaleWindow = 15 * time.Second

// LiveSpeed holds instantaneous upload and download rates in bytes per second.
type LiveSpeed struct {
	Up   int64 `json:"up" example:"131072"`
	Down int64 `json:"down" example:"1048576"`
}

// LiveSpeedSnapshot aggregates active live rates across the local panel and all synced nodes.
type LiveSpeedSnapshot struct {
	Total LiveSpeed
	Users map[string]LiveSpeed
	Nodes map[int]LiveSpeed
}

type liveSpeedSource struct {
	updatedAt time.Time
	nodeSpeed LiveSpeed
	users     map[string]LiveSpeed
}

var (
	liveSpeedMu      sync.RWMutex
	liveSpeedSources = make(map[int]*liveSpeedSource)
)

// ResetLiveSpeedsForTest clears all tracked live speeds between tests.
func ResetLiveSpeedsForTest() {
	liveSpeedMu.Lock()
	liveSpeedSources = make(map[int]*liveSpeedSource)
	liveSpeedMu.Unlock()
}

// UpdateLocalLiveSpeeds records local panel (nodeID 0) inbound and per-client rates from a poll delta.
func UpdateLocalLiveSpeeds(now time.Time, windowSec float64, inboundTraffics []*xray.Traffic, clientTraffics []*xray.ClientTraffic) {
	if windowSec <= 0 {
		windowSec = 5
	}
	if now.IsZero() {
		now = time.Now()
	}
	var inUp, inDown int64
	for _, tr := range inboundTraffics {
		if tr == nil || !tr.IsInbound {
			continue
		}
		if tr.Up > 0 {
			inUp += tr.Up
		}
		if tr.Down > 0 {
			inDown += tr.Down
		}
	}
	users := make(map[string]LiveSpeed, len(clientTraffics))
	for _, ct := range clientTraffics {
		if ct == nil || ct.Email == "" {
			continue
		}
		upRate := int64(float64(max(ct.Up, 0)) / windowSec)
		downRate := int64(float64(max(ct.Down, 0)) / windowSec)
		if upRate == 0 && downRate == 0 {
			continue
		}
		cur := users[ct.Email]
		cur.Up += upRate
		cur.Down += downRate
		users[ct.Email] = cur
	}

	nodeSpeed := LiveSpeed{
		Up:   int64(float64(inUp) / windowSec),
		Down: int64(float64(inDown) / windowSec),
	}
	liveSpeedMu.Lock()
	liveSpeedSources[0] = &liveSpeedSource{
		updatedAt: now,
		nodeSpeed: nodeSpeed,
		users:     users,
	}
	liveSpeedMu.Unlock()
}

// UpdateNodeLiveSpeeds records normalized bytes/sec for synced remote nodes and their active clients.
func UpdateNodeLiveSpeeds(now time.Time, nodeSpeeds map[int]LiveSpeed, nodeUserSpeeds map[int]map[string]LiveSpeed) {
	if now.IsZero() {
		now = time.Now()
	}
	liveSpeedMu.Lock()
	defer liveSpeedMu.Unlock()

	seen := make(map[int]struct{}, len(nodeSpeeds)+len(nodeUserSpeeds))
	for id := range nodeSpeeds {
		if id > 0 {
			seen[id] = struct{}{}
		}
	}
	for id := range nodeUserSpeeds {
		if id > 0 {
			seen[id] = struct{}{}
		}
	}
	for id := range seen {
		usersCopy := make(map[string]LiveSpeed)
		for email, sp := range nodeUserSpeeds[id] {
			if email == "" || (sp.Up <= 0 && sp.Down <= 0) {
				continue
			}
			usersCopy[email] = LiveSpeed{Up: max(sp.Up, 0), Down: max(sp.Down, 0)}
		}
		ns := nodeSpeeds[id]
		liveSpeedSources[id] = &liveSpeedSource{
			updatedAt: now,
			nodeSpeed: LiveSpeed{Up: max(ns.Up, 0), Down: max(ns.Down, 0)},
			users:     usersCopy,
		}
	}
}

// GetLiveSpeedSnapshot merges non-stale sources (<=15s) across local and remote nodes.
func GetLiveSpeedSnapshot(now time.Time) LiveSpeedSnapshot {
	if now.IsZero() {
		now = time.Now()
	}
	snap := LiveSpeedSnapshot{
		Users: make(map[string]LiveSpeed),
		Nodes: make(map[int]LiveSpeed),
	}

	liveSpeedMu.RLock()
	defer liveSpeedMu.RUnlock()

	for nodeID, src := range liveSpeedSources {
		if src == nil || now.Sub(src.updatedAt) > liveSpeedStaleWindow {
			continue
		}
		if src.nodeSpeed.Up > 0 || src.nodeSpeed.Down > 0 {
			snap.Nodes[nodeID] = src.nodeSpeed
			snap.Total.Up += src.nodeSpeed.Up
			snap.Total.Down += src.nodeSpeed.Down
		}
		for email, sp := range src.users {
			cur := snap.Users[email]
			cur.Up += sp.Up
			cur.Down += sp.Down
			snap.Users[email] = cur
		}
	}
	return snap
}

// AttachLiveSpeedPayload adds liveTotalSpeed, liveNodeSpeeds, and capped liveUserSpeeds to a WS traffic map.
func AttachLiveSpeedPayload(payload map[string]any, now time.Time, maxUsers int) {
	snap := GetLiveSpeedSnapshot(now)
	payload["liveTotalSpeed"] = snap.Total
	payload["liveNodeSpeeds"] = snap.Nodes
	if maxUsers <= 0 || len(snap.Users) <= maxUsers {
		payload["liveUserSpeeds"] = snap.Users
		return
	}
	type ranked struct {
		email string
		speed LiveSpeed
	}
	items := make([]ranked, 0, len(snap.Users))
	for email, sp := range snap.Users {
		items = append(items, ranked{email: email, speed: sp})
	}
	slices.SortFunc(items, func(a, b ranked) int {
		if c := cmp.Compare(b.speed.Up+b.speed.Down, a.speed.Up+a.speed.Down); c != 0 {
			return c
		}
		return cmp.Compare(a.email, b.email)
	})
	capped := make(map[string]LiveSpeed, maxUsers)
	for _, it := range items[:maxUsers] {
		capped[it.email] = it.speed
	}
	payload["liveUserSpeeds"] = capped
}

// CloneLiveSpeedUsers returns a shallow copy of user speeds from a snapshot.
func (s LiveSpeedSnapshot) CloneUsers() map[string]LiveSpeed {
	out := make(map[string]LiveSpeed, len(s.Users))
	maps.Copy(out, s.Users)
	return out
}
