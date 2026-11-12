package auth_svc

import (
	"context"
	"fmt"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SyncOpenMonitoringPermissionSeeds adds the monitoring catalog and system role
// grants using the caller's migration transaction. It preserves account scopes,
// custom grants and all other access-control settings.
func SyncOpenMonitoringPermissionSeeds(ctx context.Context, tx *gorm.DB) error {
	if ctx == nil || tx == nil {
		return fmt.Errorf("sync monitoring permissions: invalid database")
	}
	permissions := make([]model.Permission, 0, 4)
	for _, permission := range PermissionCatalog() {
		switch permission.Code {
		case model.PermissionMonitoringAPIRead, model.PermissionMonitoringHourlyRead,
			model.PermissionMonitoringDailyRead, model.PermissionMonitoringAreaRead:
			permissions = append(permissions, permission)
		}
	}
	if len(permissions) != 4 {
		return fmt.Errorf("sync monitoring permissions: incomplete catalog")
	}
	tx = tx.WithContext(ctx)
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "module", "description", "risk_level", "api_grantable", "sort", "updated_at",
		}),
	}).Create(&permissions).Error; err != nil {
		return fmt.Errorf("sync monitoring permissions: catalog: %w", err)
	}
	var roles []model.Role
	if err := tx.Where("code IN ? AND is_system = ?",
		[]string{model.RoleCodeAdmin, model.RoleCodeOperator}, true).Find(&roles).Error; err != nil {
		return fmt.Errorf("sync monitoring permissions: read system roles: %w", err)
	}
	grants := make([]model.RolePermission, 0, len(roles)*len(permissions))
	for _, role := range roles {
		for _, permission := range permissions {
			grants = append(grants, model.RolePermission{RoleID: role.ID, PermissionCode: permission.Code})
		}
	}
	if len(grants) == 0 {
		return nil
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&grants).Error; err != nil {
		return fmt.Errorf("sync monitoring permissions: grant system roles: %w", err)
	}
	return nil
}
