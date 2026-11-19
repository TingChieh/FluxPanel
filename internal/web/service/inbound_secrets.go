package service

import (
	"errors"
	"fmt"

	"github.com/TingChieh/FluxPanel/internal/crypto/nodetoken"
	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

// MigrateInboundSecretsToActiveKey re-encrypts every inbound's settings and
// stream_settings under the configured active key (idempotent).
func (s *InboundService) MigrateInboundSecretsToActiveKey() (int, int, error) {
	codec := nodetoken.Active()
	if !codec.Enabled() {
		return 0, 0, errors.New("node-token encryption is off; set NODE_TOKEN_ENCRYPTION=migration|required and a key first")
	}
	db := database.GetDB()
	var rows []struct {
		Id             int
		Settings       string
		StreamSettings string
	}
	if err := db.Table("inbounds").Select("id, settings, stream_settings").Order("id asc").Scan(&rows).Error; err != nil {
		return 0, 0, err
	}
	changed, skipped := 0, 0
	for _, r := range rows {
		updates := map[string]any{}
		if r.Settings != "" {
			if codec.EncryptedWithActive(r.Settings) {
				if _, err := model.DecryptInboundSettings(r.Settings); err != nil {
					return changed, skipped, fmt.Errorf("inbound %d validate active settings: %w", r.Id, err)
				}
			} else {
				plain, err := model.DecryptInboundSettings(r.Settings)
				if err != nil {
					return changed, skipped, fmt.Errorf("inbound %d decrypt settings: %w", r.Id, err)
				}
				enc, err := model.EncryptInboundSettings(plain)
				if err != nil {
					return changed, skipped, fmt.Errorf("inbound %d encrypt settings: %w", r.Id, err)
				}
				updates["settings"] = enc
			}
		}
		if r.StreamSettings != "" {
			if codec.EncryptedWithActive(r.StreamSettings) {
				if _, err := model.DecryptInboundStreamSettings(r.StreamSettings); err != nil {
					return changed, skipped, fmt.Errorf("inbound %d validate active stream_settings: %w", r.Id, err)
				}
			} else {
				plain, err := model.DecryptInboundStreamSettings(r.StreamSettings)
				if err != nil {
					return changed, skipped, fmt.Errorf("inbound %d decrypt stream_settings: %w", r.Id, err)
				}
				enc, err := model.EncryptInboundStreamSettings(plain)
				if err != nil {
					return changed, skipped, fmt.Errorf("inbound %d encrypt stream_settings: %w", r.Id, err)
				}
				updates["stream_settings"] = enc
			}
		}
		if len(updates) == 0 {
			skipped++
			continue
		}
		res := db.Model(&model.Inbound{}).
			Where("id = ? AND settings = ? AND stream_settings = ?", r.Id, r.Settings, r.StreamSettings).
			Updates(updates)
		if res.Error != nil {
			return changed, skipped, res.Error
		}
		if res.RowsAffected == 1 {
			changed++
		} else {
			skipped++
		}
	}
	return changed, skipped, nil
}
