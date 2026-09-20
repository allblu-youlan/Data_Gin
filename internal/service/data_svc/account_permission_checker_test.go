package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

func TestAccountPermissionCheckerDelegatesByAccountType(t *testing.T) {
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name               string
		accountType        string
		wantAuthorizerCall bool
		wantOpenAPICall    bool
	}{
		{name: "console account uses role authorizer", accountType: model.AccountTypeConsole, wantAuthorizerCall: true},
		{name: "open API account preserves explicit grant lookup", accountType: model.AccountTypeOpenAPI, wantOpenAPICall: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := model.User{
				BaseModel:   &model.BaseModel{ID: 43},
				AccountType: tt.accountType,
				Status:      model.AccountStatusActive,
			}
			authorizer := &recordingAccountPermissionAuthorizer{allowed: true}
			openAPIPermissions := &recordingOpenAPIAccountPermissionChecker{allowed: true}
			checker := &accountPermissionChecker{
				loadUser: func(_ context.Context, userID uint) (model.User, error) {
					if userID != user.ID {
						t.Fatalf("load user ID = %d, want %d", userID, user.ID)
					}
					return user, nil
				},
				authorizer:         authorizer,
				openAPIPermissions: openAPIPermissions,
			}

			allowed, err := checker.HasPermission(t.Context(), user.ID, model.PermissionMallRead, now)
			if err != nil || !allowed {
				t.Fatalf("HasPermission() = %t, %v, want true, nil", allowed, err)
			}
			if authorizer.called != tt.wantAuthorizerCall || openAPIPermissions.called != tt.wantOpenAPICall {
				t.Fatalf("provider calls = authorizer %t, open API %t", authorizer.called, openAPIPermissions.called)
			}
			if tt.wantAuthorizerCall && (authorizer.user.ID != user.ID || authorizer.permission != model.PermissionMallRead) {
				t.Fatalf("authorizer call = user %+v, permission %q", authorizer.user, authorizer.permission)
			}
			if tt.wantOpenAPICall && (openAPIPermissions.userID != user.ID || openAPIPermissions.permission != model.PermissionMallRead || !openAPIPermissions.now.Equal(now)) {
				t.Fatalf("open API call = user %d, permission %q, now %v", openAPIPermissions.userID, openAPIPermissions.permission, openAPIPermissions.now)
			}
		})
	}
}

func TestAccountPermissionCheckerFailsClosed(t *testing.T) {
	providerError := errors.New("permission provider unavailable")
	tests := []struct {
		name       string
		checker    *accountPermissionChecker
		userID     uint
		wantError  bool
		wantCalled bool
	}{
		{name: "missing checker", wantError: true},
		{name: "missing user ID", checker: &accountPermissionChecker{}, wantError: true},
		{
			name: "user not found",
			checker: &accountPermissionChecker{
				loadUser:           func(context.Context, uint) (model.User, error) { return model.User{}, gorm.ErrRecordNotFound },
				authorizer:         &recordingAccountPermissionAuthorizer{},
				openAPIPermissions: &recordingOpenAPIAccountPermissionChecker{},
			},
			userID: 43,
		},
		{
			name: "user lookup failure",
			checker: &accountPermissionChecker{
				loadUser:           func(context.Context, uint) (model.User, error) { return model.User{}, providerError },
				authorizer:         &recordingAccountPermissionAuthorizer{},
				openAPIPermissions: &recordingOpenAPIAccountPermissionChecker{},
			},
			userID:    43,
			wantError: true,
		},
		{
			name: "authorization failure",
			checker: &accountPermissionChecker{
				loadUser: func(context.Context, uint) (model.User, error) {
					return model.User{BaseModel: &model.BaseModel{ID: 43}, AccountType: model.AccountTypeConsole, Status: model.AccountStatusActive}, nil
				},
				authorizer:         &recordingAccountPermissionAuthorizer{err: providerError},
				openAPIPermissions: &recordingOpenAPIAccountPermissionChecker{},
			},
			userID:     43,
			wantError:  true,
			wantCalled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, err := tt.checker.HasPermission(t.Context(), tt.userID, model.PermissionMallRead, time.Now())
			if allowed || (err != nil) != tt.wantError {
				t.Fatalf("HasPermission() = %t, %v, want allowed false, error %t", allowed, err, tt.wantError)
			}
			if tt.checker != nil {
				if authorizer, ok := tt.checker.authorizer.(*recordingAccountPermissionAuthorizer); ok && authorizer.called != tt.wantCalled {
					t.Fatalf("authorizer called = %t, want %t", authorizer.called, tt.wantCalled)
				}
			}
		})
	}
}

func TestAccountPermissionCheckerDeniesInactiveAndUnknownAccounts(t *testing.T) {
	tests := []struct {
		name string
		user model.User
	}{
		{
			name: "inactive console account",
			user: model.User{BaseModel: &model.BaseModel{ID: 43}, AccountType: model.AccountTypeConsole, Status: model.AccountStatusDisabled},
		},
		{
			name: "unknown account type",
			user: model.User{BaseModel: &model.BaseModel{ID: 43}, AccountType: "UNKNOWN", Status: model.AccountStatusActive},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorizer := &recordingAccountPermissionAuthorizer{allowed: true}
			openAPIPermissions := &recordingOpenAPIAccountPermissionChecker{allowed: true}
			checker := &accountPermissionChecker{
				loadUser:           func(context.Context, uint) (model.User, error) { return tt.user, nil },
				authorizer:         authorizer,
				openAPIPermissions: openAPIPermissions,
			}
			allowed, err := checker.HasPermission(t.Context(), tt.user.ID, model.PermissionMallRead, time.Now())
			if err != nil || allowed {
				t.Fatalf("HasPermission() = %t, %v, want false, nil", allowed, err)
			}
			if authorizer.called || openAPIPermissions.called {
				t.Fatal("permission provider called for denied account")
			}
		})
	}
}

type recordingAccountPermissionAuthorizer struct {
	allowed    bool
	err        error
	called     bool
	user       model.User
	permission string
}

func (authorizer *recordingAccountPermissionAuthorizer) HasPermission(_ context.Context, user model.User, permission string) (bool, error) {
	authorizer.called = true
	authorizer.user = user
	authorizer.permission = permission
	return authorizer.allowed, authorizer.err
}

type recordingOpenAPIAccountPermissionChecker struct {
	allowed    bool
	err        error
	called     bool
	userID     uint
	permission string
	now        time.Time
}

func (checker *recordingOpenAPIAccountPermissionChecker) HasPermission(_ context.Context, userID uint, permission string, now time.Time) (bool, error) {
	checker.called = true
	checker.userID = userID
	checker.permission = permission
	checker.now = now
	return checker.allowed, checker.err
}
