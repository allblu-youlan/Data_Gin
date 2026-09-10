package data_dao

import (
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/connector/caiyun"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
)

func TestBuildWeatherRepairCandidateQueryUsesLatestEligibleRun(t *testing.T) {
	statement, args := buildWeatherRepairCandidateQuery(42, 900)
	for _, required := range []string{
		"MAX_EXECUTION_TIME(5000)",
		"runs.id > ?",
		"runs.id = (",
		"SELECT MAX(newest.id)",
		"runs.status IN ?",
		"latest.freshness_status IN ?",
		"latest.subtype LIKE ?",
		"SELECT MAX(current_latest.fetched_at_utc)",
		"SELECT MAX(current_life.fetched_at_utc)",
		"ORDER BY runs.id ASC",
		"LIMIT ?",
	} {
		if !strings.Contains(statement, required) {
			t.Fatalf("query missing %q: %s", required, statement)
		}
	}
	if strings.Contains(statement, caiyun.EndpointWeatherV26) || strings.Contains(statement, weatherdomain.SourceAPIV26Daily) {
		t.Fatalf("query interpolates provider values: %s", statement)
	}
	if strings.Count(statement, "?") != len(args) {
		t.Fatalf("placeholder count=%d args=%d", strings.Count(statement, "?"), len(args))
	}
	if args[0] != uint(42) || args[len(args)-1] != maxWeatherPageSize {
		t.Fatalf("args=%#v", args)
	}
	wantFreshness := []string{model.MallWeatherFreshnessCritical, model.MallWeatherFreshnessStale}
	if !containsRepairQueryArg(args, wantFreshness) {
		t.Fatalf("freshness args=%#v", args)
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

func containsRepairQueryArg(args []interface{}, want interface{}) bool {
	for _, arg := range args {
		if reflect.DeepEqual(arg, want) {
			return true
		}
	}
	return false
}
