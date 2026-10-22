package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

type operationSchemeMemoryStore struct{ rows []model.ExcelMatchScheme }

func (store *operationSchemeMemoryStore) ListSchemes(_ context.Context, operation string) ([]model.ExcelMatchScheme, error) {
	rows := []model.ExcelMatchScheme{}
	for _, row := range store.rows {
		if row.Operation == operation {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (store *operationSchemeMemoryStore) SaveScheme(_ context.Context, row *model.ExcelMatchScheme) error {
	for index, existing := range store.rows {
		if existing.Operation == row.Operation && strings.EqualFold(existing.Name, row.Name) {
			row.ID = existing.ID
			store.rows[index].ConfigJSON = row.ConfigJSON
			return nil
		}
	}
	row.ID = uint(len(store.rows) + 1)
	store.rows = append(store.rows, *row)
	return nil
}

func (store *operationSchemeMemoryStore) GetOperationScheme(_ context.Context, operation string, id uint) (*model.ExcelMatchScheme, error) {
	for _, row := range store.rows {
		if row.ID == id && row.Operation == operation {
			return &row, nil
		}
	}
	return nil, errors.New("not found")
}

func (store *operationSchemeMemoryStore) DeleteOperationScheme(_ context.Context, operation string, id uint) error {
	for index, row := range store.rows {
		if row.ID == id && row.Operation == operation {
			store.rows = append(store.rows[:index], store.rows[index+1:]...)
			break
		}
	}
	return nil
}

func TestOracleOperationSchemesPersistUpdateAndKeepOperationsSeparate(t *testing.T) {
	store := &operationSchemeMemoryStore{}
	service := &ExcelOperationSchemeService{store: store, normalize: func(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
		if !json.Valid(input) {
			return nil, errors.New("invalid config")
		}
		return input, nil
	}}
	imported, err := service.Save(t.Context(), " Sales ", ExcelOperationOracleImport, json.RawMessage(`{"tableName":"ORDERS"}`))
	if err != nil {
		t.Fatal(err)
	}
	procedure, err := service.Save(t.Context(), "Sales", ExcelOperationOracleProcedure, json.RawMessage(`{"procedureName":"SYNC_ORDERS"}`))
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID == procedure.ID || imported.Name != "Sales" {
		t.Fatalf("schemes are not isolated: import=%+v procedure=%+v", imported, procedure)
	}
	updated, err := service.Save(t.Context(), "sales", ExcelOperationOracleImport, json.RawMessage(`{"tableName":"ORDERS_NEW"}`))
	if err != nil || updated.ID != imported.ID || updated.Name != "Sales" || !strings.Contains(updated.ConfigJSON, "ORDERS_NEW") {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	reloaded := &ExcelOperationSchemeService{store: store}
	list, err := reloaded.List(t.Context(), ExcelOperationOracleImport)
	if err != nil || len(list) != 1 || string(list[0].Config) != updated.ConfigJSON {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if _, err := reloaded.Get(t.Context(), ExcelOperationOracleProcedure, imported.ID); err == nil {
		t.Fatal("retrieved a scheme from another operation")
	}
	if err := reloaded.Delete(t.Context(), ExcelOperationOracleImport, procedure.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.Get(t.Context(), ExcelOperationOracleProcedure, procedure.ID); err != nil {
		t.Fatal("delete crossed operation boundary")
	}
	if err := reloaded.Delete(t.Context(), ExcelOperationOracleImport, imported.ID); err != nil {
		t.Fatal(err)
	}
	list, err = reloaded.List(t.Context(), ExcelOperationOracleImport)
	if err != nil || len(list) != 0 {
		t.Fatalf("deleted list=%+v err=%v", list, err)
	}
}

func TestOracleOperationSchemesRejectInvalidConfigBeforePersisting(t *testing.T) {
	store := &operationSchemeMemoryStore{}
	service := &ExcelOperationSchemeService{store: store, normalize: func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("invalid mapping")
	}}
	if _, err := service.Save(t.Context(), "方法", ExcelOperationOracleImport, json.RawMessage(`{}`)); err == nil {
		t.Fatal("invalid config accepted")
	}
	if _, err := service.List(t.Context(), "oracle_export"); err == nil {
		t.Fatal("unsupported operation accepted")
	}
	if len(store.rows) != 0 {
		t.Fatal("invalid config persisted")
	}
}
