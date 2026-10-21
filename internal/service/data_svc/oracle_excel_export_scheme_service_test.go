package data_svc

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

func TestOracleExcelExportSchemesPersistReloadUpdateAndDelete(t *testing.T) {
	store := &fakeOracleExcelExportSchemeStore{rows: []model.ExcelMatchScheme{{
		BaseModel: model.BaseModel{ID: 1}, Name: "订单导出", Operation: excelOperationExportMatch, ConfigJSON: `{}`,
	}}}
	newService := func() *OracleExcelExportSchemeService {
		return &OracleExcelExportSchemeService{store: store, normalize: func(_ context.Context, input OracleExcelExportConfig) (OracleExcelExportConfig, error) {
			config, _, _, err := buildOracleExcelExportQuery(input, oracleExcelTestSchema())
			return config, err
		}}
	}
	service := newService()
	input := OracleExcelExportConfig{
		Owner: "DEMO", TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "CODE", Title: "订单号"}},
		Filters: []OracleExcelExportFilter{{Field: "CREATED_AT", Op: "gte", Value: "2026-10-21T10:30"}, {Field: "AMOUNT", Op: "gte", Value: "100"}},
	}
	saved, err := service.Save(t.Context(), " 订单导出 ", input)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 1 || saved.Name != "订单导出" || saved.Operation != excelOperationOracleExport || saved.Config.Filters[0].Value != "2026-10-21 10:30:00" {
		t.Fatalf("saved configuration=%+v", saved)
	}
	// Reconstruct the service to check that configuration state comes from the
	// shared repository rather than one request or an in-memory UI draft.
	reloaded, err := newService().List(t.Context())
	if err != nil || len(reloaded) != 1 || !reflect.DeepEqual(reloaded[0], *saved) {
		t.Fatalf("reloaded=%+v error=%v", reloaded, err)
	}
	var raw OracleExcelExportConfig
	if err := json.Unmarshal([]byte(saved.ConfigJSON), &raw); err != nil || !reflect.DeepEqual(raw, saved.Config) {
		t.Fatalf("stored JSON lost configuration: config=%+v error=%v", raw, err)
	}
	input.Columns[0].Title = "更新后的列名"
	updated, err := newService().Save(t.Context(), "订单导出", input)
	if err != nil || updated.ID != saved.ID || updated.Config.Columns[0].Title != "更新后的列名" || len(store.rows) != 2 {
		t.Fatalf("updated=%+v store=%+v error=%v", updated, store.rows, err)
	}
	if err := service.Delete(t.Context(), 1); err != nil || len(store.rows) != 2 {
		t.Fatalf("Oracle configuration endpoint deleted a matching scheme: rows=%+v error=%v", store.rows, err)
	}
	if err := service.Delete(t.Context(), saved.ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := service.List(t.Context())
	if err != nil || len(remaining) != 0 || len(store.rows) != 1 {
		t.Fatalf("remaining=%+v rows=%+v error=%v", remaining, store.rows, err)
	}
}

func TestOracleExcelExportSchemeRejectsInvalidDraftBeforeSaving(t *testing.T) {
	store := &fakeOracleExcelExportSchemeStore{}
	service := &OracleExcelExportSchemeService{store: store, normalize: func(_ context.Context, input OracleExcelExportConfig) (OracleExcelExportConfig, error) {
		config, _, _, err := buildOracleExcelExportQuery(input, oracleExcelTestSchema())
		return config, err
	}}
	if _, err := service.Save(t.Context(), " ", OracleExcelExportConfig{}); err == nil {
		t.Fatal("accepted empty configuration name")
	}
	if _, err := service.Save(t.Context(), "无效字段", OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "MISSING"}}}); err == nil {
		t.Fatal("accepted missing export field")
	}
	if len(store.rows) != 0 {
		t.Fatalf("invalid configuration was persisted: %+v", store.rows)
	}
	if err := service.Delete(t.Context(), 0); err == nil {
		t.Fatal("accepted zero configuration ID")
	}
}

func TestOracleExcelExportSchemesDefaultFiltersAndCaseInsensitiveUpdate(t *testing.T) {
	store := &fakeOracleExcelExportSchemeStore{}
	service := &OracleExcelExportSchemeService{store: store, normalize: func(_ context.Context, input OracleExcelExportConfig) (OracleExcelExportConfig, error) {
		config, _, _, err := buildOracleExcelExportQuery(input, oracleExcelTestSchema())
		return config, err
	}}
	input := OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS"}
	saved, err := service.Save(t.Context(), "Sales", input)
	if err != nil || saved.Config.Filters == nil || !strings.Contains(saved.ConfigJSON, `"filters":[]`) {
		t.Fatalf("default configuration is not reloadable: saved=%+v error=%v", saved, err)
	}
	updated, err := service.Save(t.Context(), "sales", input)
	if err != nil || updated.ID != saved.ID || updated.Name != "Sales" || len(store.rows) != 1 {
		t.Fatalf("case-insensitive update=%+v rows=%+v error=%v", updated, store.rows, err)
	}
}

type fakeOracleExcelExportSchemeStore struct {
	rows []model.ExcelMatchScheme
}

func (store *fakeOracleExcelExportSchemeStore) ListSchemes(_ context.Context, operation string) ([]model.ExcelMatchScheme, error) {
	rows := make([]model.ExcelMatchScheme, 0)
	for _, row := range store.rows {
		if row.Operation == operation {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (store *fakeOracleExcelExportSchemeStore) SaveScheme(_ context.Context, row *model.ExcelMatchScheme) error {
	for index, existing := range store.rows {
		if strings.EqualFold(existing.Name, row.Name) && existing.Operation == row.Operation {
			row.ID = existing.ID
			store.rows[index].ConfigJSON = row.ConfigJSON
			return nil
		}
	}
	row.ID = uint(len(store.rows) + 1)
	store.rows = append(store.rows, *row)
	return nil
}

func (store *fakeOracleExcelExportSchemeStore) DeleteOracleExportScheme(_ context.Context, id uint) error {
	for index, row := range store.rows {
		if row.ID == id && row.Operation == excelOperationOracleExport {
			store.rows = append(store.rows[:index], store.rows[index+1:]...)
			break
		}
	}
	return nil
}
