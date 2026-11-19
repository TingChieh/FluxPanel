package sub

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

func TestInboundSettingsCacheHitExpiryAndPurge(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	cache := newInboundSettingsCache(10 * time.Second)
	cache.now = func() time.Time { return now }
	var loads atomic.Int32
	load := func() (string, error) {
		loads.Add(1)
		return `{"clients":[{"email":"a@e"}],"decryption":"none"}`, nil
	}

	got, err := cache.settingsFor(1, load)
	if err != nil {
		t.Fatalf("miss: %v", err)
	}
	if got != `{"decryption":"none"}` {
		t.Fatalf("settings = %s, want the clients array dropped", got)
	}
	if _, err := cache.settingsFor(1, load); err != nil || loads.Load() != 1 {
		t.Fatalf("hit reloaded: loads = %d, err = %v", loads.Load(), err)
	}
	now = now.Add(11 * time.Second)
	if _, err := cache.settingsFor(1, load); err != nil || loads.Load() != 2 {
		t.Fatalf("expired entry must reload: loads = %d, err = %v", loads.Load(), err)
	}
	cache.purgeAll()
	if _, err := cache.settingsFor(1, load); err != nil || loads.Load() != 3 {
		t.Fatalf("purged entry must reload: loads = %d, err = %v", loads.Load(), err)
	}
}

func TestInboundSettingsCacheSingleFlight(t *testing.T) {
	cache := newInboundSettingsCache(time.Minute)
	release := make(chan struct{})
	var loads atomic.Int32
	load := func() (string, error) {
		loads.Add(1)
		<-release
		return `{"method":"aes-128-gcm"}`, nil
	}

	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			if got, err := cache.settingsFor(7, load); err != nil || got != `{"method":"aes-128-gcm"}` {
				t.Errorf("settingsFor = %q, %v", got, err)
			}
		})
	}
	// Let every caller reach the shared load before releasing it.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatalf("%d concurrent misses ran %d loads, want 1", callers, loads.Load())
	}
}

// A load that a panel edit overlaps may have read the row before the edit; it is served once but
// must not be kept, or the edit would stay invisible for a whole TTL.
func TestInboundSettingsCacheDropsALoadOverlappingAPurge(t *testing.T) {
	cache := newInboundSettingsCache(time.Minute)
	var loads atomic.Int32
	load := func() (string, error) {
		if loads.Add(1) == 1 {
			cache.purgeAll()
		}
		return `{"decryption":"none"}`, nil
	}
	if _, err := cache.settingsFor(3, load); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if _, err := cache.settingsFor(3, load); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if loads.Load() != 2 {
		t.Fatalf("loads = %d, want 2: a load that overlapped a purge was kept", loads.Load())
	}
}

const (
	parityServerKey = "c2VydmVya2V5MTIzNDU2Nw=="
	parityClientKey = "Y2xpZW50a2V5MTIzNDU2Nw=="
)

// seedParitySubscription gives subscriber "par" a VLESS, a Shadowsocks 2022 and a WireGuard
// inbound, whose links read the top-level settings, the server key and the peers respectively.
func seedParitySubscription(t *testing.T) {
	t.Helper()
	db := database.GetDB()
	seedSubProtocolInbound(t, "par", "pv", 4701, 1, `{"network":"tcp","security":"tls","tlsSettings":{"serverName":"base.sni"}}`, model.VLESS)
	ss := seedSubProtocolInbound(t, "par", "ps", 4702, 2, `{"network":"tcp","security":"none"}`, model.Shadowsocks)
	ssSettings := fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q,"network":"tcp,udp","clients":[{"email":"ps@e","password":%q,"subId":"par","enable":true}]}`, parityServerKey, parityClientKey)
	if err := db.Model(ss).Update("settings", ssSettings).Error; err != nil {
		t.Fatalf("shadowsocks settings: %v", err)
	}
	if err := db.Model(&model.ClientRecord{}).Where("email = ?", "ps@e").Update("password", parityClientKey).Error; err != nil {
		t.Fatalf("shadowsocks client password: %v", err)
	}
	serverPriv, _ := mustWireguardKeypair(t)
	clientPriv, _ := mustWireguardKeypair(t)
	wgSettings := fmt.Sprintf(`{"secretKey":%q,"clients":[{"email":"pw@e","privateKey":%q,"allowedIPs":["10.0.0.2/32"],"enable":true}]}`, serverPriv, clientPriv)
	seedTunnelSubInbound(t, model.WireGuard, "pw", "par", "pw@e", wgSettings, 4703)
}

// renderSubscription is everything a subscriber can fetch: the raw links, the JSON and the Clash body.
func renderSubscription(t *testing.T, svc *SubService) []string {
	t.Helper()
	req := svc.ForRequest("req.example.com")
	req.subscriptionBody = true
	links, _, _, _, err := req.getSubs("par")
	if err != nil {
		t.Fatalf("getSubs: %v", err)
	}
	if len(links) != 3 {
		t.Fatalf("links = %d, want one per inbound: %v", len(links), links)
	}
	jsonBody, _, err := NewSubJsonService("", "", "", "", svc).GetJson("par", "req.example.com", false)
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	clashBody, _, err := NewSubClashService(false, "", false, svc).GetClash("par", "req.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	return []string{strings.Join(links, "\n"), jsonBody, clashBody}
}

// The cache hands generators settings without the clients array; every format must still render
// exactly what the full settings render, from the miss and from the hit alike.
func TestSettingsCacheRendersTheSameSubscription(t *testing.T) {
	seedSubDB(t)
	seedParitySubscription(t)

	want := renderSubscription(t, NewSubService(""))
	cached := NewSubService("")
	t.Cleanup(cached.enableSettingsCache())
	for round := range 2 {
		got := renderSubscription(t, cached)
		for i, format := range []string{"raw", "json", "clash"} {
			if got[i] != want[i] {
				t.Fatalf("round %d %s differs with the settings cache:\n got: %s\nwant: %s", round, format, got[i], want[i])
			}
		}
	}
	if !strings.Contains(want[0], "ss://") {
		t.Fatalf("raw subscription lost its shadowsocks link: %s", want[0])
	}
	if n := len(cached.settingsCache.entries); n != 2 {
		t.Fatalf("cached %d inbounds, want 2: VLESS and Shadowsocks, never the WireGuard peers", n)
	}
}
