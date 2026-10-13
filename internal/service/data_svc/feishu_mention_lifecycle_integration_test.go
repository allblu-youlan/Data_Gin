//go:build integration

package data_svc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

func TestFeishuMentionMySQLChangedDigestTimeKeepsWindowStart(t *testing.T) {
	for _, tt := range []struct {
		clock string
		hours time.Duration
	}{{"17:00", 7}, {"20:00", 10}, {"08:00", 22}} {
		t.Run(tt.clock, func(t *testing.T) {
			s, now := mentionMySQLFixture(t, false)
			ctx := context.Background()
			start := *now
			if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "before_change", "u1")); err != nil {
				t.Fatal(err)
			}
			s.cfg.DigestTime = tt.clock
			*now = now.Add(time.Hour)
			if err := s.PlanDigests(ctx); err != nil {
				t.Fatal(err)
			}
			var schedule model.FeishuDigestSchedule
			if err := s.db.Where("app_id = ?", s.appID).First(&schedule).Error; err != nil {
				t.Fatal(err)
			}
			wantCutoff := start.Add(tt.hours * time.Hour)
			if !schedule.NextWindowStartAt.Equal(start) || !schedule.NextCutoffAt.Equal(wantCutoff) {
				t.Fatalf("changed window: start=%s cutoff=%s", schedule.NextWindowStartAt, schedule.NextCutoffAt)
			}
			*now = wantCutoff
			if err := s.PlanDigests(ctx); err != nil {
				t.Fatal(err)
			}
			var report model.FeishuDailyReport
			if err := s.db.Where("app_id = ?", s.appID).First(&report).Error; err != nil {
				t.Fatal(err)
			}
			var snapshot model.FeishuDigestSnapshot
			if err := json.Unmarshal([]byte(report.ContentSnapshot), &snapshot); err != nil {
				t.Fatal(err)
			}
			if !report.WindowStart.Equal(start) || !report.WindowEnd.Equal(wantCutoff) || snapshot.MessageCount != 1 {
				t.Fatalf("changed cutoff lost the original interval: %#v", report)
			}
		})
	}
}

func TestFeishuMentionMySQLChangedDigestTimeFinishesOldBacklog(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	start := *now
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "first_day", "u1")); err != nil {
		t.Fatal(err)
	}
	s.cfg.DigestEnabled = false
	*now = start.Add(31*time.Hour + 30*time.Minute)
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "second_day_after_new_clock", "u1")); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	s.cfg.DigestTime = "17:00"
	s.cfg.DigestEnabled = true
	*now = start.Add(57 * time.Hour)
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var reports []model.FeishuDailyReport
	if err := s.db.Where("app_id = ?", s.appID).Order("report_date").Find(&reports).Error; err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("backlog reports=%d", len(reports))
	}
	for i, report := range reports {
		if !report.WindowEnd.Equal(start.Add(time.Duration(8+24*i) * time.Hour)) {
			t.Fatalf("overdue window was changed: %s", report.WindowEnd)
		}
	}
	var schedule model.FeishuDigestSchedule
	if err := s.db.Where("app_id = ?", s.appID).First(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	if !schedule.NextWindowStartAt.Equal(start.Add(56*time.Hour)) || !schedule.NextCutoffAt.Equal(start.Add(79*time.Hour)) {
		t.Fatalf("new clock did not follow completed backlog: %#v", schedule)
	}
}
