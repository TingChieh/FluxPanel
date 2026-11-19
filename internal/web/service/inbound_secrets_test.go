package service

import (
	"crypto/rand"
	"strings"
	"testing"

	"github.com/TingChieh/FluxPanel/internal/crypto/nodetoken"
	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/web/runtime"
	"github.com/TingChieh/FluxPanel/internal/xray"
)

func enableTestInboundEncryption(t *testing.T) {
	t.Helper()
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	ring := &nodetoken.Keyring{ActiveID: "k1", Keys: map[string][32]byte{"k1": k}}
	codec, err := nodetoken.NewCodec(nodetoken.ModeRequired, ring)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	nodetoken.Init(codec)
	t.Cleanup(func() {
		off, _ := nodetoken.NewCodec(nodetoken.ModeOff, nil)
		nodetoken.Init(off)
	})
}

func rawInboundSecretColumns(t *testing.T, id int) (settings, stream string) {
	t.Helper()
	var row struct {
		Settings       string
		StreamSettings string
	}
	if err := database.GetDB().Table("inbounds").
		Select("settings, stream_settings").
		Where("id = ?", id).
		Scan(&row).Error; err != nil {
		t.Fatalf("raw query inbound %d: %v", id, err)
	}
	return row.Settings, row.StreamSettings
}

func TestInboundSecrets_EncryptedAtRestAndTransparentlyRead(t *testing.T) {
	setupConflictDB(t)
	enableTestInboundEncryption(t)
	db := database.GetDB()
	ibSvc := InboundService{}

	if err := db.Create(&model.Node{Id: 1, Name: "hk-1", Address: "198.51.100.11", Port: 2053, ApiToken: "t", Guid: "g-hk1", Enable: true}).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	rawSettings := `{"clients":[{"email":"alice","id":"11111111-1111-1111-1111-111111111111","enable":true}]}`
	rawStream := `{"network":"tcp","security":"reality","realitySettings":{"privateKey":"super-secret-reality-key","shortIds":["a1b2"]}}`
	snap := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{
			Tag:            "n1-vless",
			Enable:         true,
			Port:           11703,
			Protocol:       model.VLESS,
			Settings:       rawSettings,
			StreamSettings: rawStream,
			ClientStats:    []xray.ClientTraffic{{Email: "alice", Enable: true, Up: 10, Down: 20}},
		}},
	}
	if _, err := ibSvc.SetRemoteTraffic(1, snap, false, false); err != nil {
		t.Fatalf("SetRemoteTraffic: %v", err)
	}

	var ib model.Inbound
	if err := db.Where("tag = ?", "n1-vless").First(&ib).Error; err != nil {
		t.Fatalf("load inbound: %v", err)
	}
	if !strings.Contains(ib.Settings, "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("AfterFind did not decrypt settings: %s", ib.Settings)
	}
	if !strings.Contains(ib.StreamSettings, "super-secret-reality-key") {
		t.Fatalf("AfterFind did not decrypt streamSettings: %s", ib.StreamSettings)
	}

	storedSettings, storedStream := rawInboundSecretColumns(t, ib.Id)
	if !nodetoken.IsEncrypted(storedSettings) || strings.Contains(storedSettings, "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("settings stored in plaintext: %s", storedSettings)
	}
	if !nodetoken.IsEncrypted(storedStream) || strings.Contains(storedStream, "super-secret-reality-key") {
		t.Fatalf("stream_settings stored in plaintext: %s", storedStream)
	}

	emails, err := ibSvc.GetAllEmails()
	if err != nil {
		t.Fatalf("GetAllEmails: %v", err)
	}
	if len(emails) != 1 || emails[0] != "alice" {
		t.Fatalf("GetAllEmails = %v, want [alice]", emails)
	}

	opts, err := ibSvc.GetInboundOptions(ib.UserId)
	if err != nil {
		t.Fatalf("GetInboundOptions: %v", err)
	}
	if len(opts) != 1 || opts[0].Security != "reality" {
		t.Fatalf("GetInboundOptions = %+v, want security=reality", opts)
	}
}

func TestInboundSecrets_MigrateLegacyPlaintext(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()
	ibSvc := InboundService{}

	// Seed legacy plaintext row while encryption is off.
	ib := &model.Inbound{
		UserId:         1,
		Tag:            "legacy-1",
		Enable:         true,
		Port:           11702,
		Protocol:       model.Shadowsocks,
		Settings:       `{"method":"2022-blake3-aes-256-gcm","password":"legacy-ss-password","clients":[]}`,
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("seed legacy inbound: %v", err)
	}
	if rawS, _ := rawInboundSecretColumns(t, ib.Id); nodetoken.IsEncrypted(rawS) {
		t.Fatalf("expected plaintext before encryption enabled")
	}

	enableTestInboundEncryption(t)
	changed, skipped, err := ibSvc.MigrateInboundSecretsToActiveKey()
	if err != nil {
		t.Fatalf("MigrateInboundSecretsToActiveKey: %v", err)
	}
	if changed != 1 || skipped != 0 {
		t.Fatalf("first migration = (%d,%d), want (1,0)", changed, skipped)
	}

	storedS, storedSt := rawInboundSecretColumns(t, ib.Id)
	if !nodetoken.IsEncrypted(storedS) || strings.Contains(storedS, "legacy-ss-password") {
		t.Fatalf("settings not encrypted after migration: %s", storedS)
	}
	if !nodetoken.IsEncrypted(storedSt) {
		t.Fatalf("stream_settings not encrypted after migration: %s", storedSt)
	}

	changed, skipped, err = ibSvc.MigrateInboundSecretsToActiveKey()
	if err != nil {
		t.Fatalf("second MigrateInboundSecretsToActiveKey: %v", err)
	}
	if changed != 0 || skipped != 1 {
		t.Fatalf("second migration = (%d,%d), want (0,1)", changed, skipped)
	}

	reloaded, err := ibSvc.GetInbound(ib.Id)
	if err != nil {
		t.Fatalf("GetInbound: %v", err)
	}
	if !strings.Contains(reloaded.Settings, "legacy-ss-password") {
		t.Fatalf("decrypted settings mismatch: %s", reloaded.Settings)
	}
}

// The write callback encrypts the caller's struct in place; a rejected insert
// must still hand it back as plaintext so a retry or error path never sees ciphertext.
func TestInboundSecrets_FailedWriteRestoresCallerPlaintext(t *testing.T) {
	setupConflictDB(t)
	enableTestInboundEncryption(t)
	db := database.GetDB()

	first := &model.Inbound{Tag: "dup-tag", Port: 20001, Protocol: model.VLESS, Enable: true, Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp"}`}
	if err := db.Create(first).Error; err != nil {
		t.Fatalf("create first: %v", err)
	}
	const settings = `{"clients":[],"decryption":"none"}`
	const stream = `{"network":"tcp","security":"reality","realitySettings":{"privateKey":"k"}}`
	second := &model.Inbound{Tag: "dup-tag", Port: 20002, Protocol: model.VLESS, Enable: true, Settings: settings, StreamSettings: stream}
	if err := db.Create(second).Error; err == nil {
		t.Fatal("precondition: duplicate tag insert must fail")
	}
	if second.Settings != settings || second.StreamSettings != stream {
		t.Fatalf("after failed create caller holds settings=%q stream=%q, want original plaintext", second.Settings, second.StreamSettings)
	}
}
