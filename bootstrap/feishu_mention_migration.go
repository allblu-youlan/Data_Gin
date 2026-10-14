package bootstrap

import (
	"fmt"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

func finalizeFeishuMentionReportMigration(db *gorm.DB) error {
	// AutoMigrate creates the replacement constraint first. Keep the old one
	// until the new five-column key exists, so an upgrade never loses uniqueness.
	migrator := db.Migrator()
	if !migrator.HasIndex(&model.FeishuDailyReport{}, "uk_feishu_report_kind") {
		return fmt.Errorf("Feishu report kind unique index is missing")
	}
	if migrator.HasIndex(&model.FeishuDailyReport{}, "uk_feishu_report") {
		if err := migrator.DropIndex(&model.FeishuDailyReport{}, "uk_feishu_report"); err != nil {
			return fmt.Errorf("migrate Feishu report unique index: %w", err)
		}
	}
	return nil
}
