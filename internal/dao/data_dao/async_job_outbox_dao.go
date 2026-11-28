package data_dao

import (
	"context"
	"fmt"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxOutboxClaimSize = 500

type AsyncJobOutboxDAO struct {
	db *gorm.DB
}

func NewAsyncJobOutboxDAO(databases ...*gorm.DB) *AsyncJobOutboxDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &AsyncJobOutboxDAO{db: db}
}

func (dao *AsyncJobOutboxDAO) WithDB(db *gorm.DB) *AsyncJobOutboxDAO {
	return &AsyncJobOutboxDAO{db: db}
}

func (dao *AsyncJobOutboxDAO) Create(ctx context.Context, row *model.AsyncJobOutbox) error {
	if row == nil {
		return fmt.Errorf("outbox: create nil row")
	}
	if err := dao.db.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("outbox: create: %w", err)
	}
	return nil
}

func (dao *AsyncJobOutboxDAO) CreateBatchIgnoreTaskConflicts(ctx context.Context, rows []model.AsyncJobOutbox) (int64, error) {
	if dao == nil || dao.db == nil || ctx == nil {
		return 0, fmt.Errorf("outbox: batch store is not configured")
	}
	if len(rows) == 0 {
		return 0, nil
	}
	result := dao.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_key"}}, DoNothing: true}).
		CreateInBatches(&rows, 200)
	if result.Error != nil {
		return 0, fmt.Errorf("outbox: create batch: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// ClaimBatch atomically claims ready unpublished tasks. MySQL 8 SKIP LOCKED
// lets multiple dispatchers make progress without processing the same row.
func (dao *AsyncJobOutboxDAO) ClaimBatch(ctx context.Context, workerID string, taskTypes []string, now time.Time, lockTimeout time.Duration, limit int) ([]model.AsyncJobOutbox, error) {
	if workerID == "" || len(taskTypes) == 0 {
		return nil, fmt.Errorf("outbox: worker id and task types are required")
	}
	limit = normalizeOutboxClaimSize(limit)
	if lockTimeout <= 0 {
		lockTimeout = time.Minute
	}
	lockExpiredAt := now.Add(-lockTimeout)
	var claimed []model.AsyncJobOutbox

	err := database.TransactionWithBeginRetry(dao.db.WithContext(ctx), func(tx *gorm.DB) error {
		if err := buildOutboxClaimQuery(tx, taskTypes, now, lockExpiredAt, limit).Find(&claimed).Error; err != nil {
			return fmt.Errorf("outbox: select claim batch: %w", err)
		}
		if len(claimed) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(claimed))
		for i := range claimed {
			ids = append(ids, claimed[i].ID)
			claimed[i].LockedBy = workerID
			lockedAt := now.UTC()
			claimed[i].LockedAt = &lockedAt
		}
		result := tx.Model(&model.AsyncJobOutbox{}).
			Where("id IN ? AND published_at IS NULL", ids).
			Updates(map[string]interface{}{
				"locked_by": workerID,
				"locked_at": now.UTC(),
			})
		if result.Error != nil {
			return fmt.Errorf("outbox: mark claimed: %w", result.Error)
		}
		if result.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("outbox: claim count changed during transaction")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func buildOutboxClaimQuery(db *gorm.DB, taskTypes []string, now, lockExpiredAt time.Time, limit int) *gorm.DB {
	return db.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("published_at IS NULL").
		Where("task_type IN ?", taskTypes).
		Where("available_at <= ?", now.UTC()).
		Where("(locked_at IS NULL OR locked_at < ?)", lockExpiredAt.UTC()).
		Order("id ASC").
		Limit(limit)
}

func (dao *AsyncJobOutboxDAO) MarkPublished(ctx context.Context, id uint, publishedAt time.Time) error {
	result := dao.db.WithContext(ctx).Model(&model.AsyncJobOutbox{}).
		Where("id = ? AND published_at IS NULL", id).
		Updates(map[string]interface{}{
			"published_at":    publishedAt.UTC(),
			"locked_by":       "",
			"locked_at":       nil,
			"last_error_safe": "",
		})
	if result.Error != nil {
		return fmt.Errorf("outbox: mark published: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("outbox: pending row not found")
	}
	return nil
}

func (dao *AsyncJobOutboxDAO) MarkFailed(ctx context.Context, id uint, availableAt time.Time, safeError string) error {
	result := dao.db.WithContext(ctx).Model(&model.AsyncJobOutbox{}).
		Where("id = ? AND published_at IS NULL", id).
		Updates(map[string]interface{}{
			"attempts":        gorm.Expr("attempts + 1"),
			"available_at":    availableAt.UTC(),
			"last_error_safe": safeError,
			"locked_by":       "",
			"locked_at":       nil,
		})
	if result.Error != nil {
		return fmt.Errorf("outbox: mark failed: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("outbox: pending row not found")
	}
	return nil
}

func normalizeOutboxClaimSize(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > maxOutboxClaimSize {
		return maxOutboxClaimSize
	}
	return limit
}
