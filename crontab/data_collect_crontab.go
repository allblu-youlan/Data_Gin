package crontab

import (
	"context"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
)

type DataCollectCrontab struct{}

// GetSpec 获取定时任务执行表达式
func (d DataCollectCrontab) GetSpec() string {
	// 每30分钟执行一次
	return "0 */30 * * * *"
}

// Run 执行定时任务
func (d DataCollectCrontab) Run() {
	d.RunContext(context.Background())
}

func (d DataCollectCrontab) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	logger.Info("开始执行数据采集定时任务")

	// 1. 获取所有活跃的数据源
	dataSourceDAO := data_dao.NewDataSourceDAO()
	activeSources, err := dataSourceDAO.FindActive(ctx)
	if err != nil {
		logger.Error("获取活跃数据源失败", zap.Error(err))
		return
	}

	logger.Info("获取到活跃数据源", zap.Int("count", len(activeSources)))

	// 2. 遍历数据源执行采集
	collectService := data_svc.NewCollectService()
	for _, source := range activeSources {
		if ctx.Err() != nil {
			return
		}
		// 检查是否需要执行采集（根据schedule字段）
		if d.shouldCollect(source.Schedule) {
			logger.Info("开始采集数据源", zap.Uint("source_id", source.ID), zap.String("source_name", source.Name))

			// 执行数据采集
			result, err := collectService.CollectFromSource(ctx, source.ID)
			if err != nil {
				logger.Error("数据采集失败", zap.Uint("source_id", source.ID), zap.Error(err))
				if !database.CanServe(ctx) {
					return
				}
				continue
			}

			logger.Info("数据采集成功", zap.Uint("source_id", source.ID), zap.String("result", result))
		}
	}

	logger.Info("数据采集定时任务执行完成")
}

// shouldCollect 判断是否需要执行采集
func (d DataCollectCrontab) shouldCollect(schedule string) bool {
	// 这里简化处理，实际应该根据schedule字段判断
	// 例如：解析cron表达式，判断当前时间是否符合执行条件
	return true
}
