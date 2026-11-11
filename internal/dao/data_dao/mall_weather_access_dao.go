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

var ErrAPIIdempotencyNotFound = errors.New("api idempotency: not found")

type MallWeatherPermissionDAO struct {
	db *gorm.DB
}

func NewMallWeatherPermissionDAO(databases ...*gorm.DB) *MallWeatherPermissionDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &MallWeatherPermissionDAO{db: db}
}

func (dao *MallWeatherPermissionDAO) WithDB(db *gorm.DB) *MallWeatherPermissionDAO {
	return &MallWeatherPermissionDAO{db: db}
}

// GrantPermanentPermissions idempotently grants a permanent permission set.
// Existing expired grants are restored by clearing expires_at.
func (dao *MallWeatherPermissionDAO) GrantPermanentPermissions(
	ctx context.Context,
	userID uint,
	grantedBy uint,
	permissions []string,
) error {
	if dao == nil || dao.db == nil || ctx == nil || userID == 0 || grantedBy == 0 || len(permissions) == 0 {
		return fmt.Errorf("mall weather permission: invalid permanent grant")
	}

	seen := make(map[string]struct{}, len(permissions))
	grants := make([]model.MallWeatherUserPermission, 0, len(permissions))
	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" || len(permission) > 64 {
			return fmt.Errorf("mall weather permission: invalid permanent grant")
		}
		if _, exists := seen[permission]; exists {
			return fmt.Errorf("mall weather permission: duplicate permanent grant")
		}
		seen[permission] = struct{}{}
		grants = append(grants, model.MallWeatherUserPermission{
			UserID:     userID,
			Permission: permission,
			GrantedBy:  grantedBy,
			ExpiresAt:  nil,
		})
	}

	result := dao.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "permission"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"granted_by", "expires_at", "updated_at",
		}),
	}).Create(&grants)
	if result.Error != nil {
		return fmt.Errorf("mall weather permission: grant permanent permissions: %w", result.Error)
	}
	return nil
}

func (dao *MallWeatherPermissionDAO) HasPermission(ctx context.Context, userID uint, permission string, now time.Time) (bool, error) {
	permission = strings.TrimSpace(permission)
	if dao == nil || dao.db == nil || ctx == nil || userID == 0 || permission == "" || len(permission) > 64 {
		return false, fmt.Errorf("mall weather permission: invalid lookup")
	}
	var match struct {
		Exists int `gorm:"column:permission_exists"`
	}
	result := dao.db.WithContext(ctx).
		Model(&model.MallWeatherUserPermission{}).
		Select("1 AS permission_exists").
		Where("user_id = ? AND permission IN ?", userID, model.OpenAPIPermissionCandidates(permission)).
		Where("expires_at IS NULL OR expires_at > ?", now.UTC()).
		Limit(1).
		Find(&match)
	if result.Error != nil {
		return false, fmt.Errorf("mall weather permission: lookup: %w", result.Error)
	}
	return result.RowsAffected == 1 && match.Exists == 1, nil
}

type APIIdempotencyDAO struct {
	db *gorm.DB
}

func NewAPIIdempotencyDAO(databases ...*gorm.DB) *APIIdempotencyDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &APIIdempotencyDAO{db: db}
}

func (dao *APIIdempotencyDAO) WithDB(db *gorm.DB) *APIIdempotencyDAO {
	return &APIIdempotencyDAO{db: db}
}

// Reserve inserts a placeholder without overwriting an existing request. It
// must be called inside the same transaction as the protected business write.
func (dao *APIIdempotencyDAO) Reserve(ctx context.Context, record *model.APIIdempotencyRecord) (bool, error) {
	if record == nil || record.OperationScope == "" || record.ActorUserID == 0 || len(record.KeyHash) != 64 || len(record.RequestHash) != 64 {
		return false, fmt.Errorf("api idempotency: invalid reservation")
	}
	if record.ResponseJSON == "" {
		record.ResponseJSON = model.JSONText(`{}`)
	}
	result := dao.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(record)
	if result.Error != nil {
		return false, fmt.Errorf("api idempotency: reserve: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (dao *APIIdempotencyDAO) FindForUpdate(ctx context.Context, operationScope string, actorUserID uint, keyHash string) (*model.APIIdempotencyRecord, error) {
	var record model.APIIdempotencyRecord
	err := dao.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("operation_scope = ? AND actor_user_id = ? AND key_hash = ?", operationScope, actorUserID, keyHash).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAPIIdempotencyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("api idempotency: find: %w", err)
	}
	return &record, nil
}

func (dao *APIIdempotencyDAO) Complete(ctx context.Context, id, resourceID uint, httpStatus int, responseJSON model.JSONText) error {
	if id == 0 || resourceID == 0 || httpStatus < 200 || httpStatus > 299 || responseJSON == "" {
		return fmt.Errorf("api idempotency: invalid completion")
	}
	result := dao.db.WithContext(ctx).
		Model(&model.APIIdempotencyRecord{}).
		Where("id = ? AND resource_id = 0", id).
		Updates(map[string]interface{}{
			"resource_id":   resourceID,
			"http_status":   httpStatus,
			"response_json": responseJSON,
		})
	if result.Error != nil {
		return fmt.Errorf("api idempotency: complete: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("api idempotency: reservation is not pending")
	}
	return nil
}
