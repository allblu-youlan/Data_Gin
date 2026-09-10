package routers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"gin-biz-web-api/internal/controller/auth_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/phonecode"
	redisclient "gin-biz-web-api/pkg/redis"
	"gin-biz-web-api/pkg/sms"
	"go.uber.org/zap"
)

// RegisterAPIRoutes 注册 api 相关路由
func RegisterAPIRoutes(r *gin.Engine) {
	// 设置静态资源访问
	setStaticURL(r)

	var api *gin.RouterGroup
	api = r.Group("/api")
	registerHealthRoutes(r, api)

	// Health routes are registered before this guard so liveness and readiness
	// remain observable while database-backed business routes fail fast.
	api.Use(middleware.RequireDatabaseAvailability())

	// 全局限流中间件
	// 作为参考 Github API 每小时最多 60 个请求（根据 IP）
	api.Use(middleware.LimitIP("200000-H"))

	// 授权相关
	apiAuth(api)
	// 数据存储相关
	apiData(api)
	// 开放接口账号与数据授权，仅可信管理员可用
	registerDataAuthorizationRoutes(api)
	registerAccessRoutes(api)

}

func registerHealthRoutes(root, api gin.IRoutes) {
	for _, routes := range []gin.IRoutes{root, api} {
		routes.GET("/health", healthCheck)
		routes.HEAD("/health", healthCheck)
	}
	ready := databaseReadiness(pingApplicationDatabase)
	api.GET("/ready", ready)
	api.HEAD("/ready", ready)
}

func healthCheck(c *gin.Context) {
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	smsStatus := "ok"
	if _, err := sms.LoadConfig(); err != nil {
		smsStatus = "degraded"
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "gin-biz-web-api",
		"components": gin.H{
			"sms": smsStatus,
		},
	})
}

type databasePing func(context.Context) error

func pingApplicationDatabase(ctx context.Context) error {
	return database.RequireAvailable(ctx)
}

func databaseReadiness(ping databasePing) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()

		status := http.StatusOK
		readinessStatus := "ok"
		databaseStatus := "ok"
		if ping == nil || ping(ctx) != nil {
			status = http.StatusServiceUnavailable
			readinessStatus = "unavailable"
			databaseStatus = "unavailable"
		}
		if c.Request.Method == http.MethodHead {
			c.Status(status)
			return
		}
		c.JSON(status, gin.H{
			"status":  readinessStatus,
			"service": "gin-biz-web-api",
			"components": gin.H{
				"database": databaseStatus,
			},
		})
	}
}

// setStaticURL 设置静态资源访问
func setStaticURL(r *gin.Engine) {
	// 设置文件服务去提供静态资源的访问：https://gin-gonic.com/zh-cn/docs/examples/serving-static-files
	// eg：
	// 需要访问 `public/uploads/image/2022/03/19/c20ad4d76fe97759aa27a0c99bff6710-20220319023344.jpg` 文件时
	// 则访问地址为：http://localhost:3000/static/image/2022/03/19/c20ad4d76fe97759aa27a0c99bff6710-20220319023344.jpg
	r.StaticFS(config.GetString("cfg.upload.static_fs_relative_path"), http.Dir(config.GetString("cfg.upload.save_path")))
}

func apiAuth(api *gin.RouterGroup) {
	authGroup := api.Group("/auth")
	authGroup.Use(middleware.LimitIP("60000-H"))
	{
		loginCtrl := auth_ctrl.NewLoginController()
		authGroup.POST("/login", loginCtrl.ConsoleLogin) // 管理台登录

		var accountAuthService *auth_svc.AccountAuthService
		if smsClient, err := sms.NewFromEnvironment(); err == nil {
			codes := phonecode.New(
				phonecode.NewRedisStore(redisclient.Instance().Client, redisclient.GetNamespace()),
				smsClient,
			)
			accountAuthService = auth_svc.NewDatabaseAccountAuthService(codes)
			if logger.Logger != nil {
				logger.Info("短信服务已接入", zap.String("provider", "aliyun"))
			}
		} else {
			// SMS configuration is optional: password login remains available,
			// while phone-code endpoints fail closed with HTTP 503.
			accountAuthService = auth_svc.NewDatabaseAccountAuthService(nil)
			if logger.Logger != nil {
				logger.Warn("短信服务未接入", zap.Error(err))
			}
		}
		accountAuthCtrl := auth_ctrl.NewAccountAuthController(accountAuthService)
		authGroup.POST("/login/password", accountAuthCtrl.LoginPassword)
		authGroup.POST("/phone-codes", middleware.LimitRoute("10-H"), accountAuthCtrl.SendPhoneCode)
		authGroup.POST("/login/phone-code", accountAuthCtrl.LoginPhoneCode)
		authGroup.POST("/password/reset", accountAuthCtrl.ResetPassword)
		authGroup.POST("/password/change", middleware.AuthJWT(), accountAuthCtrl.ChangePassword)
		authGroup.GET("/me", middleware.AuthJWT(), accountAuthCtrl.Profile)

		tokenCtrl := new(auth_ctrl.TokenController)
		authGroup.POST("/token/refresh", middleware.AuthJWT(), tokenCtrl.RefreshToken) // 刷新令牌
		authGroup.GET("/token/info", middleware.AuthJWT(), tokenCtrl.GetTokenInfo)     // 根据token查询当前的信息

		// token数据管理
		tokenDataCtrl := auth_ctrl.NewTokenDataController()
		tokenDataGroup := authGroup.Group("/token-data")
		tokenDataGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionSystemAccountManage))
		tokenDataGroup.POST("", tokenDataCtrl.CreateTokenData)
		tokenDataGroup.GET("", tokenDataCtrl.GetAllTokenData)
		tokenDataGroup.GET("/:id", tokenDataCtrl.GetTokenDataByID)
		tokenDataGroup.POST("/update/:id", tokenDataCtrl.UpdateTokenData)
		tokenDataGroup.POST("/delete/:id", tokenDataCtrl.DeleteTokenData)
	}
}
