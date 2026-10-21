package data_dao

import (
	"context"
	"time"

	"gin-biz-web-api/model"
)

// The output extension is known after rendering. Unlike an uploaded Excel
// source, the Oracle export can be recreated on another worker during retry.
func (dao *ExcelMatchJobDAO) UpdateOracleExportResultPath(ctx context.Context, id uint, workDir, resultPath string) error {
	return dao.db.WithContext(ctx).Model(&model.ExcelMatchJob{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]interface{}{"work_dir": workDir, "result_file_path": resultPath, "updated_at": time.Now().Unix()}).Error
}

func (dao *ExcelMatchJobDAO) DeleteOracleExportScheme(ctx context.Context, id uint) error {
	return dao.db.WithContext(ctx).Where("id = ? AND operation = ?", id, "oracle_export").Delete(&model.ExcelMatchScheme{}).Error
}
