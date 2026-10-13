package model

import (
	"context"
	"gorm.io/gorm/schema"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestFeishuMentionReportDateKeepsDateOnly(t *testing.T) {
	parsed, err := schema.Parse(&FeishuDailyReport{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := parsed.LookUpField("ReportDate")
	row := FeishuDailyReport{}
	value := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	if err := field.Serializer.Scan(context.Background(), field, reflect.ValueOf(&row).Elem(), value); err != nil {
		t.Fatal(err)
	}
	if row.ReportDate != "2026-10-13" {
		t.Fatalf("date=%s", row.ReportDate)
	}
}
