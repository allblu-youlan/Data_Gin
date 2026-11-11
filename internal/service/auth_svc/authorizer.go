package auth_svc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

const maximumPermissionCodeBytes = 64

type authorizationRepository interface {
	ActiveConsoleRoles(ctx context.Context, userID uint) ([]authorizationRole, error)
	ConsoleRoleHasPermission(ctx context.Context, userID uint, code string) (bool, error)
	ConsoleHasReportCategoryAction(ctx context.Context, userID uint, action string) (bool, error)
	OpenAPIHasAnyPermission(ctx context.Context, userID uint, codes []string, now time.Time) (bool, error)
}

type authorizationRole struct {
	Code    string
	IsSuper bool
}

// Authorizer resolves permissions for both internal console and open API users.
// Invalid input is denied without touching the repository.
type Authorizer struct {
	repository authorizationRepository
	now        func() time.Time
}

func NewAuthorizer(db *gorm.DB) *Authorizer {
	return &Authorizer{repository: &gormAuthorizationRepository{db: db}, now: time.Now}
}

func newAuthorizer(repository authorizationRepository) *Authorizer {
	return &Authorizer{repository: repository, now: time.Now}
}

func (a *Authorizer) HasPermission(ctx context.Context, user model.User, code string) (bool, error) {
	code = strings.TrimSpace(code)
	if ctx == nil || user.BaseModel == nil || user.ID == 0 || user.Status != model.AccountStatusActive || !knownPermissionCode(code) {
		return false, nil
	}
	if a == nil || a.repository == nil || a.now == nil {
		return false, fmt.Errorf("authorizer: service unavailable")
	}

	switch user.AccountType {
	case model.AccountTypeConsole:
		roles, err := a.repository.ActiveConsoleRoles(ctx, user.ID)
		if err != nil {
			return false, fmt.Errorf("authorizer: load console roles: %w", err)
		}
		for _, role := range roles {
			if role.IsSuper || role.Code == model.RoleCodeSuperAdmin {
				return true, nil
			}
		}
		if len(roles) > 0 {
			for _, candidate := range permissionCandidates(code) {
				allowed, err := a.repository.ConsoleRoleHasPermission(ctx, user.ID, candidate)
				if err != nil {
					return false, fmt.Errorf("authorizer: check console permission: %w", err)
				}
				if allowed {
					return true, nil
				}
			}
		}
		if action := reportCategoryActionForPermission(code); action != "" {
			allowed, err := a.repository.ConsoleHasReportCategoryAction(ctx, user.ID, action)
			if err != nil {
				return false, fmt.Errorf("authorizer: check report category permission: %w", err)
			}
			return allowed, nil
		}
		return false, nil
	case model.AccountTypeOpenAPI:
		allowed, err := a.repository.OpenAPIHasAnyPermission(ctx, user.ID, permissionCandidates(code), a.now().UTC())
		if err != nil {
			return false, fmt.Errorf("authorizer: check open api permission: %w", err)
		}
		return allowed, nil
	default:
		return false, nil
	}
}

func permissionCandidates(code string) []string {
	switch code {
	case model.PermissionSourceRead:
		return []string{model.PermissionSourceRead, model.PermissionSourceManage}
	case model.PermissionPipelineRead:
		return []string{model.PermissionPipelineRead, model.PermissionPipelineManage}
	case model.PermissionDeliveryRead:
		return []string{model.PermissionDeliveryRead, model.PermissionDeliveryManage}
	default:
		return model.OpenAPIPermissionCandidates(code)
	}
}

func knownPermissionCode(code string) bool {
	if code == "" || len([]byte(code)) > maximumPermissionCodeBytes {
		return false
	}
	for _, known := range PermissionCodes() {
		if code == known {
			return true
		}
	}
	return false
}

type gormAuthorizationRepository struct{ db *gorm.DB }

func (r *gormAuthorizationRepository) database(ctx context.Context) (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("authorizer: database unavailable")
	}
	return r.db.WithContext(ctx), nil
}

func (r *gormAuthorizationRepository) ActiveConsoleRoles(ctx context.Context, userID uint) ([]authorizationRole, error) {
	db, err := r.database(ctx)
	if err != nil {
		return nil, err
	}
	var roles []authorizationRole
	err = db.Table("roles").
		Select("roles.code, roles.is_super").
		Joins("INNER JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ? AND roles.status = ?", userID, model.RoleStatusActive).
		Find(&roles).Error
	return roles, err
}

func (r *gormAuthorizationRepository) ConsoleRoleHasPermission(ctx context.Context, userID uint, code string) (bool, error) {
	db, err := r.database(ctx)
	if err != nil {
		return false, err
	}
	var marker int
	err = db.Table("role_permissions").
		Select("1").
		Joins("INNER JOIN roles ON roles.id = role_permissions.role_id").
		Joins("INNER JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ? AND roles.status = ? AND role_permissions.permission_code = ?", userID, model.RoleStatusActive, code).
		Limit(1).Scan(&marker).Error
	return marker == 1, err
}

func (r *gormAuthorizationRepository) ConsoleHasReportCategoryAction(ctx context.Context, userID uint, action string) (bool, error) {
	db, err := r.database(ctx)
	if err != nil {
		return false, err
	}
	actions, err := loadConsoleReportCategoryActions(ctx, db, userID)
	if err != nil {
		return false, err
	}
	for _, candidate := range actions {
		if candidate == action {
			return true, nil
		}
	}
	return false, nil
}

func (r *gormAuthorizationRepository) OpenAPIHasAnyPermission(ctx context.Context, userID uint, codes []string, now time.Time) (bool, error) {
	db, err := r.database(ctx)
	if err != nil {
		return false, err
	}
	var marker int
	err = db.Table("mall_weather_user_permissions").
		Select("1").
		Where("user_id = ? AND permission IN ? AND (expires_at IS NULL OR expires_at > ?)", userID, codes, now.UTC()).
		Limit(1).Scan(&marker).Error
	return marker == 1, err
}
