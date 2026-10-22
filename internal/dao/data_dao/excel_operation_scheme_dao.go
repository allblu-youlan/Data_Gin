package data_dao

import (
	"context"

	"gin-biz-web-api/model"
)

func (dao *ExcelMatchJobDAO) GetOperationScheme(ctx context.Context, operation string, id uint) (*model.ExcelMatchScheme, error) {
	var row model.ExcelMatchScheme
	if err := dao.db.WithContext(ctx).Where("id = ? AND operation = ?", id, operation).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (dao *ExcelMatchJobDAO) DeleteOperationScheme(ctx context.Context, operation string, id uint) error {
	return dao.db.WithContext(ctx).Where("id = ? AND operation = ?", id, operation).Delete(&model.ExcelMatchScheme{}).Error
}
