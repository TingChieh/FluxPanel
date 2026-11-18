package sub

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

func TestSub_HostMihomoDialerProxy(t *testing.T) {
	seedSubDB(t)
	ibRelay := seedSubInbound(t, "s1", "Relay-Hub", 11701, 1, `{"network":"tcp","security":"none"}`)
	ibExit := seedSubInbound(t, "s1", "Edge-US", 10703, 2, mihomoRealityStream)

	seedHost(t, &model.Host{
		InboundId: ibRelay.Id, SortOrder: 0, Remark: "Relay-Hub", Address: "relay.example.com", Port: 11701,
	})
	seedHost(t, &model.Host{
		InboundId: ibExit.Id, SortOrder: 1, Remark: "Edge-US", Address: "edge.example.com", Port: 443,
		MihomoDialerProxy: " Relay-Hub ",
	})
	seedHost(t, &model.Host{
		InboundId: ibExit.Id, SortOrder: 2, Remark: "SG-Loop", Address: "loop.example.com", Port: 443,
		MihomoDialerProxy: "SG-Loop",
	})

	for _, mode := range []string{"inbound-first", "hosts-first"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "hosts-first" {
				if err := database.GetDB().Where("key = ?", "subHostSortMode").Delete(&model.Setting{}).Error; err != nil {
					t.Fatal(err)
				}
				if err := database.GetDB().Create(&model.Setting{Key: "subHostSortMode", Value: mode}).Error; err != nil {
					t.Fatal(err)
				}
			}
			out, _, err := NewSubClashService(false, "", false, NewSubService("{{HOST}}")).GetClash("s1", "req.example.com")
			if err != nil {
				t.Fatalf("GetClash: %v", err)
			}
			var doc struct {
				Proxies []struct {
					Name        string `yaml:"name"`
					DialerProxy string `yaml:"dialer-proxy"`
				} `yaml:"proxies"`
			}
			if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("decode Clash YAML: %v\n%s", err, out)
			}
			byName := make(map[string]string, len(doc.Proxies))
			for _, p := range doc.Proxies {
				byName[p.Name] = p.DialerProxy
			}
			if got := byName["Edge-US"]; got != "Relay-Hub" {
				t.Fatalf("Edge-US dialer-proxy = %q, want Relay-Hub\n%s", got, out)
			}
			if got := byName["SG-Loop"]; got != "" {
				t.Fatalf("self-referencing dialer-proxy should be omitted, got %q", got)
			}
		})
	}

	rawLinks, _, _, _, err := NewSubService("").GetSubs("s1", "req.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if strings.Contains(strings.Join(rawLinks, "\n"), "dialer-proxy") {
		t.Fatalf("raw share links must not include Clash dialer-proxy: %v", rawLinks)
	}
}

func TestSub_HostMihomoDialerProxyExpandsRemarkVars(t *testing.T) {
	seedSubDB(t)
	ibExit := seedSubInbound(t, "s-tmpl", "Edge-US", 10705, 1, mihomoRealityStream)
	seedHost(t, &model.Host{
		InboundId: ibExit.Id, SortOrder: 1, Remark: "Edge-US", Address: "edge.example.com", Port: 443,
		MihomoDialerProxy: "Relay-Hub-{{EMAIL}}",
	})

	out, _, err := NewSubClashService(false, "", false, NewSubService("{{HOST}}-{{EMAIL}}")).GetClash("s-tmpl", "req.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	var doc struct {
		Proxies []struct {
			Name        string `yaml:"name"`
			DialerProxy string `yaml:"dialer-proxy"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode Clash YAML: %v\n%s", err, out)
	}
	if len(doc.Proxies) != 1 || doc.Proxies[0].Name != "Edge-US-Edge-US@e" || doc.Proxies[0].DialerProxy != "Relay-Hub-Edge-US@e" {
		t.Fatalf("unexpected proxies = %+v\n%s", doc.Proxies, out)
	}
}

func TestSub_HostMihomoDialerProxyOmittedInLegacyClash(t *testing.T) {
	seedSubDB(t)
	ib := seedSubInbound(t, "s-leg", "vm", 4451, 1, wsTLSStream)
	ib.Protocol = model.VMESS
	ib.Settings = `{"clients":[{"id":"11111111-2222-4333-8444-000000004451","email":"vm@e","subId":"s-leg","security":"auto","enable":true}]}`
	if err := database.GetDB().Save(ib).Error; err != nil {
		t.Fatal(err)
	}
	seedHost(t, &model.Host{
		InboundId: ib.Id, SortOrder: 1, Remark: "RelayEdge", Address: "edge.example.com", Port: 443,
		Security: "tls", MihomoDialerProxy: "Relay-Hub",
	})

	out, _, err := NewSubClashService(false, "", false, NewSubService("")).GetClashLegacy("s-leg", "req.example.com")
	if err != nil {
		t.Fatalf("GetClashLegacy: %v", err)
	}
	if strings.Contains(out, "dialer-proxy") {
		t.Fatalf("legacy Clash YAML must omit Mihomo dialer-proxy:\n%s", out)
	}
}

func TestSub_ExternalLinkClashDialerProxy(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "s-ext", "Relay-Hub", 11702, 1, wsTLSStream)

	db := database.GetDB()
	var rec model.ClientRecord
	if err := db.Where("email = ?", "Relay-Hub@e").First(&rec).Error; err != nil {
		t.Fatal(err)
	}
	ext := model.ClientExternalLink{
		ClientId:    rec.Id,
		Kind:        model.ExternalLinkKindLink,
		Value:       clashExternalVlessLink + "#Edge-US",
		Remark:      "Edge-US",
		DialerProxy: " Relay-Hub ",
		SortIndex:   1,
	}
	if err := db.Create(&ext).Error; err != nil {
		t.Fatal(err)
	}

	out, _, err := NewSubClashService(false, "", false, NewSubService("")).GetClash("s-ext", "req.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	var doc struct {
		Proxies []struct {
			Name        string `yaml:"name"`
			DialerProxy string `yaml:"dialer-proxy"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode Clash YAML: %v\n%s", err, out)
	}
	var got string
	for _, p := range doc.Proxies {
		if p.Name == "Edge-US" {
			got = p.DialerProxy
		}
	}
	if got != "Relay-Hub" {
		t.Fatalf("external proxy dialer-proxy = %q, want Relay-Hub\n%s", got, out)
	}
}
