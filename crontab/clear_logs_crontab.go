package crontab

import (
	"context"

	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/logger"
)

type ClearLogsCrontab struct {
}

// Run 按日期轮转日志文件
func (c ClearLogsCrontab) Run() {
	c.RunContext(context.Background())
}

func (c ClearLogsCrontab) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if "daily" != config.GetString("cfg.log.type") {
		return
	}

	_ = logger.Rotate(
		config.GetInt64("cfg.log.max_size"),
		config.GetInt64("cfg.log.max_age"),
	)
}
