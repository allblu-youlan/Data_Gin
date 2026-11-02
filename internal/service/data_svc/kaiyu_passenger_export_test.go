package data_svc

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"

	"github.com/xuri/excelize/v2"
)

func TestKaiyuPassengerExportContainsScopedNumericHours(t *testing.T) {
	hour := time.Date(2026, 10, 28, 9, 0, 0, 0, kaiyuLocation)
	service := newKaiyuPassengerQueryService(kaiyuQueryScopeFunc(func(_ context.Context, actor uint, ids []uint) ([]uint, error) {
		if actor != 11 || len(ids) != 1 || ids[0] != 7 {
			t.Fatal("export skipped requested scope")
		}
		return ids, nil
	}), kaiyuTestMalls{{BaseModel: model.BaseModel{ID: 7}, NameCN: "=1+1", KaiyuShopID: "s7"}}, func(context.Context) (kaiyuPassengerReader, error) {
		return kaiyuQueryReaderFunc(func(_ context.Context, _ time.Time, _ time.Time, ids []uint) ([]model.KaiyuPassengerHourly, error) {
			if len(ids) != 1 || ids[0] != 7 {
				t.Fatal("export widened mall scope")
			}
			return []model.KaiyuPassengerHourly{{MallID: 7, ShopID: "s7", ShopName: "来源门店", HourStart: hour, Passby: 20, Traffic: 10, TrafficCount: 8, TrafficBatch: 5, CaptureRate: .5, AvgVisitDuration: 12.5}}, nil
		}), nil
	})
	export, err := service.Export(t.Context(), 11, KaiyuPassengerQuery{StartDate: "2026-10-28", EndDate: "2026-10-28", MallID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if export.FileName != "kaiyu_passenger_2026-10-28_2026-10-28.xlsx" {
		t.Fatalf("filename=%s", export.FileName)
	}
	book, err := excelize.OpenReader(bytes.NewReader(export.Content))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for _, sheet := range []string{"门店汇总", "小时趋势", "小时明细"} {
		rows, err := book.GetRows(sheet)
		if err != nil || len(rows) != 2 {
			t.Fatalf("sheet=%s rows=%v err=%v", sheet, rows, err)
		}
	}
	if value, err := book.GetCellValue("小时明细", "B2"); err != nil || value != "=1+1" {
		t.Fatalf("mall name=%q err=%v", value, err)
	}
	if formula, err := book.GetCellFormula("小时明细", "B2"); err != nil || formula != "" {
		t.Fatalf("user data became formula %q, %v", formula, err)
	}
	if value, _ := book.GetCellValue("小时明细", "E2"); value != "2026-10-28 09:00:00" {
		t.Fatalf("hour=%q", value)
	}
	if value, _ := book.GetCellValue("小时明细", "G2", excelize.Options{RawCellValue: true}); value != "10" {
		t.Fatalf("traffic not numeric=%q", value)
	}
	if value, _ := book.GetCellValue("小时明细", "K2", excelize.Options{RawCellValue: true}); value != "12.5" {
		t.Fatalf("duration not seconds=%q", value)
	}
	for cell, want := range map[string]string{"N1": "销售额（分）", "O1": "客单数", "P1": "客单价（分）", "Q1": "转化率"} {
		if got, err := book.GetCellValue("小时明细", cell); err != nil || got != want {
			t.Fatalf("%s=%q want=%q err=%v", cell, got, want, err)
		}
	}
}

func TestKaiyuPassengerExportRejectsForbiddenAndOversizedData(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		scopeErr, errorResult error
	}{
		{"forbidden", auth_svc.ErrMallScopeForbidden, auth_svc.ErrMallScopeForbidden},
		{"too many rows", nil, reportoracle.ErrKaiyuPassengerTooManyRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := newKaiyuPassengerQueryService(kaiyuQueryScopeFunc(func(context.Context, uint, []uint) ([]uint, error) { return nil, tc.scopeErr }), kaiyuTestMalls(nil), func(context.Context) (kaiyuPassengerReader, error) {
				if tc.scopeErr != nil {
					t.Fatal("forbidden export reached Oracle")
				}
				return kaiyuQueryReaderFunc(func(context.Context, time.Time, time.Time, []uint) ([]model.KaiyuPassengerHourly, error) {
					return nil, reportoracle.ErrKaiyuPassengerTooManyRows
				}), nil
			})
			if result, err := service.Export(t.Context(), 11, KaiyuPassengerQuery{StartDate: "2026-10-28", EndDate: "2026-10-28"}); result != nil || !errors.Is(err, tc.errorResult) {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
}
