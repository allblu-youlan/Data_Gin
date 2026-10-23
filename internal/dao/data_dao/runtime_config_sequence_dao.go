package data_dao

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NextSequence persists a shared counter in the existing runtime configuration
// table. Its row lock also serializes callers on different application nodes.
func (dao *RuntimeConfigDAO) NextSequence(ctx context.Context, key string) (uint64, error) {
	if dao == nil || dao.db == nil {
		return 0, errors.New("sequence database is unavailable")
	}
	if key == "" || len(key) > 100 {
		return 0, errors.New("sequence key is invalid")
	}
	var next uint64
	err := dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := int(time.Now().Unix())
		initial := model.RuntimeConfig{ConfigKey: key, ConfigJSON: "0"}
		initial.CreatedAt, initial.UpdatedAt = now, now
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		var current model.RuntimeConfig
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "config_json").Where("config_key = ?", key).First(&current).Error; err != nil {
			return err
		}
		value, err := strconv.ParseUint(current.ConfigJSON, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid sequence value: %w", err)
		}
		next = value + 1
		if next == 0 {
			return errors.New("sequence is exhausted")
		}
		return tx.Model(&model.RuntimeConfig{}).Where("config_key = ?", key).
			Updates(map[string]interface{}{"config_json": strconv.FormatUint(next, 10), "updated_at": now}).Error
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}
