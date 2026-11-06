package weather

import (
	"gin-biz-web-api/pkg/providerhttp"
	redisv8 "github.com/go-redis/redis/v8"
)

type ProviderCircuitBreaker = providerhttp.ProviderCircuitBreaker
type RedisCircuitBreakerConfig = providerhttp.RedisCircuitBreakerConfig
type RedisCircuitBreaker = providerhttp.RedisCircuitBreaker

func NewRedisCircuitBreaker(client *redisv8.Client, cfg RedisCircuitBreakerConfig) (*RedisCircuitBreaker, error) {
	return providerhttp.NewRedisCircuitBreaker(client, cfg)
}
