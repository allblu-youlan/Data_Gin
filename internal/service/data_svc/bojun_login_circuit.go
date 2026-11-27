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
	service.loginCircuits = make(map[string]providerhttp.ProviderCircuitBreaker, 2)
	for _, target := range []shanghaimall.Target{shanghaimall.TargetJialiCheng, shanghaimall.TargetQiantan} {
		circuit, err := providerhttp.NewRedisCircuitBreaker(redis.Client, providerhttp.RedisCircuitBreakerConfig{
			Key: projectredis.GenNamespace("circuit:bojun_login:" + string(target)), FailureThreshold: 1,
			OpenTimeout: bojunLoginCircuitOpenTimeout, ProbeTTL: 2*shanghaimall.RequestTimeout() + 10*time.Second,
		})
		if err != nil {
			service.loginCircuitErr = err
			return service
		}
		service.loginCircuits[string(target)] = circuit
	}
	return service
}

func (service *BojunOrderPushService) allowLogin(ctx context.Context, target bojunOrderPushTarget) error {
	if target.Code != string(shanghaimall.TargetJialiCheng) && target.Code != string(shanghaimall.TargetQiantan) {
		return nil
	}
	if service.loginCircuitErr != nil {
		return service.loginCircuitErr
	}
	circuit := service.loginCircuits[target.Code]
	if circuit == nil {
		return nil
	}
	allowed, remaining, err := circuit.Allow(ctx)
	if err != nil {
		return fmt.Errorf("bojun mall login circuit allow: %w", err)
	}
	if !allowed {
		return &bojunLoginDeferredError{target: target.Code, retryAfter: remaining + time.Second}
	}
	return nil
}

func (service *BojunOrderPushService) reportLogin(ctx context.Context, target bojunOrderPushTarget, pushErr error) error {
	circuit := service.loginCircuits[target.Code]
	if circuit == nil {
		return nil
	}
	if shanghaimall.IsRetryableLoginFailure(pushErr) {
		var hint interface{ RetryDelay() time.Duration }
		var reportErr error
		if errors.As(pushErr, &hint) && hint.RetryDelay() > bojunLoginCircuitOpenTimeout {
			reportErr = circuit.ReportRateLimited(ctx, hint.RetryDelay())
		} else {
			reportErr = circuit.ReportFailure(ctx)
		}
		if reportErr != nil {
			return fmt.Errorf("bojun mall login circuit failure report: %w", reportErr)
		}
		return nil
	}
	// This gate tracks transient login availability. A permanent rejection or
	// a sales error releases its probe without changing the order's result.
	if err := circuit.ReportSuccess(ctx); err != nil && logger.Logger != nil {
		logger.Warn("Bojun mall login circuit recovery report failed", zap.Error(err))
	}
	return nil
}

type bojunLoginDeferredError struct {
	target     string
	retryAfter time.Duration
}

func (err *bojunLoginDeferredError) Error() string {
	return "bojun mall login deferred: target=" + err.target
}

func (err *bojunLoginDeferredError) TaskDeferred() bool { return err != nil }

func (err *bojunLoginDeferredError) RetryDelay() time.Duration {
	if err == nil {
		return 0
	}
	return err.retryAfter
}
