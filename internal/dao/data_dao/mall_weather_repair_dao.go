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
), candidate_runs AS (
  SELECT runs.*
  FROM newest_runs
  INNER JOIN mall_weather_fetch_runs AS runs ON runs.id = newest_runs.id
  WHERE runs.id > ?
  AND runs.task_kind IN ?
  AND runs.status IN ?
), candidate_malls AS (
  SELECT DISTINCT candidate.mall_id
  FROM candidate_runs AS candidate
  WHERE candidate.status = ?
), ranked_latest AS (
  SELECT latest.mall_id, latest.data_kind, latest.subtype,
    latest.fetched_at_utc, latest.freshness_status,
    MAX(CASE WHEN latest.data_kind IN ? THEN latest.fetched_at_utc END)
      OVER (PARTITION BY latest.mall_id, latest.data_kind) AS current_weather_fetched_at,
    MAX(CASE WHEN latest.data_kind = ? AND latest.subtype LIKE ? THEN latest.fetched_at_utc END)
      OVER (PARTITION BY latest.mall_id) AS current_v26_life_fetched_at,
    MAX(CASE WHEN latest.data_kind = ? AND latest.subtype LIKE ? THEN latest.fetched_at_utc END)
      OVER (PARTITION BY latest.mall_id) AS current_v3_life_fetched_at
  FROM mall_weather_latest AS latest
  INNER JOIN candidate_malls AS candidate ON candidate.mall_id = latest.mall_id
  WHERE latest.data_kind IN ?
), current_freshness AS (
  SELECT latest.mall_id,
    MAX(CASE WHEN latest.data_kind IN ?
      AND latest.fetched_at_utc = latest.current_weather_fetched_at
      AND latest.freshness_status IN ? THEN 1 ELSE 0 END) AS weather_needs_repair,
    MAX(CASE WHEN latest.data_kind = ? AND latest.subtype LIKE ?
      AND latest.fetched_at_utc = latest.current_v26_life_fetched_at
      AND latest.freshness_status IN ? THEN 1 ELSE 0 END) AS v26_life_needs_repair,
    MAX(CASE WHEN latest.data_kind = ? AND latest.subtype LIKE ?
      AND latest.fetched_at_utc = latest.current_v3_life_fetched_at
      AND latest.freshness_status IN ? THEN 1 ELSE 0 END) AS v3_life_needs_repair
  FROM ranked_latest AS latest
  GROUP BY latest.mall_id
)
SELECT /*+ MAX_EXECUTION_TIME(%d) */ runs.*
FROM candidate_runs AS runs
LEFT JOIN current_freshness AS freshness ON freshness.mall_id = runs.mall_id
WHERE runs.status IN ?
  OR (runs.endpoint_kind = ? AND (COALESCE(freshness.weather_needs_repair, 0) = 1 OR COALESCE(freshness.v26_life_needs_repair, 0) = 1))
  OR (runs.endpoint_kind = ? AND COALESCE(freshness.v3_life_needs_repair, 0) = 1)
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
	currentDataKinds := append([]string(nil), weatherDataKinds...)
	currentDataKinds = append(currentDataKinds, model.MallWeatherDataKindLife)
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
		afterID,
		taskKinds,
		terminalStatuses,
		"success",
		weatherDataKinds,
		model.MallWeatherDataKindLife,
		v26LifePattern,
		model.MallWeatherDataKindLife,
		v3LifePattern,
		currentDataKinds,
		weatherDataKinds,
		freshnessStatuses,
		model.MallWeatherDataKindLife,
		v26LifePattern,
		freshnessStatuses,
		model.MallWeatherDataKindLife,
		v3LifePattern,
		freshnessStatuses,
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
