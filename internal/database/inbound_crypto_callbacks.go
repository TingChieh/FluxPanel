package database

import (
	"reflect"

	"github.com/TingChieh/FluxPanel/internal/crypto/nodetoken"
	"github.com/TingChieh/FluxPanel/internal/database/model"

	"gorm.io/gorm"
)

var inboundModelType = reflect.TypeOf(model.Inbound{})

// registerInboundCryptoCallbacks transparently encrypts inbounds.settings and
// stream_settings on write and decrypts on read for both pointer and value models.
func registerInboundCryptoCallbacks(gormDB *gorm.DB) error {
	if err := gormDB.Callback().Create().Before("gorm:create").Register("fluxpanel:encrypt_inbound_create", encryptInboundBeforeWrite); err != nil {
		return err
	}
	if err := gormDB.Callback().Create().After("gorm:create").Register("fluxpanel:decrypt_inbound_after_create", decryptInboundAfterOp); err != nil {
		return err
	}
	if err := gormDB.Callback().Update().Before("gorm:update").Register("fluxpanel:encrypt_inbound_update", encryptInboundBeforeWrite); err != nil {
		return err
	}
	if err := gormDB.Callback().Update().After("gorm:update").Register("fluxpanel:decrypt_inbound_after_update", decryptInboundAfterOp); err != nil {
		return err
	}
	if err := gormDB.Callback().Query().After("gorm:after_query").Register("fluxpanel:decrypt_inbound_query", decryptInboundAfterOp); err != nil {
		return err
	}
	return nil
}

func isInboundStmt(tx *gorm.DB) bool {
	return tx != nil && tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.ModelType == inboundModelType
}

func encryptInboundBeforeWrite(tx *gorm.DB) {
	if !nodetoken.Enabled() || !isInboundStmt(tx) || tx.Error != nil {
		return
	}
	if m, ok := tx.Statement.Dest.(map[string]any); ok {
		if raw, exists := m["settings"]; exists {
			if s, ok := raw.(string); ok && s != "" {
				enc, err := model.EncryptInboundSettings(s)
				if err != nil {
					_ = tx.AddError(err)
					return
				}
				m["settings"] = enc
			}
		}
		if raw, exists := m["stream_settings"]; exists {
			if s, ok := raw.(string); ok && s != "" {
				enc, err := model.EncryptInboundStreamSettings(s)
				if err != nil {
					_ = tx.AddError(err)
					return
				}
				m["stream_settings"] = enc
			}
		}
		return
	}
	err := visitInboundValues(tx.Statement.ReflectValue, func(ib *model.Inbound) error {
		if ib.Settings != "" {
			enc, err := model.EncryptInboundSettings(ib.Settings)
			if err != nil {
				return err
			}
			ib.Settings = enc
		}
		if ib.StreamSettings != "" {
			enc, err := model.EncryptInboundStreamSettings(ib.StreamSettings)
			if err != nil {
				return err
			}
			ib.StreamSettings = enc
		}
		return nil
	})
	if err != nil {
		_ = tx.AddError(err)
	}
}

// decryptInboundAfterOp also runs after a failed write: the encrypt callback
// mutated the caller's struct in place and must hand it back as plaintext.
func decryptInboundAfterOp(tx *gorm.DB) {
	if !isInboundStmt(tx) {
		return
	}
	if err := visitInboundValues(tx.Statement.ReflectValue, model.DecryptInboundSecrets); err != nil {
		_ = tx.AddError(err)
	}
}

func visitInboundValues(rv reflect.Value, fn func(*model.Inbound) error) error {
	if !rv.IsValid() {
		return nil
	}
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		if rv.Type() == inboundModelType && rv.CanAddr() {
			if ib, ok := rv.Addr().Interface().(*model.Inbound); ok {
				return fn(ib)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if err := visitInboundValues(rv.Index(i), fn); err != nil {
				return err
			}
		}
	}
	return nil
}
