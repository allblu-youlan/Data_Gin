package auth_svc

import (
	"context"
	"gin-biz-web-api/model"
	"testing"
)

func TestFeishuMentionPermissionsRequireExplicitGrant(t *testing.T) {
	for _, code := range []string{model.PermissionOfficeMentionRead, model.PermissionOfficeMentionManage, model.PermissionOfficeMentionRetry} {
		t.Run(code, func(t *testing.T) {
			if !knownPermissionCode(code) {
				t.Fatal("permission missing from catalog")
			}
			permission := model.Permission{Code: code, Module: "群消息提醒"}
			for _, role := range []string{model.RoleCodeOperator, model.RoleCodeViewer} {
				if defaultRoleIncludesPermission(role, permission) {
					t.Fatalf("new sensitive permission automatically granted to %s", role)
				}
			}
			for _, role := range []string{model.RoleCodeAdmin, model.RoleCodeSuperAdmin} {
				if !defaultRoleIncludesPermission(role, permission) {
					t.Fatalf("admin missing %s", code)
				}
			}
			repository := &fakeAuthorizationRepository{roles: []authorizationRole{{Code: model.RoleCodeOperator}}, consolePermissions: map[string]bool{model.PermissionOfficeMessageRead: true, model.PermissionOfficeMessageManage: true, model.PermissionOfficeMessagePush: true}}
			user := model.User{BaseModel: &model.BaseModel{ID: 7}, AccountType: model.AccountTypeConsole, Status: model.AccountStatusActive}
			allowed, err := newAuthorizer(repository).HasPermission(context.Background(), user, code)
			if err != nil || allowed {
				t.Fatalf("old office permission leaked: allowed=%v error=%v", allowed, err)
			}
		})
	}
	if candidates := permissionCandidates(model.PermissionOfficeMentionRead); len(candidates) != 1 || candidates[0] != model.PermissionOfficeMentionRead {
		t.Fatal("manage or old permissions must not imply mention read")
	}
}
