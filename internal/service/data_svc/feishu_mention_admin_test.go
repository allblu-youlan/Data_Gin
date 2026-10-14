package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	appconfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFeishuMentionAdminFilters(t *testing.T) {
	cases := []struct {
		name, query, kind string
		valid             bool
	}{
		{"defaults", "", "recipients", true}, {"out of range page retained", "page=99&per_page=100", "recipients", true},
		{"zero page", "page=0", "recipients", false}, {"empty page", "page=", "recipients", false}, {"large page size", "per_page=101", "jobs", false},
		{"invalid confirmation", "viewStatus=unread", "recipients", false}, {"invalid task status", "status=ready", "jobs", false}, {"invalid report status", "status=pending", "reports", false}, {"invalid task type", "jobType=arbitrary", "jobs", false}, {"strict boolean", "enabled=1", "chats", false},
		{"one received boundary", "receivedFrom=2026-10-13T10:00:00%2B08:00", "recipients", false}, {"valid received range", "receivedFrom=2026-10-13T10:00:00%2B08:00&receivedTo=2026-10-13T18:00:00%2B08:00", "recipients", true}, {"equal received boundaries", "receivedFrom=2026-10-13T10:00:00Z&receivedTo=2026-10-13T10:00:00Z", "recipients", false},
		{"single report date valid", "reportDateFrom=2026-10-13&reportDateTo=2026-10-13", "reports", true}, {"one date boundary", "reportDateFrom=2026-10-13", "reports", false}, {"zero recipient id", "recipientRecordId=0", "jobs", false}, {"repeated pagination", "page=1&page=2", "jobs", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, _ := url.ParseQuery(tc.query)
			f, e := ParseFeishuMentionAdminFilter(v, tc.kind)
			if (e == nil) != tc.valid {
				t.Fatalf("filter=%+v error=%v", f, e)
			}
			if tc.query == "page=99&per_page=100" && f.Page != 99 {
				t.Fatal("out of range page must be retained")
			}
			if f.ReceivedFrom != nil && f.ReceivedFrom.Location().String() != "Asia/Shanghai" {
				t.Fatal("query time must be Asia/Shanghai")
			}
		})
	}
}
func TestFeishuMentionRecipientConfirmationDTO(t *testing.T) {
	service := NewFeishuMentionAdminService(nil, "cli_current", appconfig.FeishuMentionConfig{})
	for _, view := range []string{"not_required", "pending", "confirmed"} {
		t.Run(view, func(t *testing.T) {
			r := data_dao.FeishuMentionAdminRecipientRow{FeishuMessageRecipient: model.FeishuMessageRecipient{BaseModel: model.BaseModel{ID: 9}, ViewStatus: view}, DisplayText: strings.Repeat("文", 300)}
			dto := service.recipientDTO(r, nil, true)
			if dto.ViewStatus != view || dto.ConfirmationEnabled || dto.OriginalReadStatus != "unavailable" || dto.InitialReminder != nil || dto.FollowupReminder != nil || dto.OriginalURL != nil {
				t.Fatalf("invalid derived state: %+v", dto)
			}
			if len([]rune(dto.ContentPreview)) != 161 || *dto.DisplayText != r.DisplayText {
				t.Fatal("only preview may be truncated")
			}
			encoded, _ := json.Marshal(dto)
			if strings.Contains(string(encoded), "rawContent") {
				t.Fatal("raw provider content leaked")
			}
		})
	}
}
func TestFeishuMentionReportUsesFrozenIdentity(t *testing.T) {
	snapshot := model.FeishuDigestSnapshot{ChatName: "原群名", RecipientName: "张三", MessageCount: 2, Timezone: "Asia/Shanghai"}
	encoded, _ := json.Marshal(snapshot)
	report := model.FeishuDailyReport{BaseModel: model.BaseModel{ID: 7}, ChatID: "oc_a", RecipientOpenID: "ou_a", ContentSnapshot: model.JSONText(encoded), PartCount: 3, Status: "failed"}
	part1, part2 := uint(1), uint(2)
	id := uint(7)
	dto, err := mentionReportDTO(report, []model.FeishuDeliveryJob{{ReportID: &id, PartNo: &part1, Status: "sent"}, {ReportID: &id, PartNo: &part2, Status: "failed"}}, true)
	if err != nil || dto.ChatName != "原群名" || dto.MessageCount != 2 || dto.SentPartCount != 1 || len(dto.Parts) != 2 {
		t.Fatalf("snapshot DTO=%+v error=%v", dto, err)
	}
}
func TestFeishuMentionAdminErrorsRemainSafe(t *testing.T) {
	status, message := FeishuMentionAdminSafeError(errors.New("SQL password=secret raw_content=private"))
	if status != 500 || strings.Contains(message, "secret") || strings.Contains(message, "private") {
		t.Fatal("internal error leaked")
	}
}

func TestFeishuMentionCardRetryRequiresUpdateableTarget(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour)
	expired := now.Add(-14 * 24 * time.Hour)
	for _, tc := range []struct {
		name    string
		sentAt  *time.Time
		message string
		want    bool
	}{{"recent original", &recent, "om_target", true}, {"missing sent time", nil, "om_target", false}, {"expired original", &expired, "om_target", false}, {"different card", &recent, "om_other", false}} {
		t.Run(tc.name, func(t *testing.T) {
			card := model.FeishuDeliveryJob{Status: "sent", SentAt: tc.sentAt, FeishuMessageID: tc.message}
			if got := mentionCardUpdateable(card, "om_target", now); got != tc.want {
				t.Fatalf("updateable=%v want %v", got, tc.want)
			}
		})
	}
}

func TestFeishuMentionAdminUsesShanghaiTimesWithoutChangingInstant(t *testing.T) {
	utc := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	encoded, err := json.Marshal(mentionAdminTime(&utc))
	if err != nil || string(encoded) != `"2026-10-13T10:00:00+08:00"` {
		t.Fatalf("encoded time=%s error=%v", encoded, err)
	}
	if utc.Location() != time.UTC {
		t.Fatal("conversion mutated immutable source")
	}
	service := NewFeishuMentionAdminService(nil, "cli", appconfig.FeishuMentionConfig{})
	if service.now().Location().String() != "Asia/Shanghai" {
		t.Fatal("admin clock must use Shanghai")
	}
}

func TestFeishuMentionAdminStatusKeepsPauseReasonWithoutDatabase(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  appconfig.FeishuMentionConfig
		want string
	}{
		{"module off", appconfig.FeishuMentionConfig{}, "模块已关闭"},
		{"digest off", appconfig.FeishuMentionConfig{Enabled: true}, "日报已暂停，恢复后按原统计日补建"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewFeishuMentionAdminService(nil, "cli_current", tc.cfg)
			status, err := service.Status(context.Background())
			if err != nil || status["pauseReason"] != tc.want || status["appId"] != "cli_current" {
				t.Fatalf("status=%+v error=%v", status, err)
			}
			if status["backlog"] != nil || status["observations"] != nil {
				t.Fatal("unavailable database must not appear to have zero backlog")
			}
		})
	}
}

func TestFeishuMentionAdminStatusDoesNotGuessDigestTimeWithoutDatabase(t *testing.T) {
	for _, tc := range []struct {
		time    string
		enabled bool
	}{
		{"18:00", true},
		{"20:30", true},
		{"00:05", false},
	} {
		t.Run(tc.time, func(t *testing.T) {
			cfg := appconfig.FeishuMentionConfig{Enabled: true, DigestEnabled: tc.enabled, DigestTime: tc.time, Timezone: "Asia/Shanghai"}
			service := NewFeishuMentionAdminService(nil, "cli_current", cfg)
			status, err := service.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				DigestTime    *string `json:"digestTime"`
				Timezone      string  `json:"timezone"`
				DigestEnabled bool    `json:"digestEnabled"`
			}
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.DigestTime != nil || decoded.Timezone != "Asia/Shanghai" || decoded.DigestEnabled != tc.enabled {
				t.Fatalf("status must not present process defaults as the persisted setting when storage is unavailable: %+v", decoded)
			}
		})
	}
}
