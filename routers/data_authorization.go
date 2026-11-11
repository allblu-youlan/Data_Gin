package routers

import (
	"gin-biz-web-api/internal/controller/auth_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

func registerDataAuthorizationRoutes(api *gin.RouterGroup) {
	controller := auth_ctrl.NewDataAuthorizationController()
	group := api.Group("/v1/data-authorizations")
	group.Use(
		middleware.LimitOpenAPIIP("data-authorization-admin", "300-M"),
		middleware.AuthInternalBearerJWT(),
		middleware.RequirePermission(model.PermissionSystemAccountManage),
		middleware.LimitOpenAPIUserRoute("data-authorization-admin", "60-M"),
	)
	group.GET("/permissions", controller.PermissionCatalog)
	group.POST("/accounts/query", controller.QueryAccounts)
	group.POST("/accounts/create", controller.CreateAccount)
	group.POST("/accounts/:id/permissions/grant", controller.Grant)
	group.POST("/accounts/:id/permissions/revoke", controller.Revoke)
	group.POST("/accounts/:id/token/reissue", controller.ReissueToken)
	group.POST("/audits/query", controller.QueryAudits)
}
