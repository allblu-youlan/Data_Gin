package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"
	projectredis "gin-biz-web-api/pkg/redis"
	"gin-biz-web-api/pkg/shanghaimall"

	"go.uber.org/zap"
)

const bojunLoginCircuitOpenTimeout = time.Minute

func newBojunOracleOrderPushService() *BojunOrderPushService {
	service := NewBojunOrderPushService()
	redis := projectredis.Instance()
	if redis == nil || redis.Client == nil {
		service.loginCircuitErr = fmt.Errorf("bojun mall login circuit: redis is unavailable")
		return service
	}
	service.loginCircuit, service.loginCircuitErr = providerhttp.NewRedisCircuitBreaker(redis.Client, providerhttp.RedisCircuitBreakerConfig{
		Key: projectredis.GenNamespace("circuit:bojun_login:jialicheng"), FailureThreshold: 1,
		OpenTimeout: bojunLoginCircuitOpenTimeout, ProbeTTL: 2*shanghaimall.RequestTimeout() + 10*time.Second,
	})
	return service
}

func (service *BojunOrderPushService) allowLogin(ctx context.Context, target bojunOrderPushTarget) error {
	if target.Code != string(shanghaimall.TargetJialiCheng) {
		return nil
	}
	if service.loginCircuitErr != nil {
		return service.loginCircuitErr
	}
	if service.loginCircuit == nil {
		return nil
	}
	allowed, remaining, err := service.loginCircuit.Allow(ctx)
	if err != nil {
		return fmt.Errorf("bojun mall login circuit allow: %w", err)
	}
	if !allowed {
		return &bojunLoginDeferredError{retryAfter: remaining + time.Second}
	}
	return nil
}

func (service *BojunOrderPushService) reportLogin(ctx context.Context, target bojunOrderPushTarget, pushErr error) error {
	if target.Code != string(shanghaimall.TargetJialiCheng) || service.loginCircuit == nil {
		return nil
	}
	if shanghaimall.IsRetryableLoginFailure(pushErr) {
		var hint interface{ RetryDelay() time.Duration }
		var reportErr error
		if errors.As(pushErr, &hint) && hint.RetryDelay() > bojunLoginCircuitOpenTimeout {
			reportErr = service.loginCircuit.ReportRateLimited(ctx, hint.RetryDelay())
		} else {
			reportErr = service.loginCircuit.ReportFailure(ctx)
		}
		if reportErr != nil {
			return fmt.Errorf("bojun mall login circuit failure report: %w", reportErr)
		}
		return nil
	}
	// This gate tracks transient login availability. A permanent rejection or
	// a sales error releases its probe without changing the order's result.
	if err := service.loginCircuit.ReportSuccess(ctx); err != nil && logger.Logger != nil {
		logger.Warn("Bojun mall login circuit recovery report failed", zap.Error(err))
	}
	return nil
}

type bojunLoginDeferredError struct {
	retryAfter time.Duration
}

func (err *bojunLoginDeferredError) Error() string {
	return "bojun mall login deferred: target=jialicheng"
}

func (err *bojunLoginDeferredError) TaskDeferred() bool { return err != nil }

func (err *bojunLoginDeferredError) RetryDelay() time.Duration {
	if err == nil {
		return 0
	}
	return err.retryAfter
}
