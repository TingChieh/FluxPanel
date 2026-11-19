package model

import (
	"github.com/TingChieh/FluxPanel/internal/crypto/nodetoken"
)

var (
	inboundSettingsAAD       = []byte("inbounds/settings")
	inboundStreamSettingsAAD = []byte("inbounds/stream_settings")
)

// EncryptInboundSettings encrypts an inbound settings JSON blob when at-rest
// encryption is enabled; returns raw unchanged in ModeOff or when empty.
func EncryptInboundSettings(raw string) (string, error) {
	return nodetoken.EncryptBound(inboundSettingsAAD, raw)
}

// DecryptInboundSettings transparently decrypts an encrypted inbound settings
// blob and passes legacy plaintext through unchanged.
func DecryptInboundSettings(stored string) (string, error) {
	return nodetoken.DecryptBound(inboundSettingsAAD, stored)
}

// EncryptInboundStreamSettings encrypts an inbound stream_settings JSON blob
// when at-rest encryption is enabled.
func EncryptInboundStreamSettings(raw string) (string, error) {
	return nodetoken.EncryptBound(inboundStreamSettingsAAD, raw)
}

// DecryptInboundStreamSettings transparently decrypts an encrypted inbound
// stream_settings blob and passes legacy plaintext through unchanged.
func DecryptInboundStreamSettings(stored string) (string, error) {
	return nodetoken.DecryptBound(inboundStreamSettingsAAD, stored)
}

// DecryptInboundSecrets decrypts an Inbound's settings and stream_settings in place.
func DecryptInboundSecrets(i *Inbound) error {
	if i == nil {
		return nil
	}
	if nodetoken.IsEncrypted(i.Settings) {
		pt, err := DecryptInboundSettings(i.Settings)
		if err != nil {
			return err
		}
		i.Settings = pt
	}
	if nodetoken.IsEncrypted(i.StreamSettings) {
		pt, err := DecryptInboundStreamSettings(i.StreamSettings)
		if err != nil {
			return err
		}
		i.StreamSettings = pt
	}
	return nil
}
