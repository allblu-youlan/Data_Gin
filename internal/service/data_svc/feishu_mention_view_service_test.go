package data_svc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

func TestFeishuMentionViewReportRequiresIdentity(t *testing.T) {
	s, _, _ := mentionViewTestService(t)
	for _, openID := range []string{"", "recipient-name", "ou_a' OR 1=1"} {
		if _, err := s.Report(t.Context(), openID, 42, 1); !errors.Is(err, ErrFeishuMentionViewUnauthorized) {
			t.Fatal("unauthenticated report request reached storage")
		}
	}
	if _, err := s.Report(t.Context(), "ou_viewer", 0, 1); !errors.Is(err, ErrFeishuMentionViewInvalid) {
		t.Fatal("zero report id accepted")
	}
	if _, err := s.Report(t.Context(), "ou_viewer", 42, 0); !errors.Is(err, ErrFeishuMentionViewInvalid) {
		t.Fatal("zero page accepted")
	}
}

func TestFeishuMentionViewDTOOnlyExposesPagedFrozenDisplayData(t *testing.T) {
	now := time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)
	link := "https://applink.feishu.cn/client/message/link?token=source-link"
	snapshot := model.FeishuDigestSnapshot{
		SchemaVersion: 1, AppID: "private-app-id", ChatID: "private-chat-id", ChatName: "冻结群名", RecipientOpenID: "ou_viewer", RecipientName: "冻结姓名",
		ReportDate: "2026-10-14", WindowStart: now.Add(-time.Hour), WindowEnd: now, GeneratedAt: now, MessageCount: 21,
		Parts: []model.FeishuDigestPart{{PartNo: 1, Title: "private-delivery-title", Fragments: []model.FeishuDigestFragment{{Text: "private-old-card-content"}}}},
	}
	for i := 1; i <= 21; i++ {
		snapshot.Messages = append(snapshot.Messages, model.FeishuDigestMessage{
			RecipientRecordID: uint(i), SourceMessageID: uint(i), MessageID: "private-source-id", SenderOpenID: "ou_sender", SenderName: "发送人",
			MessageCreatedAt: now, ReceivedAt: now, DisplayText: fmt.Sprintf("正文%d\n<script>literal text</script>", i), OriginalURL: &link,
		})
	}
	dto := mentionViewReportDTO(42, snapshot, 2)
	if dto.Report.ChatName != "冻结群名" || dto.Report.MessageCount != 21 || dto.Pagination.Page != 2 || dto.Pagination.TotalPage != 2 || len(dto.Messages) != 1 || dto.Messages[0].DisplayText != snapshot.Messages[20].DisplayText {
		t.Fatalf("wrong frozen page: %+v", dto)
	}
	if dto.Messages[0].MessageCreatedAt.Format(time.RFC3339) != "2026-10-14T10:00:00+08:00" || dto.Report.Timezone != "Asia/Shanghai" {
		t.Fatal("viewer time not converted to Shanghai")
	}
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-", "recipientRecordId", "sourceMessageId", "messageId", "rawContent", "contentSnapshot", "retryable", "parts", "status", "appId", "chatId"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private field leaked outside HTML: %s", forbidden)
		}
	}
	if snapshot.Messages[20].MessageCreatedAt.Location() != time.UTC {
		t.Fatal("viewer mutated frozen snapshot")
	}
	if page := mentionViewReportDTO(42, snapshot, 99); len(page.Messages) != 0 || page.Pagination.Page != 99 {
		t.Fatal("out-of-range page must be empty")
	}
	unsafe := "javascript:alert(1)"
	snapshot.Messages[0].OriginalURL = &unsafe
	if page := mentionViewReportDTO(42, snapshot, 1); page.Messages[0].OriginalURL != nil {
		t.Fatal("unsafe source URL exposed")
	}
}
