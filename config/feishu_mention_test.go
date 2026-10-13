package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pkgConfig "gin-biz-web-api/pkg/config"
)

func TestFeishuMentionDefaultsAndExplicitPause(t *testing.T) {
	unsetFeishuMentionDigestTimeEnv(t)
	loadFeishuMentionTestConfig(t, "")
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
	unsetFeishuMentionDigestTimeEnv(t)
	loadFeishuMentionTestConfig(t, "")
	base, err := LoadFeishuMentionConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*FeishuMentionConfig){
		func(c *FeishuMentionConfig) { c.Timezone = "UTC" },
		func(c *FeishuMentionConfig) { c.DigestTime = "8:00" },
		func(c *FeishuMentionConfig) { c.DigestTime = "24:00" },
		func(c *FeishuMentionConfig) { c.MaxAttempts = 0 },
		func(c *FeishuMentionConfig) { c.SendRatePerSecond = 1001 },
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

func TestFeishuMentionDigestTimeConfigurationPriority(t *testing.T) {
	for _, tt := range []struct {
		name, yaml, env, want string
		setEnv                bool
	}{
		{name: "default", want: "18:00"},
		{name: "yaml", yaml: "FeishuMention:\n  DigestTime: \"17:30\"\n", want: "17:30"},
		{name: "environment overrides yaml", yaml: "FeishuMention:\n  DigestTime: \"17:30\"\n", env: "20:15", setEnv: true, want: "20:15"},
		{name: "environment overrides invalid yaml", yaml: "FeishuMention:\n  DigestTime: \"invalid\"\n", env: "00:00", setEnv: true, want: "00:00"},
		{name: "environment without yaml", env: "23:59", setEnv: true, want: "23:59"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(EnvFeishuMentionDigestTime, tt.env)
			} else {
				unsetFeishuMentionDigestTimeEnv(t)
			}
			loadFeishuMentionTestConfig(t, tt.yaml)
			cfg, err := LoadFeishuMentionConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DigestTime != tt.want || cfg.Timezone != "Asia/Shanghai" {
				t.Fatalf("digest time=%q timezone=%q want=%q Asia/Shanghai", cfg.DigestTime, cfg.Timezone, tt.want)
			}
			if got := pkgConfig.Instance().GetString("cfg.feishu_mention.digest_time"); got != tt.want {
				t.Fatalf("registered digest_time=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestFeishuMentionRejectsExplicitInvalidDigestTimeEnvironment(t *testing.T) {
	for _, value := range []string{"", " ", "8:00", "24:00", "18:60", "18:00:00", "18:00 ", "12:ab"} {
		t.Run(fmt.Sprintf("value=%q", value), func(t *testing.T) {
			t.Setenv(EnvFeishuMentionDigestTime, value)
			loadFeishuMentionTestConfig(t, "FeishuMention:\n  DigestTime: \"17:30\"\n")
			cfg, err := LoadFeishuMentionConfig()
			if err == nil || !strings.Contains(err.Error(), EnvFeishuMentionDigestTime) {
				t.Fatalf("invalid environment did not return an actionable error: %v", err)
			}
			if cfg.DigestTime != value {
				t.Fatalf("invalid environment was silently replaced: %q", cfg.DigestTime)
			}
		})
	}
}

func TestFeishuMentionRejectsExplicitInvalidDigestTimeYAML(t *testing.T) {
	unsetFeishuMentionDigestTimeEnv(t)
	for _, value := range []string{"", "8:00", "24:00"} {
		t.Run(fmt.Sprintf("value=%q", value), func(t *testing.T) {
			loadFeishuMentionTestConfig(t, fmt.Sprintf("FeishuMention:\n  DigestTime: %q\n", value))
			if _, err := LoadFeishuMentionConfig(); err == nil {
				t.Fatal("accepted invalid YAML digest time")
			}
		})
	}
}

func unsetFeishuMentionDigestTimeEnv(t *testing.T) {
	t.Helper()
	// Setenv registers restoration of the original environment before Unsetenv.
	t.Setenv(EnvFeishuMentionDigestTime, "")
	if err := os.Unsetenv(EnvFeishuMentionDigestTime); err != nil {
		t.Fatal(err)
	}
}

func loadFeishuMentionTestConfig(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgConfig.NewConfig("", dir+string(os.PathSeparator))
}
