package data_dao

import (
	"context"
	"gin-biz-web-api/model"
	"time"
)

// Workflows contain writes. Never replay a running/failed workflow after a
// queue redelivery, since its Oracle transaction may already have committed.
func (dao *ExcelMatchJobDAO) ClaimWorkflow(ctx context.Context, id uint) (bool, error) {
	now := time.Now()
	result := dao.db.WithContext(ctx).Model(&model.ExcelMatchJob{}).
		Where("id = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)", id, "pending", now).
		Updates(map[string]interface{}{"status": "running", "started_at": model.TimeNormal{Time: now}, "updated_at": now.Unix()})
	return result.RowsAffected == 1, result.Error
}

func (dao *ExcelMatchJobDAO) DeleteWorkflow(ctx context.Context, id uint) error {
	return dao.db.WithContext(ctx).Where("id = ? AND operation = ?", id, "excel_workflow").Delete(&model.ExcelMatchScheme{}).Error
}
