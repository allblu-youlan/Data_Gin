package data_dao

import (
	"context"
	"fmt"

	"gin-biz-web-api/connector/caiyun"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
)

const weatherRepairCandidateQuery = `SELECT /*+ MAX_EXECUTION_TIME(5000) */ runs.*
FROM mall_weather_fetch_runs AS runs
INNER JOIN malls AS mall ON mall.id = runs.mall_id AND mall.deleted_at IS NULL
WHERE runs.id > ?
  AND mall.status = ?
  AND mall.geocode_status = ?
  AND mall.weather_enabled = ?
  AND mall.weather_provider = ?
  AND runs.endpoint_kind IN ?
  AND runs.task_kind IN ?
  AND runs.status IN ?
  AND runs.id = (
    SELECT MAX(newest.id)
    FROM mall_weather_fetch_runs AS newest
    WHERE newest.mall_id = runs.mall_id
      AND newest.endpoint_kind = runs.endpoint_kind
      AND newest.task_kind IN ?
      AND newest.status IN ?
  )
  AND (
    runs.status IN ?
    OR EXISTS (
      SELECT 1
      FROM mall_weather_latest AS latest
      WHERE latest.mall_id = runs.mall_id
        AND latest.freshness_status IN ?
        AND (
          (runs.endpoint_kind = ? AND (
            (latest.data_kind IN ? AND latest.fetched_at_utc = (
              SELECT MAX(current_latest.fetched_at_utc)
              FROM mall_weather_latest AS current_latest
              WHERE current_latest.mall_id = latest.mall_id
                AND current_latest.data_kind = latest.data_kind
            ))
            OR (latest.data_kind = ? AND latest.subtype LIKE ? AND latest.fetched_at_utc = (
              SELECT MAX(current_life.fetched_at_utc)
              FROM mall_weather_latest AS current_life
              WHERE current_life.mall_id = latest.mall_id
                AND current_life.data_kind = ?
                AND current_life.subtype LIKE ?
            ))
          ))
          OR (runs.endpoint_kind = ? AND latest.data_kind = ? AND latest.subtype LIKE ? AND latest.fetched_at_utc = (
            SELECT MAX(current_life.fetched_at_utc)
            FROM mall_weather_latest AS current_life
            WHERE current_life.mall_id = latest.mall_id
              AND current_life.data_kind = ?
              AND current_life.subtype LIKE ?
          ))
        )
    )
  )
ORDER BY runs.id ASC
LIMIT ?`

func (dao *MallWeatherDAO) ListRepairCandidatesAfterID(ctx context.Context, afterID uint, limit int) ([]model.MallWeatherFetchRun, error) {
	if dao == nil || dao.db == nil || ctx == nil {
		return nil, fmt.Errorf("mall weather: repair candidate store is not configured")
	}
	statement, args := buildWeatherRepairCandidateQuery(afterID, limit)
	var rows []model.MallWeatherFetchRun
	if err := dao.db.WithContext(ctx).Raw(statement, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("mall weather: list repair candidates: %w", err)
	}
	return rows, nil
}

func buildWeatherRepairCandidateQuery(afterID uint, limit int) (string, []interface{}) {
	limit = normalizeWeatherRepairPageSize(limit)
	terminalStatuses := []string{"success", "partial_success", "failed"}
	repairStatuses := []string{"partial_success", "failed"}
	taskKinds := []string{"fast", "full", "lifeindex", "manual", "repair"}
	args := []interface{}{
		afterID,
		"active",
		"confirmed",
		true,
		weatherdomain.ProviderCaiyun,
		[]string{caiyun.EndpointWeatherV26, caiyun.EndpointLifeIndexV3},
		taskKinds,
		terminalStatuses,
		taskKinds,
		terminalStatuses,
		repairStatuses,
		[]string{model.MallWeatherFreshnessCritical, model.MallWeatherFreshnessStale},
		caiyun.EndpointWeatherV26,
		[]string{
			model.MallWeatherDataKindRealtime,
			model.MallWeatherDataKindMinutely,
			model.MallWeatherDataKindHourly,
			model.MallWeatherDataKindDaily,
		},
		model.MallWeatherDataKindLife,
		weatherdomain.SourceAPIV26Daily + ":%",
		model.MallWeatherDataKindLife,
		weatherdomain.SourceAPIV26Daily + ":%",
		caiyun.EndpointLifeIndexV3,
		model.MallWeatherDataKindLife,
		weatherdomain.SourceAPIV3LifeIndex + ":%",
		model.MallWeatherDataKindLife,
		weatherdomain.SourceAPIV3LifeIndex + ":%",
		limit,
	}
	return weatherRepairCandidateQuery, args
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
