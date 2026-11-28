package job

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestRegisteredLegacyHandlersRejectInvalidJSONWithoutDuplicateErrors(t *testing.T) {
	for _, test := range []struct {
		taskType string
		handler  func(context.Context, *asynq.Task) error
	}{
		{TypeFoo, HandFooTask},
		{TypeDataProcess, HandleDataProcessTask},
		{TypeSalesSync, HandleSalesSyncTask},
		{TypeXianOrderSync, HandleXianOrderSyncTask},
		{TypeYouzanSync, HandleYouzanSyncTask},
		{TypeYouzanReturn, HandleYouzanReturnTask},
	} {
		t.Run(test.taskType, func(t *testing.T) {
			logs := observeTaskLogs(t)
			err := test.handler(t.Context(), asynq.NewTask(test.taskType, []byte(`{"private":"payload-secret"`)))
			var syntaxError *json.SyntaxError
			if !errors.Is(err, asynq.SkipRetry) || !errors.As(err, &syntaxError) || !strings.Contains(err.Error(), test.taskType) {
				t.Fatalf("invalid payload lost terminal classification or JSON cause: %v", err)
			}
			if logs.FilterLevelExact(zap.ErrorLevel).Len() != 0 {
				t.Fatal("invalid task emitted Error before the common error handler")
			}
			if strings.Contains(err.Error(), "payload-secret") {
				t.Fatal("invalid payload was included in returned error")
			}
		})
	}
}

func TestLegacyTaskDatabaseFailuresKeepBusinessContextWithoutDuplicateErrors(t *testing.T) {
	for _, test := range []struct {
		name, payload string
		taskType      string
		handler       func(context.Context, *asynq.Task) error
		want          []string
	}{
		{"sales", `{"ShopCode":"YXHZR001","Status":"70","StoreCode":"store","MallItemCode":"item"}`, TypeSalesSync, HandleSalesSyncTask, []string{"YXHZR001", "70"}},
		{"raw record", `{"raw_data_id":240056}`, TypeDataProcess, HandleDataProcessTask, []string{"raw_data_id=240056"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			logs := observeTaskLogs(t)
			cause := errors.New("database read interrupted")
			db := taskFailureTestDB(t)
			ctx := context.WithValue(t.Context(), dataProcessContextKey{}, test.name)
			queries := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				queries++
				if tx.Statement.Context != ctx {
					t.Error("task context did not reach the database")
				}
				tx.AddError(cause)
			}); err != nil {
				t.Fatal(err)
			}
			err := test.handler(ctx, asynq.NewTask(test.taskType, []byte(test.payload)))
			if !errors.Is(err, cause) || errors.Is(err, asynq.SkipRetry) || queries != 1 {
				t.Fatalf("database failure identity/classification changed: %v queries=%d", err, queries)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks business context %q", err, want)
				}
			}
			if logs.FilterLevelExact(zap.ErrorLevel).Len() != 0 {
				t.Error("retryable database failure emitted duplicate Error")
			}
		})
	}
}

func TestDataProcessMetadataFailureKeepsRawRecordID(t *testing.T) {
	logs := observeTaskLogs(t)
	db := taskFailureTestDB(t)
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		*tx.Statement.Dest.(*model.RawData) = model.RawData{Metadata: `{"private":"metadata-secret"`}
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	err := HandleDataProcessTask(t.Context(), NewDataProcessTask(240056))
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) || !strings.Contains(err.Error(), "raw_data_id=240056") || strings.Contains(err.Error(), "metadata-secret") {
		t.Fatalf("metadata failure lost context or leaked content: %v", err)
	}
	if logs.FilterLevelExact(zap.ErrorLevel).Len() != 0 {
		t.Error("metadata failure emitted duplicate Error")
	}
}

func observeTaskLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	return logs
}

func taskFailureTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	return db
}
