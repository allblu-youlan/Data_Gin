package data_dao

import (
	"context"
	"fmt"
	"time"

	"gin-biz-web-api/connector/caiyun"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
)

const (
	weatherRepairDefaultExecutionLimit = 5 * time.Second
	weatherRepairMaximumExecutionLimit = 7 * time.Second
	weatherRepairExecutionSafetyMargin = 500 * time.Millisecond
)

const weatherRepairCandidateQuery = `WITH eligible_malls AS (
  SELECT mall.id
  FROM malls AS mall
  WHERE mall.deleted_at IS NULL
  AND mall.status = ?
  AND mall.geocode_status = ?
  AND mall.weather_enabled = ?
  AND mall.weather_provider = ?
), newest_runs AS (
  SELECT candidate.mall_id, candidate.endpoint_kind, MAX(candidate.id) AS id
  FROM mall_weather_fetch_runs AS candidate
  INNER JOIN eligible_malls AS mall ON mall.id = candidate.mall_id
  WHERE candidate.endpoint_kind IN ?
  AND candidate.task_kind IN ?
  AND candidate.status IN ?
  GROUP BY candidate.mall_id, candidate.endpoint_kind
), current_weather AS (
  SELECT latest.mall_id,
    MAX(CASE WHEN latest.freshness_status IN ? THEN 1 ELSE 0 END) AS needs_repair
  FROM mall_weather_latest AS latest
  INNER JOIN (
    SELECT mall_id, data_kind, MAX(fetched_at_utc) AS fetched_at_utc
    FROM mall_weather_latest
    WHERE data_kind IN ?
    GROUP BY mall_id, data_kind
  ) AS current
    ON current.mall_id = latest.mall_id
    AND current.data_kind = latest.data_kind
    AND current.fetched_at_utc = latest.fetched_at_utc
  GROUP BY latest.mall_id
), current_v26_life AS (
  SELECT latest.mall_id,
    MAX(CASE WHEN latest.freshness_status IN ? THEN 1 ELSE 0 END) AS needs_repair
  FROM mall_weather_latest AS latest
  INNER JOIN (
    SELECT mall_id, MAX(fetched_at_utc) AS fetched_at_utc
    FROM mall_weather_latest
    WHERE data_kind = ? AND subtype LIKE ?
    GROUP BY mall_id
  ) AS current
    ON current.mall_id = latest.mall_id
    AND current.fetched_at_utc = latest.fetched_at_utc
  WHERE latest.data_kind = ? AND latest.subtype LIKE ?
  GROUP BY latest.mall_id
), current_v3_life AS (
  SELECT latest.mall_id,
    MAX(CASE WHEN latest.freshness_status IN ? THEN 1 ELSE 0 END) AS needs_repair
  FROM mall_weather_latest AS latest
  INNER JOIN (
    SELECT mall_id, MAX(fetched_at_utc) AS fetched_at_utc
    FROM mall_weather_latest
    WHERE data_kind = ? AND subtype LIKE ?
    GROUP BY mall_id
  ) AS current
    ON current.mall_id = latest.mall_id
    AND current.fetched_at_utc = latest.fetched_at_utc
  WHERE latest.data_kind = ? AND latest.subtype LIKE ?
  GROUP BY latest.mall_id
)
SELECT /*+ MAX_EXECUTION_TIME(%d) */ runs.*
FROM newest_runs
INNER JOIN mall_weather_fetch_runs AS runs ON runs.id = newest_runs.id
LEFT JOIN current_weather AS weather ON weather.mall_id = runs.mall_id
LEFT JOIN current_v26_life AS v26_life ON v26_life.mall_id = runs.mall_id
LEFT JOIN current_v3_life AS v3_life ON v3_life.mall_id = runs.mall_id
WHERE runs.id > ?
  AND runs.task_kind IN ?
  AND runs.status IN ?
  AND (
    runs.status IN ?
    OR (runs.endpoint_kind = ? AND (COALESCE(weather.needs_repair, 0) = 1 OR COALESCE(v26_life.needs_repair, 0) = 1))
    OR (runs.endpoint_kind = ? AND COALESCE(v3_life.needs_repair, 0) = 1)
  )
ORDER BY runs.id ASC
LIMIT ?`

func (dao *MallWeatherDAO) ListRepairCandidatesAfterID(ctx context.Context, afterID uint, limit int) ([]model.MallWeatherFetchRun, error) {
	if dao == nil || dao.db == nil || ctx == nil {
		return nil, fmt.Errorf("mall weather: repair candidate store is not configured")
	}
	statement, args := buildWeatherRepairCandidateQuery(afterID, limit, weatherRepairExecutionLimit(ctx))
	var rows []model.MallWeatherFetchRun
	if err := dao.db.WithContext(ctx).Raw(statement, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("mall weather: list repair candidates: %w", err)
	}
	return rows, nil
}

func buildWeatherRepairCandidateQuery(afterID uint, limit, maxExecutionMillis int) (string, []interface{}) {
	limit = normalizeWeatherRepairPageSize(limit)
	maxExecutionMillis = normalizeWeatherRepairExecutionLimit(maxExecutionMillis)
	terminalStatuses := []string{"success", "partial_success", "failed"}
	repairStatuses := []string{"partial_success", "failed"}
	taskKinds := []string{"fast", "full", "lifeindex", "manual", "repair"}
	weatherDataKinds := []string{
		model.MallWeatherDataKindRealtime,
		model.MallWeatherDataKindMinutely,
		model.MallWeatherDataKindHourly,
		model.MallWeatherDataKindDaily,
	}
	freshnessStatuses := []string{model.MallWeatherFreshnessCritical, model.MallWeatherFreshnessStale}
	v26LifePattern := weatherdomain.SourceAPIV26Daily + ":%"
	v3LifePattern := weatherdomain.SourceAPIV3LifeIndex + ":%"
	args := []interface{}{
		"active",
		"confirmed",
		true,
		weatherdomain.ProviderCaiyun,
		[]string{caiyun.EndpointWeatherV26, caiyun.EndpointLifeIndexV3},
		taskKinds,
		terminalStatuses,
		freshnessStatuses,
		weatherDataKinds,
		freshnessStatuses,
		model.MallWeatherDataKindLife,
		v26LifePattern,
		model.MallWeatherDataKindLife,
		v26LifePattern,
		freshnessStatuses,
		model.MallWeatherDataKindLife,
		v3LifePattern,
		model.MallWeatherDataKindLife,
		v3LifePattern,
		afterID,
		taskKinds,
		terminalStatuses,
		repairStatuses,
		caiyun.EndpointWeatherV26,
		caiyun.EndpointLifeIndexV3,
		limit,
	}
	return fmt.Sprintf(weatherRepairCandidateQuery, maxExecutionMillis), args
}

func weatherRepairExecutionLimit(ctx context.Context) int {
	limit := weatherRepairDefaultExecutionLimit
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			limit = time.Until(deadline) - weatherRepairExecutionSafetyMargin
		}
	}
	return normalizeWeatherRepairExecutionLimit(int(limit / time.Millisecond))
}

func normalizeWeatherRepairExecutionLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	maximum := int(weatherRepairMaximumExecutionLimit / time.Millisecond)
	if limit > maximum {
		return maximum
	}
	return limit
}

func normalizeWeatherRepairPageSize(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > maxWeatherPageSize {
		return maxWeatherPageSize
	}
	return limit
}
