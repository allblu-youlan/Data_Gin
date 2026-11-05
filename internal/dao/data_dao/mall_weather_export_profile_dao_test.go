package data_dao

import (
	"context"
	"testing"

	"gin-biz-web-api/model"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestMallWeatherExportProfileDAORejectsInvalidStateBeforeDatabase(t *testing.T) {
	if _, err := (&MallWeatherExportProfileDAO{}).Save(context.Background(), nil, nil); err == nil {
		t.Fatal("Save() accepted an unconfigured DAO")
	}
	_, err := (&MallWeatherExportProfileDAO{}).List(
		context.Background(),
		MallWeatherExportProfileListQuery{Limit: 10},
	)
	if err == nil {
		t.Fatal("List() accepted an unconfigured DAO")
	}
	if _, err := (&MallWeatherExportProfileDAO{}).FindByID(context.Background(), 1); err == nil {
		t.Fatal("FindByID() accepted an unconfigured DAO")
	}
	if _, err := (&MallWeatherExportProfileDAO{}).EnsureSystemProfile(context.Background(), nil); err == nil {
		t.Fatal("EnsureSystemProfile() accepted an unconfigured DAO")
	}
}

func TestMallWeatherExportProfileJSONComparisonIgnoresDatabaseFormatting(t *testing.T) {
	left := model.JSONText(`{"timeZone":"Asia/Shanghai","datasets":[{"kind":"hourly"}]}`)
	right := model.JSONText(`{ "datasets": [{ "kind": "hourly" }], "timeZone": "Asia/Shanghai" }`)
	if !sameMallWeatherExportProfileJSON(left, right) {
		t.Fatal("sameMallWeatherExportProfileJSON() rejected semantically equal JSON")
	}
	if sameMallWeatherExportProfileJSON(left, model.JSONText(`{"timeZone":"UTC"}`)) {
		t.Fatal("sameMallWeatherExportProfileJSON() accepted different JSON")
	}
}

func TestMallWeatherExportProfileDuplicateErrorsAreConflicts(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "mysql duplicate", err: &mysqlDriver.MySQLError{Number: 1062, Message: "duplicate"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !isMySQLDuplicateKeyError(test.err) {
				t.Fatalf("isMySQLDuplicateKeyError(%v) = false", test.err)
			}
		})
	}
	if isMySQLDuplicateKeyError(context.Canceled) {
		t.Fatal("context cancellation was classified as a duplicate")
	}
}
