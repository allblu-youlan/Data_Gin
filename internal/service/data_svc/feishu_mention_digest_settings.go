package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FeishuMentionDigestSettingsInput struct {
	DigestTime string `json:"digestTime"`
}

type FeishuMentionDigestSettingsDTO struct {
	DigestTime string `json:"digestTime"`
	Timezone   string `json:"timezone"`
}

func mentionDigestSettingsKey(appID string) string {
	return fmt.Sprintf("feishu_mention_digest:%x", sha256.Sum256([]byte(appID)))
}

func validMentionDigestTime(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

// Read every time so receivers, planners and the admin API share changes
// immediately, including when they run in separate processes.
func loadMentionDigestSettings(ctx context.Context, db *gorm.DB, appID, fallback string) (FeishuMentionDigestSettingsDTO, error) {
	if fallback == "" {
		fallback = "18:00"
	}
	settings := FeishuMentionDigestSettingsDTO{DigestTime: fallback, Timezone: "Asia/Shanghai"}
	if db == nil {
		return settings, fmt.Errorf("feishu mention: digest settings database unavailable")
	}
	if appID == "" {
		return settings, nil
	}
	raw, exists, err := data_dao.NewRuntimeConfigDAO(db).GetValue(ctx, mentionDigestSettingsKey(appID))
	if err != nil {
		return settings, fmt.Errorf("feishu mention: read digest settings: %w", err)
	}
	if !exists {
		return settings, nil
	}
	var stored FeishuMentionDigestSettingsDTO
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || !validMentionDigestTime(stored.DigestTime) || stored.Timezone != "Asia/Shanghai" {
		return stored, fmt.Errorf("feishu mention: invalid stored digest settings")
	}
	return stored, nil
}

func (s *FeishuMentionAdminService) SaveDigestSettings(ctx context.Context, actor uint, input FeishuMentionDigestSettingsInput) (*FeishuMentionDigestSettingsDTO, error) {
	if actor == 0 || !validMentionDigestTime(input.DigestTime) {
		return nil, ErrFeishuMentionAdminInvalid
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	if s.dao.AppID == "" {
		return nil, ErrFeishuMentionAdminInvalid
	}
	result := FeishuMentionDigestSettingsDTO{DigestTime: input.DigestTime, Timezone: "Asia/Shanghai"}
	err := s.dao.Transaction(ctx, func(d *data_dao.FeishuMentionAdminDAO) error {
		fallback := s.config.DigestTime
		if fallback == "" {
			fallback = "18:00"
		}
		initial, err := json.Marshal(FeishuMentionDigestSettingsDTO{DigestTime: fallback, Timezone: "Asia/Shanghai"})
		if err != nil {
			return err
		}
		key := mentionDigestSettingsKey(d.AppID)
		row := model.RuntimeConfig{ConfigKey: key, ConfigJSON: string(initial)}
		row.CreatedAt, row.UpdatedAt = int(s.now().Unix()), int(s.now().Unix())
		// Serializes simultaneous first saves as well as later updates, without
		// requiring a digest schedule or turning the module on.
		if err := d.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		if err := d.DB.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", key).First(&row).Error; err != nil {
			return err
		}
		before, err := loadMentionDigestSettings(ctx, d.DB, d.AppID, fallback)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if err := data_dao.NewRuntimeConfigDAO(d.DB).SetValue(ctx, key, string(raw)); err != nil {
			return err
		}
		return mentionAudit(ctx, d, actor, "FEISHU_DIGEST_SETTINGS_UPDATE", "runtime_config", row.ID,
			map[string]interface{}{"appId": d.AppID, "settings": before},
			map[string]interface{}{"appId": d.AppID, "settings": result})
	})
	if err != nil {
		return nil, mentionAdminError(err)
	}
	return &result, nil
}
