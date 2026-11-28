package data_dao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrBojunOracleSyncStateNotInitialized = errors.New("bojun Oracle sync state is not initialized")
	ErrBojunOracleSyncLeaseLost           = errors.New("bojun Oracle sync lease is unavailable or lost")
)

type BojunOracleSyncStateDAO struct {
	db *gorm.DB
}

func NewBojunOracleSyncStateDAO(databases ...*gorm.DB) *BojunOracleSyncStateDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &BojunOracleSyncStateDAO{db: db}
}

func (dao *BojunOracleSyncStateDAO) Get(ctx context.Context, sourceCode string) (*model.BojunOracleSyncState, error) {
	sourceCode = strings.TrimSpace(sourceCode)
	if dao == nil || dao.db == nil || ctx == nil || sourceCode == "" {
		return nil, gorm.ErrInvalidData
	}
	var state model.BojunOracleSyncState
	err := dao.db.WithContext(ctx).Where("source_code = ?", sourceCode).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && !state.Initialized) {
		return nil, ErrBojunOracleSyncStateNotInitialized
	}
	if err != nil {
		return nil, fmt.Errorf("get bojun Oracle sync state: %w", err)
	}
	return &state, nil
}

func (dao *BojunOracleSyncStateDAO) Initialize(
	ctx context.Context,
	sourceCode string,
	retailID uint64,
	now time.Time,
) (*model.BojunOracleSyncState, bool, error) {
	sourceCode = strings.TrimSpace(sourceCode)
	if dao == nil || dao.db == nil || ctx == nil || sourceCode == "" || now.IsZero() {
		return nil, false, gorm.ErrInvalidData
	}
	unixNow := int(now.Unix())
	state := model.BojunOracleSyncState{
		SourceCode: sourceCode, LastRetailID: retailID, Initialized: true,
		CommonTimestampsField: model.CommonTimestampsField{CreatedAt: unixNow, UpdatedAt: unixNow},
	}
	created := dao.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&state)
	if created.Error != nil {
		return nil, false, fmt.Errorf("initialize bojun Oracle sync state: %w", created.Error)
	}
	initializedNow := created.RowsAffected == 1
	if !initializedNow {
		updated := dao.db.WithContext(ctx).
			Model(&model.BojunOracleSyncState{}).
			Where("source_code = ? AND initialized = ?", sourceCode, false).
			Updates(map[string]interface{}{
				"last_retail_id": retailID,
				"initialized":    true,
				"updated_at":     unixNow,
			})
		if updated.Error != nil {
			return nil, false, fmt.Errorf("initialize existing bojun Oracle sync state: %w", updated.Error)
		}
		initializedNow = updated.RowsAffected == 1
	}
	current, err := dao.Get(ctx, sourceCode)
	if err != nil {
		return nil, false, err
	}
	return current, initializedNow, nil
}

func (dao *BojunOracleSyncStateDAO) AcquireLease(
	ctx context.Context,
	sourceCode string,
	token string,
	now time.Time,
	ttl time.Duration,
) (*model.BojunOracleSyncState, bool, error) {
	sourceCode = strings.TrimSpace(sourceCode)
	token = strings.TrimSpace(token)
	if dao == nil || dao.db == nil || ctx == nil || sourceCode == "" || token == "" || now.IsZero() || ttl <= 0 {
		return nil, false, gorm.ErrInvalidData
	}
	expiresAt := now.Add(ttl)
	var affected int64
	err := database.TransactionWithBeginRetry(dao.db.WithContext(ctx), func(tx *gorm.DB) error {
		result := tx.Model(&model.BojunOracleSyncState{}).
			Where("source_code = ? AND initialized = ?", sourceCode, true).
			Where("lease_token = '' OR lease_token = ? OR lease_expires_at IS NULL OR lease_expires_at <= ?", token, now).
			Updates(map[string]interface{}{
				"lease_token":      token,
				"lease_expires_at": expiresAt,
				"updated_at":       now.Unix(),
			})
		affected = result.RowsAffected
		return result.Error
	})
	if err != nil {
		return nil, false, fmt.Errorf("acquire bojun Oracle sync lease: %w", err)
	}
	if affected != 1 {
		if _, err := dao.Get(ctx, sourceCode); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	state, err := dao.Get(ctx, sourceCode)
	if err != nil {
		return nil, false, err
	}
	return state, true, nil
}

func (dao *BojunOracleSyncStateDAO) Advance(
	ctx context.Context,
	sourceCode string,
	token string,
	expectedRetailID uint64,
	nextRetailID uint64,
	now time.Time,
	ttl time.Duration,
) error {
	sourceCode = strings.TrimSpace(sourceCode)
	token = strings.TrimSpace(token)
	if dao == nil || dao.db == nil || ctx == nil || sourceCode == "" || token == "" ||
		now.IsZero() || ttl <= 0 || nextRetailID < expectedRetailID {
		return gorm.ErrInvalidData
	}
	result := dao.db.WithContext(ctx).
		Model(&model.BojunOracleSyncState{}).
		Where(
			"source_code = ? AND initialized = ? AND lease_token = ? AND lease_expires_at > ? AND last_retail_id = ?",
			sourceCode, true, token, now, expectedRetailID,
		).
		Updates(map[string]interface{}{
			"last_retail_id":    nextRetailID,
			"lease_expires_at":  now.Add(ttl),
			"last_succeeded_at": now,
			"updated_at":        now.Unix(),
		})
	if result.Error != nil {
		return fmt.Errorf("advance bojun Oracle sync state: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrBojunOracleSyncLeaseLost
	}
	return nil
}

func (dao *BojunOracleSyncStateDAO) RenewLease(
	ctx context.Context,
	sourceCode string,
	token string,
	now time.Time,
	ttl time.Duration,
) error {
	sourceCode = strings.TrimSpace(sourceCode)
	token = strings.TrimSpace(token)
	if dao == nil || dao.db == nil || ctx == nil || sourceCode == "" || token == "" || now.IsZero() || ttl <= 0 {
		return gorm.ErrInvalidData
	}
	result := dao.db.WithContext(ctx).
		Model(&model.BojunOracleSyncState{}).
		Where("source_code = ? AND initialized = ? AND lease_token = ? AND lease_expires_at > ?", sourceCode, true, token, now).
		Updates(map[string]interface{}{
			"lease_expires_at": now.Add(ttl),
			"updated_at":       now.Unix(),
		})
	if result.Error != nil {
		return fmt.Errorf("renew bojun Oracle sync lease: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrBojunOracleSyncLeaseLost
	}
	return nil
}

func (dao *BojunOracleSyncStateDAO) ReleaseLease(ctx context.Context, sourceCode string, token string, now time.Time) error {
	sourceCode = strings.TrimSpace(sourceCode)
	token = strings.TrimSpace(token)
	if dao == nil || dao.db == nil || ctx == nil || sourceCode == "" || token == "" || now.IsZero() {
		return gorm.ErrInvalidData
	}
	result := dao.db.WithContext(ctx).
		Model(&model.BojunOracleSyncState{}).
		Where("source_code = ? AND lease_token = ?", sourceCode, token).
		Updates(map[string]interface{}{
			"lease_token":      "",
			"lease_expires_at": nil,
			"updated_at":       now.Unix(),
		})
	if result.Error != nil {
		return fmt.Errorf("release bojun Oracle sync lease: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrBojunOracleSyncLeaseLost
	}
	return nil
}
