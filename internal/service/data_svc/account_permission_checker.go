package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

type accountPermissionAuthorizer interface {
	HasPermission(context.Context, model.User, string) (bool, error)
}

type openAPIAccountPermissionChecker interface {
	HasPermission(context.Context, uint, string, time.Time) (bool, error)
}

type accountPermissionChecker struct {
	loadUser           func(context.Context, uint) (model.User, error)
	authorizer         accountPermissionAuthorizer
	openAPIPermissions openAPIAccountPermissionChecker
}

func newAccountPermissionChecker(db *gorm.DB) *accountPermissionChecker {
	return &accountPermissionChecker{
		loadUser: func(ctx context.Context, userID uint) (model.User, error) {
			if db == nil {
				return model.User{}, fmt.Errorf("account permission checker: database unavailable")
			}
			var user model.User
			err := db.WithContext(ctx).
				Select("id", "account_type", "status").
				First(&user, userID).Error
			return user, err
		},
		authorizer:         auth_svc.NewAuthorizer(db),
		openAPIPermissions: data_dao.NewMallWeatherPermissionDAO(db),
	}
}

func (checker *accountPermissionChecker) HasPermission(
	ctx context.Context,
	userID uint,
	permission string,
	now time.Time,
) (bool, error) {
	if checker == nil || checker.loadUser == nil || checker.authorizer == nil || checker.openAPIPermissions == nil || ctx == nil || userID == 0 {
		return false, fmt.Errorf("account permission checker: invalid lookup")
	}
	user, err := checker.loadUser(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("account permission checker: load user: %w", err)
	}
	if user.Status != model.AccountStatusActive {
		return false, nil
	}
	var allowed bool
	switch user.AccountType {
	case model.AccountTypeConsole:
		allowed, err = checker.authorizer.HasPermission(ctx, user, permission)
	case model.AccountTypeOpenAPI:
		allowed, err = checker.openAPIPermissions.HasPermission(ctx, user.ID, permission, now)
	default:
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("account permission checker: authorize user: %w", err)
	}
	return allowed, nil
}
