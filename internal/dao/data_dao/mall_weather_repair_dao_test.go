package data_dao

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/connector/caiyun"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
)

func TestBuildWeatherRepairCandidateQueryUsesLatestEligibleRun(t *testing.T) {
	statement, args := buildWeatherRepairCandidateQuery(42, 900, 5000)
	for _, required := range []string{
		"MAX_EXECUTION_TIME(5000)",
		"WITH eligible_malls AS",
		"newest_runs AS",
		"candidate_runs AS",
		"candidate_malls AS",
		"WHERE candidate.status = ?",
		"ranked_latest AS",
		"current_freshness AS",
		"OVER (PARTITION BY latest.mall_id, latest.data_kind)",
		"INNER JOIN candidate_malls AS candidate ON candidate.mall_id = latest.mall_id",
		"runs.id > ?",
		"candidate.status IN ?",
		"latest.freshness_status IN ?",
		"latest.subtype LIKE ?",
		"ORDER BY runs.id ASC",
		"LIMIT ?",
	} {
		if !strings.Contains(statement, required) {
			t.Fatalf("query missing %q: %s", required, statement)
		}
	}
	if strings.Count(statement, "FROM mall_weather_latest") != 1 {
		t.Fatalf("latest data must be read once for the candidate malls: %s", statement)
	}
	for _, forbidden := range []string{"runs.id = (", "SELECT MAX(newest.id)", "SELECT MAX(current_latest.fetched_at_utc)"} {
		if strings.Contains(statement, forbidden) {
			t.Fatalf("query retains correlated subquery %q: %s", forbidden, statement)
		}
	}
	if strings.Contains(statement, caiyun.EndpointWeatherV26) || strings.Contains(statement, weatherdomain.SourceAPIV26Daily) {
		t.Fatalf("query interpolates provider values: %s", statement)
	}
	if strings.Count(statement, "?") != len(args) {
		t.Fatalf("placeholder count=%d args=%d", strings.Count(statement, "?"), len(args))
	}
	taskKinds := []string{"fast", "full", "lifeindex", "manual", "repair"}
	terminalStatuses := []string{"success", "partial_success", "failed"}
	repairStatuses := []string{"partial_success", "failed"}
	weatherDataKinds := []string{
		model.MallWeatherDataKindRealtime,
		model.MallWeatherDataKindMinutely,
		model.MallWeatherDataKindHourly,
		model.MallWeatherDataKindDaily,
	}
	currentDataKinds := append([]string(nil), weatherDataKinds...)
	currentDataKinds = append(currentDataKinds, model.MallWeatherDataKindLife)
	wantFreshness := []string{model.MallWeatherFreshnessCritical, model.MallWeatherFreshnessStale}
	v26LifePattern := weatherdomain.SourceAPIV26Daily + ":%"
	v3LifePattern := weatherdomain.SourceAPIV3LifeIndex + ":%"
	wantArgs := []interface{}{
		"active",
		"confirmed",
		true,
		weatherdomain.ProviderCaiyun,
		[]string{caiyun.EndpointWeatherV26, caiyun.EndpointLifeIndexV3},
		taskKinds,
		terminalStatuses,
		uint(42),
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
		wantFreshness,
		model.MallWeatherDataKindLife,
		v26LifePattern,
		wantFreshness,
		model.MallWeatherDataKindLife,
		v3LifePattern,
		wantFreshness,
		repairStatuses,
		caiyun.EndpointWeatherV26,
		caiyun.EndpointLifeIndexV3,
		maxWeatherPageSize,
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args=%#v want=%#v", args, wantArgs)
	}
}

func TestWeatherRepairExecutionLimitLeavesServerCleanupMargin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	limit := weatherRepairExecutionLimit(ctx)
	if limit < 2000 || limit > 2500 {
		t.Fatalf("weatherRepairExecutionLimit()=%d, want about 2500ms", limit)
	}
	if got := normalizeWeatherRepairExecutionLimit(8000); got != 7000 {
		t.Fatalf("normalizeWeatherRepairExecutionLimit(8000)=%d want=7000", got)
	}
	if got := normalizeWeatherRepairExecutionLimit(0); got != 1 {
		t.Fatalf("normalizeWeatherRepairExecutionLimit(0)=%d want=1", got)
	}
}

func TestNormalizeWeatherRepairPageSize(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "default", input: 0, want: 100},
		{name: "requested", input: 200, want: 200},
		{name: "capped", input: 501, want: maxWeatherPageSize},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeWeatherRepairPageSize(test.input); got != test.want {
				t.Fatalf("normalizeWeatherRepairPageSize()=%d want=%d", got, test.want)
			}
		})
	}
}
