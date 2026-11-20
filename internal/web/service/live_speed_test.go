package service

import (
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/xray"
)

func TestLiveSpeedTracker_AggregatesLocalAndRemotePerUserAndTotal(t *testing.T) {
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	UpdateLocalLiveSpeeds(
		now,
		5,
		[]*xray.Traffic{{IsInbound: true, Tag: "local-1", Up: 500_000, Down: 1_500_000}},
		[]*xray.ClientTraffic{{Email: "alice", Up: 500_000, Down: 1_000_000}},
	)
	UpdateNodeLiveSpeeds(
		now,
		map[int]LiveSpeed{
			1: {Up: 250_000, Down: 500_000},
		},
		map[int]map[string]LiveSpeed{
			1: {
				"alice": {Up: 200_000, Down: 400_000},
				"bob":   {Up: 50_000, Down: 100_000},
			},
		},
	)

	snap := GetLiveSpeedSnapshot(now.Add(2 * time.Second))
	if got := snap.Nodes[0]; got != (LiveSpeed{Up: 100_000, Down: 300_000}) {
		t.Fatalf("local node speed = %+v, want {100000 300000}", got)
	}
	if got := snap.Nodes[1]; got != (LiveSpeed{Up: 250_000, Down: 500_000}) {
		t.Fatalf("node 1 speed = %+v, want {250000 500000}", got)
	}
	if got := snap.Total; got != (LiveSpeed{Up: 350_000, Down: 800_000}) {
		t.Fatalf("total speed = %+v, want {350000 800000}", got)
	}
	if got := snap.Users["alice"]; got != (LiveSpeed{Up: 300_000, Down: 600_000}) {
		t.Fatalf("alice speed = %+v, want {300000 600000}", got)
	}
	if got := snap.Users["bob"]; got != (LiveSpeed{Up: 50_000, Down: 100_000}) {
		t.Fatalf("bob speed = %+v, want {50000 100000}", got)
	}
}

func TestLiveSpeedTracker_ExpiresStaleEntriesAfter15s(t *testing.T) {
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)

	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	UpdateLocalLiveSpeeds(
		t0,
		5,
		[]*xray.Traffic{{IsInbound: true, Tag: "local-1", Up: 500, Down: 1000}},
		[]*xray.ClientTraffic{{Email: "alice", Up: 500, Down: 1000}},
	)
	UpdateNodeLiveSpeeds(
		t0.Add(10*time.Second),
		map[int]LiveSpeed{1: {Up: 20, Down: 40}},
		map[int]map[string]LiveSpeed{1: {"bob": {Up: 20, Down: 40}}},
	)

	snap := GetLiveSpeedSnapshot(t0.Add(16 * time.Second))
	if _, ok := snap.Nodes[0]; ok {
		t.Fatalf("stale local node speed should expire after 15s")
	}
	if _, ok := snap.Users["alice"]; ok {
		t.Fatalf("stale local user speed should expire after 15s")
	}
	if got := snap.Nodes[1]; got != (LiveSpeed{Up: 20, Down: 40}) {
		t.Fatalf("fresh node 1 speed = %+v, want {20 40}", got)
	}
	if got := snap.Total; got != (LiveSpeed{Up: 20, Down: 40}) {
		t.Fatalf("total speed = %+v, want {20 40}", got)
	}
}

func TestLiveSpeedTracker_ZeroDeltaClearsActiveUser(t *testing.T) {
	ResetLiveSpeedsForTest()
	t.Cleanup(ResetLiveSpeedsForTest)

	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	UpdateLocalLiveSpeeds(
		t0,
		5,
		[]*xray.Traffic{{IsInbound: true, Tag: "local-1", Up: 500, Down: 1000}},
		[]*xray.ClientTraffic{{Email: "alice", Up: 500, Down: 1000}},
	)
	UpdateLocalLiveSpeeds(t0.Add(5*time.Second), 5, nil, nil)

	snap := GetLiveSpeedSnapshot(t0.Add(6 * time.Second))
	if len(snap.Users) != 0 {
		t.Fatalf("expected active users cleared on zero delta, got %+v", snap.Users)
	}
	if snap.Total != (LiveSpeed{}) {
		t.Fatalf("expected total speed zero, got %+v", snap.Total)
	}
}
