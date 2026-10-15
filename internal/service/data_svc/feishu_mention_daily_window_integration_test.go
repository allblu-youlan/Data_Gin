//go:build integration

package data_svc

import (
	"encoding/json"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
)

func TestFeishuMentionMySQLDailyIncludesOnlyMessagesSentOnReportDay(t *testing.T) {
	core, now := mentionMySQLFixture(t, false)
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, mentionLocation())
	end := start.Add(18 * time.Hour)
	// Reproduce an existing installation whose cursor still starts yesterday.
	if err := core.db.Model(&model.FeishuDigestSchedule{}).Where("app_id = ?", core.appID).
		Updates(map[string]any{"next_window_start_at": start.Add(-10*time.Hour + 21*time.Minute), "next_cutoff_at": end}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id                string
		received, created time.Time
	}{
		{"yesterday", start.Add(-9*time.Hour - 27*time.Minute), start.Add(-9*time.Hour - 27*time.Minute)},
		{"before_midnight", start.Add(-time.Millisecond), start.Add(-time.Millisecond)},
		// UTC's previous date is already today at Shanghai midnight.
		{"at_midnight", start.UTC(), start.UTC()},
		{"today", start.Add(10 * time.Hour), start.Add(10 * time.Hour)},
		{"yesterday_arrived_today", start.Add(11 * time.Hour), start.Add(-time.Hour)},
		{"created_at_cutoff", end.Add(-time.Minute), end},
		{"received_at_cutoff", end, end.Add(-time.Minute)},
		{"after_cutoff", end.Add(time.Minute), end.Add(time.Minute)},
	} {
		mentionRegenerateArchive(t, core, now, tc.id, tc.id, tc.received, tc.created)
	}
	// A pair with only old content must not get a report at all.
	*now = start.Add(11 * time.Hour)
	event := mentionTestEvent(core, "chat_b", "old_pair", "ou_other")
	event.CreatedAt = start.Add(-time.Hour)
	if err := core.Archive(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	*now = end.Add(time.Minute)
	admin := NewFeishuMentionAdminService(core.db, core.appID, core.cfg)
	admin.now = core.now
	status, err := admin.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	window := status["digest"].(map[string]interface{})
	observations := status["observations"].(*data_dao.FeishuMentionAdminObservations)
	if !window["nextWindowStartAt"].(time.Time).Equal(start) || observations.Archive.WindowMessages == nil || *observations.Archive.WindowMessages != 2 {
		t.Fatalf("admin window does not match today's report: %+v %+v", window, observations.Archive)
	}
	for range 2 {
		if err := core.PlanDigests(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var reports []model.FeishuDailyReport
	if err := core.db.Where("app_id = ?", core.appID).Find(&reports).Error; err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("only today's pair should get one report, got %d", len(reports))
	}
	report := reports[0]
	var snapshot model.FeishuDigestSnapshot
	if err := json.Unmarshal([]byte(report.ContentSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if report.ReportDate != "2026-10-14" || !report.WindowStart.Equal(start) || !report.WindowEnd.Equal(end) ||
		!snapshot.WindowStart.Equal(start) || !snapshot.WindowEnd.Equal(end) || snapshot.MessageCount != 2 || len(snapshot.Messages) != 2 {
		t.Fatalf("wrong daily window: report=%+v snapshot=%+v", report, snapshot)
	}
	if snapshot.Messages[0].MessageID != "at_midnight" || snapshot.Messages[1].MessageID != "today" {
		t.Fatalf("previous-day or out-of-window messages were included: %+v", snapshot.Messages)
	}
}
