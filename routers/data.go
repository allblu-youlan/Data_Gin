package routers

import (
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

func apiData(api *gin.RouterGroup) {
	registerMallRoutes(api, data_ctrl.NewMallController())
	registerBusinessOverviewRoutes(api, data_ctrl.NewBusinessOverviewController())
	weatherCtrl := data_ctrl.NewMallWeatherController()
	registerMallWeatherRoutes(api, weatherCtrl)
	registerOpenWeatherRoutes(api, weatherCtrl, data_ctrl.NewOpenWeatherMallController())
	registerOpenBojunOrderRoutes(api, data_ctrl.NewOpenBojunOrderController())
	registerMallWeatherRefreshRoutes(api, data_ctrl.NewMallWeatherRefreshController())
	registerMallWeatherExportProfileRoutes(api, data_ctrl.NewMallWeatherExportProfileController())
	registerMallWeatherExportJobRoutes(api, data_ctrl.NewMallWeatherExportJobController())
	registerMallWeatherFeishuPushRoutes(api, data_ctrl.NewMallWeatherFeishuPushController())
	registerMallWeatherSheetPushOptionRoutes(api, data_ctrl.NewMallWeatherSheetPushOptionController())
	registerMallWeatherCapacityPlanRoutes(api, data_ctrl.NewMallWeatherCapacityPlanController())
	registerMallWeatherMetricsRoutes(api, data_ctrl.NewMallWeatherMetricsController())
	registerReportCenterRoutes(api, global.ReportCenterEnabledAtStartup)
	registerOfficeMessageRoutes(api, data_ctrl.NewOfficeMessageController())

	sourceGroup := api.Group("/v1/sources")
	sourceGroup.Use(middleware.AuthJWT())
	{
		sourceCtrl := data_ctrl.NewSourceController()
		sourceGroup.GET("", middleware.RequirePermission(model.PermissionSourceRead), sourceCtrl.ListSources)
		sourceGroup.GET("/:id", middleware.RequirePermission(model.PermissionSourceRead), sourceCtrl.GetSource)
		sourceGroup.POST("", middleware.RequirePermission(model.PermissionSourceManage), sourceCtrl.CreateSource)
		sourceGroup.PUT("/:id", middleware.RequirePermission(model.PermissionSourceManage), sourceCtrl.UpdateSource)
		sourceGroup.PATCH("/:id/enabled", middleware.RequirePermission(model.PermissionSourceManage), sourceCtrl.UpdateSourceEnabled)
		sourceGroup.POST("/:id/test", middleware.RequirePermission(model.PermissionSourceManage), sourceCtrl.TestSource)
		sourceGroup.POST("/:id/fetch", middleware.RequirePermission(model.PermissionSourceManage), sourceCtrl.FetchSource)
	}

	transformGroup := api.Group("/v1/transform-rules")
	transformGroup.Use(middleware.AuthJWT())
	{
		transformCtrl := data_ctrl.NewTransformController()
		transformGroup.GET("", middleware.RequirePermission(model.PermissionPipelineRead), transformCtrl.ListRules)
		transformGroup.GET("/:id", middleware.RequirePermission(model.PermissionPipelineRead), transformCtrl.GetRule)
		transformGroup.POST("", middleware.RequirePermission(model.PermissionPipelineManage), transformCtrl.CreateRule)
		transformGroup.PUT("/:id", middleware.RequirePermission(model.PermissionPipelineManage), transformCtrl.UpdateRule)
		transformGroup.PATCH("/:id/enabled", middleware.RequirePermission(model.PermissionPipelineManage), transformCtrl.UpdateRuleEnabled)
		transformGroup.POST("/test", middleware.RequirePermission(model.PermissionPipelineManage), transformCtrl.TestRule)
	}

	rawRecordsGroup := api.Group("/v1/raw-records")
	rawRecordsGroup.Use(middleware.AuthJWT())
	{
		transformCtrl := data_ctrl.NewTransformController()
		rawRecordCtrl := data_ctrl.NewRawRecordController()
		rawRecordsGroup.GET("", middleware.RequirePermission(model.PermissionDataRead), rawRecordCtrl.List)
		rawRecordsGroup.POST("/:id/retransform", middleware.RequirePermission(model.PermissionDataManage), transformCtrl.RetransformRawRecord)
	}

	destinationGroup := api.Group("/v1/destinations")
	destinationGroup.Use(middleware.AuthJWT())
	{
		deliveryCtrl := data_ctrl.NewDeliveryController()
		destinationGroup.GET("", middleware.RequirePermission(model.PermissionDeliveryRead), deliveryCtrl.ListDestinations)
		destinationGroup.GET("/:id", middleware.RequirePermission(model.PermissionDeliveryRead), deliveryCtrl.GetDestination)
		destinationGroup.POST("", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.CreateDestination)
		destinationGroup.PUT("/:id", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.UpdateDestination)
		destinationGroup.PATCH("/:id/enabled", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.UpdateDestinationEnabled)
		destinationGroup.POST("/:id/test", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.TestDestination)
	}

	deliveryTaskGroup := api.Group("/v1/delivery-tasks")
	deliveryTaskGroup.Use(middleware.AuthJWT())
	{
		deliveryCtrl := data_ctrl.NewDeliveryController()
		deliveryTaskGroup.GET("", middleware.RequirePermission(model.PermissionDeliveryRead), deliveryCtrl.ListTasks)
		deliveryTaskGroup.GET("/:id", middleware.RequirePermission(model.PermissionDeliveryRead), deliveryCtrl.GetTask)
		deliveryTaskGroup.POST("", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.CreateTask)
		deliveryTaskGroup.PUT("/:id", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.UpdateTask)
		deliveryTaskGroup.PATCH("/:id/enabled", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.UpdateTaskEnabled)
		deliveryTaskGroup.POST("/:id/run", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.RunTask)
	}

	deliveryLogGroup := api.Group("/v1/delivery-logs")
	deliveryLogGroup.Use(middleware.AuthJWT())
	{
		deliveryCtrl := data_ctrl.NewDeliveryController()
		deliveryLogGroup.GET("", middleware.RequirePermission(model.PermissionDeliveryRead), deliveryCtrl.ListLogs)
		deliveryLogGroup.POST("/:id/retry", middleware.RequirePermission(model.PermissionDeliveryManage), deliveryCtrl.RetryLog)
	}

	orderPushConfigGroup := api.Group("/v1/order-push-skip-config")
	orderPushConfigGroup.Use(middleware.AuthJWT())
	{
		orderPushConfigCtrl := data_ctrl.NewOrderPushConfigController()
		orderPushConfigGroup.GET("", middleware.RequirePermission(model.PermissionDeliveryRead), orderPushConfigCtrl.GetSkipPolicy)
		orderPushConfigGroup.PUT("", middleware.RequirePermission(model.PermissionDeliveryManage), orderPushConfigCtrl.SaveSkipPolicy)
	}

	runGroup := api.Group("/v1/runs")
	runGroup.Use(middleware.AuthJWT())
	{
		runCtrl := data_ctrl.NewRunController()
		runGroup.GET("", middleware.RequirePermission(model.PermissionPipelineRead), runCtrl.ListRuns)
	}

	pipelineGroup := api.Group("/v1/pipelines")
	pipelineGroup.Use(middleware.AuthJWT())
	{
		pipelineCtrl := data_ctrl.NewPipelineController()
		pipelineGroup.GET("", middleware.RequirePermission(model.PermissionPipelineRead), pipelineCtrl.ListPipelines)
		pipelineGroup.GET("/:id", middleware.RequirePermission(model.PermissionPipelineRead), pipelineCtrl.GetPipeline)
		pipelineGroup.POST("", middleware.RequirePermission(model.PermissionPipelineManage), pipelineCtrl.CreatePipeline)
		pipelineGroup.PUT("/:id", middleware.RequirePermission(model.PermissionPipelineManage), pipelineCtrl.UpdatePipeline)
		pipelineGroup.GET("/:id/stages", middleware.RequirePermission(model.PermissionPipelineRead), pipelineCtrl.ListStages)
		pipelineGroup.POST("/:id/stages", middleware.RequirePermission(model.PermissionPipelineManage), pipelineCtrl.CreateStage)
		pipelineGroup.GET("/:id/steps", middleware.RequirePermission(model.PermissionPipelineRead), pipelineCtrl.ListSteps)
		pipelineGroup.POST("/:id/steps", middleware.RequirePermission(model.PermissionPipelineManage), pipelineCtrl.CreateStep)
		pipelineGroup.PUT("/:id/steps/:step_id", middleware.RequirePermission(model.PermissionPipelineManage), pipelineCtrl.UpdateStep)
		pipelineGroup.GET("/:id/preview-json", middleware.RequirePermission(model.PermissionPipelineRead), pipelineCtrl.PreviewJSON)
		pipelineGroup.POST("/:id/run", middleware.RequirePermission(model.PermissionPipelineExecute), pipelineCtrl.RunPipeline)
	}

	pipelineStageGroup := api.Group("/v1/pipeline-stages")
	pipelineStageGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionPipelineManage))
	{
		pipelineCtrl := data_ctrl.NewPipelineController()
		pipelineStageGroup.PUT("/:stage_id", pipelineCtrl.UpdateStage)
		pipelineStageGroup.POST("/:stage_id/steps", pipelineCtrl.CreateStageStep)
		pipelineStageGroup.PUT("/:stage_id/steps/:step_id", pipelineCtrl.UpdateStep)
		pipelineStageGroup.POST("/:stage_id/generate-config", pipelineCtrl.GenerateStageConfig)
		pipelineStageGroup.POST("/:stage_id/publish-config", pipelineCtrl.PublishStageConfig)
	}

	stepRunGroup := api.Group("/v1/pipeline-runs")
	stepRunGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionPipelineRead))
	{
		pipelineCtrl := data_ctrl.NewPipelineController()
		stepRunGroup.GET("/:id/steps", pipelineCtrl.ListStepRuns)
	}

	excelMatchJobGroup := api.Group("/v1/excel-match-jobs")
	excelMatchJobGroup.Use(middleware.AuthJWT())
	{
		excelMatchJobCtrl := data_ctrl.NewExcelMatchJobController()
		excelMatchJobGroup.GET("", middleware.RequirePermission(model.PermissionExcelRead), excelMatchJobCtrl.ListJobs)
		excelMatchJobGroup.POST("", middleware.RequirePermission(model.PermissionExcelExecute), excelMatchJobCtrl.CreateJob)
		excelMatchJobGroup.GET("/models", middleware.RequirePermission(model.PermissionExcelRead), excelMatchJobCtrl.ListModels)
		excelMatchJobGroup.POST("/preview", middleware.RequirePermission(model.PermissionExcelExecute), excelMatchJobCtrl.Preview)
		excelMatchJobGroup.POST("/uploads", middleware.RequirePermission(model.PermissionExcelExecute), excelMatchJobCtrl.CreateUploadSession)
		excelMatchJobGroup.POST("/uploads/:upload_id/chunks", middleware.RequirePermission(model.PermissionExcelExecute), excelMatchJobCtrl.UploadChunk)
		excelMatchJobGroup.POST("/uploads/:upload_id/complete", middleware.RequirePermission(model.PermissionExcelExecute), excelMatchJobCtrl.CompleteUpload)
		excelMatchJobGroup.GET("/schemes", middleware.RequirePermission(model.PermissionExcelRead), excelMatchJobCtrl.ListSchemes)
		excelMatchJobGroup.POST("/schemes", middleware.RequirePermission(model.PermissionExcelManage), excelMatchJobCtrl.SaveScheme)
		excelMatchJobGroup.DELETE("/schemes/:scheme_id", middleware.RequirePermission(model.PermissionExcelManage), excelMatchJobCtrl.DeleteScheme)
		excelMatchJobGroup.GET("/:id", middleware.RequirePermission(model.PermissionExcelRead), excelMatchJobCtrl.GetJob)
		excelMatchJobGroup.GET("/:id/download", middleware.RequirePermission(model.PermissionExcelRead), excelMatchJobCtrl.Download)
		excelMatchJobGroup.POST("/:id/download", middleware.RequirePermission(model.PermissionExcelRead), excelMatchJobCtrl.Download)
	}

	legacyTaskGroup := api.Group("/v1/legacy-tasks")
	legacyTaskGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionPipelineExecute))
	{
		legacyTaskCtrl := data_ctrl.NewLegacyTaskController()
		legacyTaskGroup.GET("", legacyTaskCtrl.List)
		legacyTaskGroup.POST("/:code/run", legacyTaskCtrl.Run)
	}

	bojunOrderBackfillGroup := api.Group("/v1/bojun-order-backfill")
	bojunOrderBackfillGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionDataManage))
	{
		bojunOrderBackfillCtrl := data_ctrl.NewBojunOrderBackfillController()
		bojunOrderBackfillGroup.POST("/preview", bojunOrderBackfillCtrl.Preview)
		bojunOrderBackfillGroup.POST("/confirm", bojunOrderBackfillCtrl.Confirm)
	}

	youzanDistributionOrderBackfillGroup := api.Group("/v1/youzan-distribution-order-backfill")
	youzanDistributionOrderBackfillGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionDataManage))
	{
		youzanDistributionOrderBackfillCtrl := data_ctrl.NewYouzanDistributionOrderBackfillController()
		youzanDistributionOrderBackfillGroup.POST("/preview", youzanDistributionOrderBackfillCtrl.Preview)
		youzanDistributionOrderBackfillGroup.POST("/confirm", youzanDistributionOrderBackfillCtrl.Confirm)
	}

	legacyTransformRuleGroup := api.Group("/v1/legacy-transform-rules")
	legacyTransformRuleGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionPipelineRead))
	{
		legacyTaskCtrl := data_ctrl.NewLegacyTaskController()
		legacyTransformRuleGroup.GET("", legacyTaskCtrl.ListTransformRules)
	}

	dataGroup := api.Group("/v1/data")
	dataGroup.Use(middleware.AuthJWT()) // 需要认证
	{
		dataCtrl := data_ctrl.NewDataController()

		// 数据采集
		dataGroup.POST("/collect/:source_id", middleware.RequirePermission(model.PermissionDataManage), dataCtrl.CollectController.ManualCollect)
		dataGroup.GET("/collect/status/:job_id", middleware.RequirePermission(model.PermissionDataRead), dataCtrl.CollectController.CollectStatus)
		// 任务创建
		dataGroup.POST("/task/create/:source_id", middleware.RequirePermission(model.PermissionDataManage), dataCtrl.CollectController.CreateTask)

		// 数据接收
		dataGroup.POST("/ingest", middleware.RequirePermission(model.PermissionDataManage), dataCtrl.IngestController.IngestData)
		dataGroup.POST("/ingest/batch", middleware.RequirePermission(model.PermissionDataManage), dataCtrl.IngestController.IngestBatchData)
		dataGroup.POST("/ingest/raw", middleware.RequirePermission(model.PermissionDataManage), dataCtrl.IngestController.RawIngestData) // 接收原始格式数据

		// 数据查询
		dataGroup.GET("/raw", middleware.RequirePermission(model.PermissionDataRead), dataCtrl.QueryController.GetRawData)
		dataGroup.POST("/raw/list", middleware.RequirePermission(model.PermissionDataRead), dataCtrl.QueryController.GetRawDataList)
		dataGroup.GET("/processed", middleware.RequirePermission(model.PermissionDataRead), dataCtrl.QueryController.GetProcessedData)
		dataGroup.GET("/processed/list", middleware.RequirePermission(model.PermissionDataRead), dataCtrl.QueryController.GetProcessedDataList)
		dataGroup.GET("/clean-records/list", middleware.RequirePermission(model.PermissionDataRead), dataCtrl.QueryController.GetCleanRecordList)
		dataGroup.GET("/statistics", middleware.RequirePermission(model.PermissionDataRead), dataCtrl.QueryController.GetStatistics)
	}
}

func registerBusinessOverviewRoutes(api *gin.RouterGroup, controller *data_ctrl.BusinessOverviewController) {
	api.GET(
		"/v1/business-overview/malls",
		middleware.AuthJWT(),
		middleware.RequirePermission(model.PermissionBusinessOverviewRead),
		middleware.LimitRoute("120-M"),
		controller.ListMalls,
	)
	api.GET(
		"/v1/business-overview/payments",
		middleware.AuthJWT(),
		middleware.RequirePermission(model.PermissionBusinessOverviewRead),
		middleware.LimitRoute("120-M"),
		controller.QueryPayments,
	)
}

func registerOfficeMessageRoutes(api *gin.RouterGroup, controller *data_ctrl.OfficeMessageController) {
	messages := api.Group("/v1/office-messages")
	messages.Use(middleware.AuthJWT())
	messages.GET("", middleware.RequirePermission(model.PermissionOfficeMessageRead), controller.ListMessages)
	messages.POST("", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.CreateMessage)
	messages.PUT("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.UpdateMessage)
	messages.DELETE("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.DeleteMessage)
	api.GET("/v1/office-feishu-bots", middleware.AuthJWT(), middleware.RequirePermission(model.PermissionOfficeMessageRead), controller.ListFeishuBots)

	targets := api.Group("/v1/office-push-targets")
	targets.Use(middleware.AuthJWT())
	targets.GET("", middleware.RequirePermission(model.PermissionOfficeMessageRead), controller.ListTargets)
	targets.POST("", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.CreateTarget)
	targets.PUT("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.UpdateTarget)
	targets.DELETE("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.DeleteTarget)
	targets.POST("/:id/runs", middleware.RequirePermission(model.PermissionOfficeMessagePush), middleware.LimitRoute("30-M"), controller.CreateRun)

	schedules := api.Group("/v1/office-push-schedules")
	schedules.Use(middleware.AuthJWT())
	schedules.GET("", middleware.RequirePermission(model.PermissionOfficeMessageRead), controller.ListSchedules)
	schedules.POST("", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.CreateSchedule)
	schedules.PUT("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.UpdateSchedule)
	schedules.DELETE("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.DeleteSchedule)

	api.GET("/v1/office-push-runs", middleware.AuthJWT(), middleware.RequirePermission(model.PermissionOfficeMessageRead), controller.ListRuns)
	oracle := api.Group("/v1/office-oracle")
	oracle.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionOfficeMessageManage), middleware.LimitRoute("120-M"))
	oracle.GET("/procedures", controller.ListProcedures)
	oracle.GET("/procedure-signature", controller.ProcedureSignature)
	oracle.GET("/result-tables", controller.ListResultTables)
	oracle.GET("/result-table-schema", controller.ResultTableSchema)
	oracle.POST("/select-tests", controller.TestSelect)
}

func registerReportCenterRoutes(api *gin.RouterGroup, enabled bool) {
	if !enabled {
		return
	}
	registerReportRoutes(api, data_ctrl.NewReportController())
	registerReportInputQueryRoutes(api, data_ctrl.NewReportInputQueryController())
	registerReportDatasourceRoutes(api, data_ctrl.NewReportDatasourceController())
	registerReportRunRoutes(api, data_ctrl.NewReportRunController())
	registerReportExportRoutes(api, data_ctrl.NewReportExportController())
	registerReportAuditRoutes(api, data_ctrl.NewReportAuditController())
	registerReportCategoryAccessRoutes(api, data_ctrl.NewReportCategoryAccessController())
	registerReportDownloadCatalogRoutes(api, data_ctrl.NewReportDownloadCatalogController())
}

func registerReportCategoryAccessRoutes(api *gin.RouterGroup, controller *data_ctrl.ReportCategoryAccessController) {
	group := api.Group("/v1/report-category-access")
	group.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportManage))
	group.GET("", controller.List)
	group.PUT("", controller.Replace)
}

func registerReportDownloadCatalogRoutes(api *gin.RouterGroup, controller *data_ctrl.ReportDownloadCatalogController) {
	api.GET(
		"/v1/report-downloads",
		middleware.AuthJWT(),
		middleware.RequirePermission(model.PermissionReportRead),
		middleware.RequirePermission(model.PermissionReportExecute),
		middleware.RequirePermission(model.PermissionReportExport),
		controller.List,
	)
}

func registerReportInputQueryRoutes(api *gin.RouterGroup, controller *data_ctrl.ReportInputQueryController) {
	api.GET("/v1/report-input-queries", middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportManage), controller.List)
	api.POST("/v1/report-input-query-definition-tests", middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportManage), middleware.LimitRoute("30-M"), controller.TestDefinitionDraft)
	group := api.Group("/v1/report-input-query-definitions")
	group.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportManage))
	group.GET("", controller.ListDefinitions)
	group.POST("", controller.CreateDefinition)
	group.GET("/:id", controller.GetDefinition)
	group.PUT("/:id", controller.UpdateDefinition)
	group.DELETE("/:id", controller.DeleteDefinition)
	group.POST("/:id/test", middleware.LimitRoute("30-M"), controller.TestDefinition)
	api.GET("/v1/reports/:id/input-options/:condition_code", middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportExecute), middleware.LimitRoute("120-M"), controller.Options)
}

func registerReportDatasourceRoutes(api *gin.RouterGroup, controller *data_ctrl.ReportDatasourceController) {
	api.POST("/v1/report-datasource-connection-tests", middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportManage), middleware.LimitRoute("30-M"), controller.TestConnection)
	group := api.Group("/v1/report-datasources")
	group.Use(middleware.AuthJWT())
	group.GET("", middleware.RequirePermission(model.PermissionReportManage), controller.List)
	group.GET("/:id", middleware.RequirePermission(model.PermissionReportManage), controller.Get)
	group.POST("", middleware.RequirePermission(model.PermissionReportManage), controller.Create)
	group.PUT("/:id", middleware.RequirePermission(model.PermissionReportManage), controller.Update)
	group.POST("/:id/test", middleware.RequirePermission(model.PermissionReportManage), controller.Test)
	group.GET("/:id/procedures", middleware.RequirePermission(model.PermissionReportManage), middleware.LimitRoute("120-M"), controller.ListProcedures)
	group.GET("/:id/procedure-signature", middleware.RequirePermission(model.PermissionReportManage), middleware.LimitRoute("120-M"), controller.GetProcedureSignature)
	group.GET("/:id/result-tables", middleware.RequirePermission(model.PermissionReportManage), middleware.LimitRoute("120-M"), controller.ListResultTables)
	group.GET("/:id/result-table-schema", middleware.RequirePermission(model.PermissionReportManage), middleware.LimitRoute("120-M"), controller.GetResultTableSchema)
}

const (
	openBojunPreAuthIPRateLimit = "300-M"
	openBojunUserRouteRateLimit = "30-M"
)

func registerOpenBojunOrderRoutes(
	api *gin.RouterGroup,
	controller *data_ctrl.OpenBojunOrderController,
) {
	bojunGroup := api.Group("/open/bojun")
	bojunGroup.Use(
		middleware.LimitOpenAPIIP("bojun", openBojunPreAuthIPRateLimit),
		middleware.AuthOpenToken(),
		middleware.RequirePermission(model.PermissionBojunOrderRead),
		middleware.LimitOpenAPIUserRoute("bojun", openBojunUserRouteRateLimit),
	)
	bojunGroup.POST("/orders/query", controller.Query)
}

const (
	openWeatherPreAuthIPRateLimit = "600-M"
	openWeatherUserRouteRateLimit = "120-M"
)

func registerOpenWeatherRoutes(
	api *gin.RouterGroup,
	weatherCtrl *data_ctrl.MallWeatherController,
	mallCtrl *data_ctrl.OpenWeatherMallController,
) {
	weatherGroup := api.Group("/open/weather")
	weatherGroup.Use(
		middleware.LimitOpenAPIIP("weather", openWeatherPreAuthIPRateLimit),
		middleware.AuthOpenToken(),
		middleware.RequirePermission(model.PermissionWeatherRead),
		middleware.LimitOpenAPIUserRoute("weather", openWeatherUserRouteRateLimit),
	)
	{
		weatherGroup.POST("/malls/query", mallCtrl.Query)
		weatherGroup.POST("/overview", weatherCtrl.OpenOverview)
		weatherGroup.POST("/realtime", weatherCtrl.OpenRealtime)
		weatherGroup.POST("/history/day", weatherCtrl.OpenHistoryDay)
		weatherGroup.POST("/history/day/summary", weatherCtrl.OpenHistoryDaySummary)
		weatherGroup.POST("/history/range", weatherCtrl.OpenHistoryRange)
		weatherGroup.POST("/minutely", weatherCtrl.OpenMinutely)
		weatherGroup.POST("/hourly", weatherCtrl.OpenHourly)
		weatherGroup.POST("/daily", weatherCtrl.OpenDaily)
		weatherGroup.POST("/alerts", weatherCtrl.OpenAlerts)
		weatherGroup.POST("/life-indices", weatherCtrl.OpenLifeIndices)
		// Legacy path aliases remain available while clients migrate mallId into JSON bodies.
		weatherGroup.POST("/malls/:id/overview", weatherCtrl.OpenOverview)
		weatherGroup.POST("/malls/:id/realtime", weatherCtrl.OpenRealtime)
		weatherGroup.POST("/malls/:id/minutely", weatherCtrl.OpenMinutely)
		weatherGroup.POST("/malls/:id/hourly", weatherCtrl.OpenHourly)
		weatherGroup.POST("/malls/:id/daily", weatherCtrl.OpenDaily)
		weatherGroup.POST("/malls/:id/alerts", weatherCtrl.OpenAlerts)
		weatherGroup.POST("/malls/:id/life-indices", weatherCtrl.OpenLifeIndices)
	}
}

func registerMallWeatherRoutes(api *gin.RouterGroup, weatherCtrl *data_ctrl.MallWeatherController) {
	weatherGroup := api.Group("/v1/malls")
	weatherGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionWeatherRead))
	{
		scope := middleware.RequireMallScope("id")
		weatherGroup.GET("/:id/weather/overview", scope, weatherCtrl.Overview)
		weatherGroup.GET("/:id/weather/realtime", scope, weatherCtrl.Realtime)
		weatherGroup.GET("/:id/weather/minutely", scope, weatherCtrl.Minutely)
		weatherGroup.GET("/:id/weather/hourly", scope, weatherCtrl.Hourly)
		weatherGroup.GET("/:id/weather/daily", scope, weatherCtrl.Daily)
		weatherGroup.GET("/:id/weather/alerts", scope, weatherCtrl.Alerts)
		weatherGroup.GET("/:id/weather/life-indices", scope, weatherCtrl.LifeIndices)
		weatherGroup.GET("/:id/weather/fetch-runs", scope, weatherCtrl.FetchRuns)
	}
}

func registerMallWeatherRefreshRoutes(api *gin.RouterGroup, refreshCtrl *data_ctrl.MallWeatherRefreshController) {
	weatherGroup := api.Group("/v1/malls")
	weatherGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionWeatherRefresh))
	weatherGroup.POST("/:id/weather-refresh", middleware.RequireMallScope("id"), middleware.RequireMallWeatherEnabled(), refreshCtrl.Refresh)
}

func registerReportRoutes(api *gin.RouterGroup, reportCtrl *data_ctrl.ReportController) {
	reportGroup := api.Group("/v1/reports")
	reportGroup.Use(middleware.AuthJWT())
	reportGroup.POST("", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.Create)
	reportGroup.GET("", middleware.RequirePermission(model.PermissionReportRead), reportCtrl.List)
	reportGroup.GET("/:id", middleware.RequirePermission(model.PermissionReportRead), reportCtrl.Get)
	reportGroup.PUT("/:id", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.Update)
	reportGroup.DELETE("/:id", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.Delete)
	reportGroup.POST("/:id/publish", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.Publish)
	reportGroup.PUT("/:id/active-version", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.ActivateVersion)
	reportGroup.GET("/:id/versions", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.ListVersions)
	reportGroup.GET("/:id/versions/:versionId", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.GetVersion)
	reportGroup.GET("/:id/version-diff", middleware.RequirePermission(model.PermissionReportManage), reportCtrl.VersionDiff)
	reportGroup.GET("/:id/run-contract", middleware.RequirePermission(model.PermissionReportExecute), reportCtrl.GetRunContract)
	reportGroup.POST("/:id/runs", middleware.RequirePermission(model.PermissionReportExecute), reportCtrl.CreateRun)
}

func registerReportRunRoutes(api *gin.RouterGroup, runCtrl *data_ctrl.ReportRunController) {
	runGroup := api.Group("/v1/report-runs")
	runGroup.Use(middleware.AuthJWT())
	runGroup.GET("/:id", middleware.RequirePermission(model.PermissionReportRead), runCtrl.Get)
	runGroup.GET("/:id/results", middleware.RequirePermission(model.PermissionReportRead), runCtrl.Results)
	runGroup.POST("/:id/results/query", middleware.RequirePermission(model.PermissionReportRead), runCtrl.QueryResults)
	runGroup.POST("/:id/cancel", middleware.RequirePermission(model.PermissionReportExecute), runCtrl.Cancel)
}

func registerReportExportRoutes(api *gin.RouterGroup, exportCtrl *data_ctrl.ReportExportController) {
	runGroup := api.Group("/v1/report-runs")
	runGroup.Use(middleware.AuthJWT())
	runGroup.POST("/:id/export", middleware.RequirePermission(model.PermissionReportExport), exportCtrl.Create)

	exportGroup := api.Group("/v1/report-exports")
	exportGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportRead))
	exportGroup.GET("", middleware.RequirePermission(model.PermissionReportExport), exportCtrl.List)
	exportGroup.GET("/:id", exportCtrl.Get)
	exportGroup.GET("/:id/download", middleware.RequirePermission(model.PermissionReportExport), exportCtrl.Download)
}

func registerReportAuditRoutes(api *gin.RouterGroup, auditCtrl *data_ctrl.ReportAuditController) {
	auditGroup := api.Group("/v1/report-audits")
	auditGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportManage))
	auditGroup.GET("", auditCtrl.List)
}

func registerMallWeatherExportProfileRoutes(
	api *gin.RouterGroup,
	profileCtrl *data_ctrl.MallWeatherExportProfileController,
) {
	profileGroup := api.Group("/v1/weather-export-profiles")
	profileGroup.Use(middleware.AuthJWT())
	profileGroup.POST("", middleware.RequirePermission(model.PermissionWeatherConfigManage), profileCtrl.Save)
	profileGroup.GET("", middleware.RequirePermission(model.PermissionWeatherExport), profileCtrl.List)
}

func registerMallWeatherExportJobRoutes(
	api *gin.RouterGroup,
	exportCtrl *data_ctrl.MallWeatherExportJobController,
) {
	exportGroup := api.Group("/v1/weather-exports")
	exportGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionWeatherExport))
	exportGroup.POST("", middleware.RequireMallWeatherEnabled(), exportCtrl.Create)
	exportGroup.GET("/:job_id", exportCtrl.Get)
	exportGroup.GET("/:job_id/download", exportCtrl.Download)
	exportGroup.GET("/:job_id/content", exportCtrl.DownloadContent)
	exportGroup.POST("/:job_id/content", exportCtrl.DownloadContent)
	exportGroup.GET("/:job_id/content/status", exportCtrl.DownloadContentStatus)
}

func registerMallWeatherFeishuPushRoutes(
	api *gin.RouterGroup,
	pushCtrl *data_ctrl.MallWeatherFeishuPushController,
) {
	pushGroup := api.Group("/v1/weather-sheet-pushes")
	pushGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionWeatherFeishuPush))
	pushGroup.POST("", middleware.RequireMallWeatherEnabled(), pushCtrl.Create)
	pushGroup.GET("/:run_id", pushCtrl.Get)
	pushGroup.POST("/dry-run", pushCtrl.DryRun)
}

func registerMallWeatherSheetPushOptionRoutes(
	api *gin.RouterGroup,
	optionCtrl *data_ctrl.MallWeatherSheetPushOptionController,
) {
	optionGroup := api.Group("/v1/weather-sheet-push-options")
	optionGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionWeatherConfigManage))
	optionGroup.GET("", optionCtrl.List)
}

func registerMallWeatherMetricsRoutes(
	api *gin.RouterGroup,
	metricsCtrl *data_ctrl.MallWeatherMetricsController,
) {
	metricsGroup := api.Group("/v1/mall-weather")
	metricsGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionWeatherRead))
	metricsGroup.GET("/metrics", metricsCtrl.Snapshot)
}

func registerMallWeatherCapacityPlanRoutes(
	api *gin.RouterGroup,
	capacityCtrl *data_ctrl.MallWeatherCapacityPlanController,
) {
	capacityGroup := api.Group("/v1/mall-weather")
	capacityGroup.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionWeatherConfigManage))
	capacityGroup.GET("/capacity-plan", capacityCtrl.Show)
}

func registerMallRoutes(api *gin.RouterGroup, mallCtrl *data_ctrl.MallController) {
	mallGroup := api.Group("/v1/malls")
	mallGroup.Use(middleware.AuthJWT())
	{
		weatherWriteEnabled := middleware.RequireMallWeatherEnabled()
		mallGroup.POST("", middleware.RequirePermission(model.PermissionMallWrite), weatherWriteEnabled, mallCtrl.Create)
		mallGroup.POST("/import", middleware.RequirePermission(model.PermissionMallWrite), weatherWriteEnabled, mallCtrl.Import)
		mallGroup.GET("", middleware.RequirePermission(model.PermissionMallRead), mallCtrl.List)
		mallGroup.GET("/:id", middleware.RequirePermission(model.PermissionMallRead), middleware.RequireMallScope("id"), mallCtrl.Get)
		mallGroup.PATCH("/:id", middleware.RequirePermission(model.PermissionMallWrite), middleware.RequireMallScope("id"), mallCtrl.Update)
		mallGroup.DELETE("/:id", middleware.RequirePermission(model.PermissionMallWrite), middleware.RequireMallScope("id"), mallCtrl.Delete)
		mallGroup.POST("/:id/geocode", middleware.RequirePermission(model.PermissionMallWrite), middleware.RequireMallScope("id"), weatherWriteEnabled, mallCtrl.TriggerGeocode)
		mallGroup.GET("/:id/geocode-candidates", middleware.RequirePermission(model.PermissionMallRead), middleware.RequireMallScope("id"), mallCtrl.ListGeocodeCandidates)
		mallGroup.POST("/:id/geocode-confirm", middleware.RequirePermission(model.PermissionMallGeocodeConfirm), middleware.RequireMallScope("id"), mallCtrl.ConfirmGeocode)
	}
}
