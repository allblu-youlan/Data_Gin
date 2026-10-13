package config

import (
	"testing"

	pkgConfig "gin-biz-web-api/pkg/config"
)

func TestFeishuMentionDefaultsAndExplicitPause(t *testing.T) {
	cfg, err := LoadFeishuMentionConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.PersonalReminderEnabled || cfg.FollowupEnabled || !cfg.DigestEnabled {
		t.Fatalf("unsafe defaults: %+v", cfg)
	}
	v := pkgConfig.Instance()
	v.Set("cfg.feishu_mention.digest_enabled", false)
	t.Cleanup(func() { v.Set("cfg.feishu_mention.digest_enabled", nil) })
	cfg, err = LoadFeishuMentionConfig()
	if err != nil || cfg.DigestEnabled {
		t.Fatalf("explicit pause lost: %+v, %v", cfg, err)
	}
}

func TestFeishuMentionRejectsUnsupportedAndInvalidSettings(t *testing.T) {
	base, err := LoadFeishuMentionConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*FeishuMentionConfig){
		func(c *FeishuMentionConfig) { c.Timezone = "UTC" },
		func(c *FeishuMentionConfig) { c.DigestTime = "8:00" },
		func(c *FeishuMentionConfig) { c.DigestTime = "24:00" },
		func(c *FeishuMentionConfig) { c.MaxAttempts = 0 },
		func(c *FeishuMentionConfig) { c.DeliveryLeaseSeconds = 25 },
		func(c *FeishuMentionConfig) { c.AIEnabled = true },
		func(c *FeishuMentionConfig) { c.GroupFallbackEnabled = true },
		func(c *FeishuMentionConfig) { c.DigestSkipEmpty = false },
	} {
		cfg := base
		change(&cfg)
		if cfg.Validate() == nil {
			t.Fatalf("accepted invalid settings: %+v", cfg)
		}
	}
}
