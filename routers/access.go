package routers

import (
	"net/http"

	"gin-biz-web-api/internal/controller/auth_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

func registerAccessRoutes(api *gin.RouterGroup) {
	group := api.Group("/v1/access")
	group.Use(middleware.AuthInternalBearerJWT())

	accounts := auth_ctrl.NewAccessAccountController()
	malls := auth_ctrl.NewAccessMallCatalogController()
	group.GET("/malls", middleware.RequirePermission(model.PermissionSystemAccountManage), malls.List)
	group.POST("/accounts/query", middleware.RequirePermission(model.PermissionSystemAccountRead), accounts.Query)
	group.POST("/accounts", middleware.RequirePermission(model.PermissionSystemAccountManage), accounts.Create)
	registerPOSTWriteRoute(group, http.MethodPut, "/accounts/:id", middleware.RequirePermission(model.PermissionSystemAccountManage), accounts.Update)
	registerPOSTWriteRoute(group, http.MethodPut, "/accounts/:id/status", middleware.RequirePermission(model.PermissionSystemAccountManage), accounts.SetStatus)
	registerPOSTWriteRoute(group, http.MethodPut, "/accounts/:id/password", middleware.RequirePermission(model.PermissionSystemAccountManage), accounts.ResetPassword)
	registerPOSTWriteRoute(group, http.MethodPut, "/accounts/:id/roles", middleware.RequirePermission(model.PermissionSystemAccountManage), accounts.ReplaceRoles)
	registerPOSTWriteRoute(group, http.MethodPut, "/accounts/:id/mall-scope", middleware.RequirePermission(model.PermissionSystemAccountManage), accounts.ReplaceMallScope)
	group.GET("/accounts/:id/report-categories", middleware.RequirePermission(model.PermissionSystemAccountManage), middleware.RequirePermission(model.PermissionReportManage), accounts.ListReportCategories)
	registerPOSTWriteRoute(group, http.MethodPut, "/accounts/:id/report-categories", middleware.RequirePermission(model.PermissionSystemAccountManage), middleware.RequirePermission(model.PermissionReportManage), accounts.ReplaceReportCategory)

	roles := auth_ctrl.NewAccessRoleController()
	group.GET("/permissions", middleware.RequirePermission(model.PermissionSystemRoleRead), roles.PermissionCatalog)
	group.GET("/roles", middleware.RequirePermission(model.PermissionSystemRoleRead), roles.ListRoles)
	group.POST("/roles", middleware.RequirePermission(model.PermissionSystemRoleManage), roles.CreateRole)
	registerPOSTWriteRoute(group, http.MethodPut, "/roles/:id", middleware.RequirePermission(model.PermissionSystemRoleManage), roles.UpdateRole)
	registerPOSTWriteRoute(group, http.MethodPut, "/roles/:id/status", middleware.RequirePermission(model.PermissionSystemRoleManage), roles.SetRoleStatus)
	registerPOSTWriteRoute(group, http.MethodPut, "/roles/:id/permissions", middleware.RequirePermission(model.PermissionSystemRoleManage), roles.ReplacePermissions)
	registerPOSTWriteRoute(group, http.MethodDelete, "/roles/:id", middleware.RequirePermission(model.PermissionSystemRoleManage), roles.DeleteRole)
	group.GET("/audits", middleware.RequirePermission(model.PermissionSystemAuditRead), roles.QueryAudits)
}
