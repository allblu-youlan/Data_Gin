package data_dao

import (
	"context"
	"errors"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

type RuntimeConfigDAO struct {
	db *gorm.DB
}

func NewRuntimeConfigDAO(databases ...*gorm.DB) *RuntimeConfigDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &RuntimeConfigDAO{db: db}
}

func (dao *RuntimeConfigDAO) GetValue(ctx context.Context, key string) (string, bool, error) {
	var cfg model.RuntimeConfig
	err := dao.db.WithContext(ctx).Where("config_key = ?", key).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return cfg.ConfigJSON, true, nil
}

func (dao *RuntimeConfigDAO) SetValue(ctx context.Context, key string, value string) error {
	now := int(time.Now().Unix())
	var cfg model.RuntimeConfig
	err := dao.db.WithContext(ctx).Where("config_key = ?", key).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cfg = model.RuntimeConfig{
			ConfigKey:  key,
			ConfigJSON: value,
		}
		cfg.CreatedAt = now
		cfg.UpdatedAt = now
		return dao.db.WithContext(ctx).Create(&cfg).Error
	}
	if err != nil {
		return err
	}

	return dao.db.WithContext(ctx).
		Model(&model.RuntimeConfig{}).
		Where("config_key = ?", key).
		Updates(map[string]interface{}{
			"config_json": value,
			"updated_at":  now,
		}).
		Error
}
