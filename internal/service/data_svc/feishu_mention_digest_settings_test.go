package data_svc

import (
	"context"
	"errors"
	"strings"
	"testing"

	appconfig "gin-biz-web-api/config"
)

func TestFeishuMentionDigestSettingsInputValidation(t *testing.T) {
	service := NewFeishuMentionAdminService(nil, "app", appconfig.FeishuMentionConfig{})
	for _, value := range []string{"", " ", "8:00", "24:00", "18:60", "18:00:00", "18:00 "} {
		t.Run(value, func(t *testing.T) {
			if _, err := service.SaveDigestSettings(context.Background(), 1, FeishuMentionDigestSettingsInput{DigestTime: value}); !errors.Is(err, ErrFeishuMentionAdminInvalid) {
				t.Fatalf("invalid time=%q error=%v", value, err)
			}
		})
	}
	if _, err := service.SaveDigestSettings(context.Background(), 0, FeishuMentionDigestSettingsInput{DigestTime: "18:00"}); !errors.Is(err, ErrFeishuMentionAdminInvalid) {
		t.Fatalf("missing actor error=%v", err)
	}
	for _, value := range []string{"00:00", "18:00", "23:59"} {
		if !validMentionDigestTime(value) {
			t.Fatalf("valid time rejected: %s", value)
		}
	}
}

func TestFeishuMentionDigestSettingsKeyIsBoundedAndAppScoped(t *testing.T) {
	key := mentionDigestSettingsKey(strings.Repeat("app", 100))
	if len(key) > 100 || key == mentionDigestSettingsKey("other-app") || mentionDigestSettingsKey("APP") == mentionDigestSettingsKey("app") {
		t.Fatal("runtime configuration key is not bounded and app scoped")
	}
}
