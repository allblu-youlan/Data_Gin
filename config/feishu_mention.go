package config

import (
	"fmt"
	"time"

	"github.com/spf13/cast"

	"gin-biz-web-api/pkg/config"
)

// FeishuMentionConfig is loaded once per process. Business switches must match
// across receivers, workers and schedulers using the same Feishu application.
type FeishuMentionConfig struct {
	Enabled                 bool
	ReceiverEnabled         bool
	Timezone                string
	PersonalReminderEnabled bool
	FollowupEnabled         bool
	DefaultFollowupMinutes  int
	MaxFollowups            int
	GroupFallbackEnabled    bool
	DigestEnabled           bool
	DigestTime              string
	DigestSkipEmpty         bool
	AIEnabled               bool
	APITimeoutSeconds       int
	DeliveryLeaseSeconds    int
	MaxAttempts             int
	SendRatePerSecond       int
	RetentionDays           int
}

func init() {
	config.Add("cfg.feishu_mention", func() map[string]interface{} {
		return map[string]interface{}{
			"enabled":                   mentionConfigValue("FeishuMention.Enabled", false),
			"receiver_enabled":          mentionConfigValue("FeishuMention.ReceiverEnabled", false),
			"timezone":                  mentionConfigValue("FeishuMention.Timezone", "Asia/Shanghai"),
			"personal_reminder_enabled": mentionConfigValue("FeishuMention.PersonalReminderEnabled", false),
			"followup_enabled":          mentionConfigValue("FeishuMention.FollowupEnabled", false),
			"default_followup_minutes":  mentionConfigValue("FeishuMention.DefaultFollowupMinutes", 30),
			"max_followups":             mentionConfigValue("FeishuMention.MaxFollowups", 1),
			"group_fallback_enabled":    mentionConfigValue("FeishuMention.GroupFallbackEnabled", false),
			"digest_enabled":            mentionConfigValue("FeishuMention.DigestEnabled", true),
			"digest_time":               mentionConfigValue("FeishuMention.DigestTime", "18:00"),
			"digest_skip_empty":         mentionConfigValue("FeishuMention.DigestSkipEmpty", true),
			"ai_enabled":                mentionConfigValue("FeishuMention.AIEnabled", false),
			"api_timeout_seconds":       mentionConfigValue("FeishuMention.APITimeoutSeconds", 10),
			"delivery_lease_seconds":    mentionConfigValue("FeishuMention.DeliveryLeaseSeconds", 60),
			"max_attempts":              mentionConfigValue("FeishuMention.MaxAttempts", 5),
			"send_rate_per_second":      mentionConfigValue("FeishuMention.SendRatePerSecond", 5),
			"retention_days":            mentionConfigValue("FeishuMention.RetentionDays", 90),
		}
	})
}

func LoadFeishuMentionConfig() (FeishuMentionConfig, error) {
	c := FeishuMentionConfig{
		Enabled:                 cast.ToBool(mentionConfigValue("cfg.feishu_mention.enabled", false)),
		ReceiverEnabled:         cast.ToBool(mentionConfigValue("cfg.feishu_mention.receiver_enabled", false)),
		Timezone:                cast.ToString(mentionConfigValue("cfg.feishu_mention.timezone", "Asia/Shanghai")),
		PersonalReminderEnabled: cast.ToBool(mentionConfigValue("cfg.feishu_mention.personal_reminder_enabled", false)),
		FollowupEnabled:         cast.ToBool(mentionConfigValue("cfg.feishu_mention.followup_enabled", false)),
		DefaultFollowupMinutes:  cast.ToInt(mentionConfigValue("cfg.feishu_mention.default_followup_minutes", 30)),
		MaxFollowups:            cast.ToInt(mentionConfigValue("cfg.feishu_mention.max_followups", 1)),
		GroupFallbackEnabled:    cast.ToBool(mentionConfigValue("cfg.feishu_mention.group_fallback_enabled", false)),
		DigestEnabled:           cast.ToBool(mentionConfigValue("cfg.feishu_mention.digest_enabled", true)),
		DigestTime:              cast.ToString(mentionConfigValue("cfg.feishu_mention.digest_time", "18:00")),
		DigestSkipEmpty:         cast.ToBool(mentionConfigValue("cfg.feishu_mention.digest_skip_empty", true)),
		AIEnabled:               cast.ToBool(mentionConfigValue("cfg.feishu_mention.ai_enabled", false)),
		APITimeoutSeconds:       cast.ToInt(mentionConfigValue("cfg.feishu_mention.api_timeout_seconds", 10)),
		DeliveryLeaseSeconds:    cast.ToInt(mentionConfigValue("cfg.feishu_mention.delivery_lease_seconds", 60)),
		MaxAttempts:             cast.ToInt(mentionConfigValue("cfg.feishu_mention.max_attempts", 5)),
		SendRatePerSecond:       cast.ToInt(mentionConfigValue("cfg.feishu_mention.send_rate_per_second", 5)),
		RetentionDays:           cast.ToInt(mentionConfigValue("cfg.feishu_mention.retention_days", 90)),
	}
	return c, c.Validate()
}

func (c FeishuMentionConfig) Validate() error {
	if c.Timezone != "Asia/Shanghai" {
		return fmt.Errorf("FeishuMention.Timezone must be Asia/Shanghai")
	}
	if parsed, err := time.Parse("15:04", c.DigestTime); err != nil || parsed.Format("15:04") != c.DigestTime {
		return fmt.Errorf("FeishuMention.DigestTime must be HH:mm")
	}
	if c.MaxFollowups != 1 || !c.DigestSkipEmpty || c.GroupFallbackEnabled || c.AIEnabled {
		return fmt.Errorf("FeishuMention requires MaxFollowups=1, DigestSkipEmpty=true, GroupFallbackEnabled=false and AIEnabled=false")
	}
	if c.DefaultFollowupMinutes <= 0 || c.APITimeoutSeconds <= 0 || c.MaxAttempts <= 0 || c.SendRatePerSecond <= 0 || c.RetentionDays <= 0 {
		return fmt.Errorf("FeishuMention intervals, attempts, send rate and retention must be positive")
	}
	// One token refresh may result in a second HTTP request; reserve five
	// additional seconds for committing the response under the business lease.
	if c.DeliveryLeaseSeconds <= 2*c.APITimeoutSeconds+5 {
		return fmt.Errorf("FeishuMention.DeliveryLeaseSeconds must exceed twice APITimeoutSeconds plus five seconds")
	}
	return nil
}

// Unlike the shared getter, explicit false and zero are not replaced by defaults.
func mentionConfigValue(key string, fallback interface{}) interface{} {
	if instance := config.Instance(); instance != nil && instance.IsSet(key) {
		return instance.Get(key)
	}
	return fallback
}
