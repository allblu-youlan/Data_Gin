package job

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestDataProcessTaskReturnsTriggerFailureForRetry(t *testing.T) {
	want := errors.New("token storage unavailable")
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
	ctx := context.WithValue(t.Context(), dataProcessContextKey{}, "request-context")
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		if tx.Statement.Context != ctx {
			t.Error("task context did not reach the DAO")
		}
		switch destination := tx.Statement.Dest.(type) {
		case *model.RawData:
			*destination = model.RawData{
				BaseModel: &model.BaseModel{ID: 240056}, Metadata: `{"remark":"qimai_order"}`,
				RawContent: `{"params":{"orderNo":"order-240056"}}`,
			}
			tx.RowsAffected = 1
		case *[]model.TokenData:
			tx.AddError(want)
		default:
			t.Fatalf("unexpected query destination %T", destination)
		}
	}); err != nil {
		t.Fatal(err)
	}
	previousDB, previousLogger := database.DB, logger.Logger
	core, logs := observer.New(zap.InfoLevel)
	database.DB, logger.Logger = db, zap.New(core)
	t.Cleanup(func() { database.DB, logger.Logger = previousDB, previousLogger })

	err = HandleDataProcessTask(ctx, NewDataProcessTask(240056))
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "raw_data_id=240056") {
		t.Errorf("task error=%v, want original failure with raw_data_id", err)
	}
	if logs.FilterMessage("数据处理任务完成").Len() != 0 {
		t.Error("failed trigger was logged as completed")
	}
	if logs.FilterLevelExact(zap.ErrorLevel).Len() != 0 {
		t.Error("task logged a duplicate error before the common Asynq error handler")
	}
}

type dataProcessContextKey struct{}
