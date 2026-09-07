package data_dao

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestNormalizePageSizes(t *testing.T) {
	tests := []struct {
		name   string
		input  int
		mall   int
		outbox int
	}{
		{"default", 0, 50, 100},
		{"requested", 25, 25, 25},
		{"bounded", 1000, maxMallPageSize, maxOutboxClaimSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePageSize(tt.input); got != tt.mall {
				t.Fatalf("normalizePageSize(%d) = %d, want %d", tt.input, got, tt.mall)
			}
			if got := normalizeOutboxClaimSize(tt.input); got != tt.outbox {
				t.Fatalf("normalizeOutboxClaimSize(%d) = %d, want %d", tt.input, got, tt.outbox)
			}
		})
	}
}

func TestSanitizeMallUpdates(t *testing.T) {
	tests := []struct {
		name      string
		updates   map[string]interface{}
		wantError bool
	}{
		{"allows profile field", map[string]interface{}{"name_cn": "updated"}, false},
		{"rejects immutable code", map[string]interface{}{"mall_code": "changed"}, true},
		{"rejects arbitrary sql field", map[string]interface{}{"name_cn = ?; DROP TABLE malls": "x"}, true},
		{"rejects empty update", map[string]interface{}{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeMallUpdates(tt.updates)
			if (err != nil) != tt.wantError {
				t.Fatalf("sanitizeMallUpdates() error = %v, wantError %t", err, tt.wantError)
			}
			if err == nil && !reflect.DeepEqual(got, tt.updates) {
				t.Fatalf("sanitizeMallUpdates() = %v, want %v", got, tt.updates)
			}
		})
	}
}

func TestSanitizeFetchRunUpdates(t *testing.T) {
	if _, err := sanitizeFetchRunUpdates(map[string]interface{}{"status": "running"}); err != nil {
		t.Fatalf("sanitizeFetchRunUpdates() rejected status: %v", err)
	}
	if _, err := sanitizeFetchRunUpdates(map[string]interface{}{"task_window": "changed"}); err == nil {
		t.Fatal("sanitizeFetchRunUpdates() accepted immutable task window")
	}
}

func TestSanitizeFetchAttemptUpdates(t *testing.T) {
	if _, err := sanitizeFetchAttemptUpdates(map[string]interface{}{"status": "success"}); err != nil {
		t.Fatalf("sanitizeFetchAttemptUpdates() rejected status: %v", err)
	}
	if _, err := sanitizeFetchAttemptUpdates(map[string]interface{}{"attempt_no": 99}); err == nil {
		t.Fatal("sanitizeFetchAttemptUpdates() accepted immutable attempt number")
	}
}

func TestFindRawSnapshotByIDRejectsInvalidBoundary(t *testing.T) {
	if _, err := (&MallWeatherDAO{}).FindRawSnapshotByID(context.Background(), 1); err == nil {
		t.Fatal("FindRawSnapshotByID() accepted unconfigured DAO")
	}
	if _, err := NewMallWeatherDAO(dryRunWeatherDAOTestDB(t)).FindRawSnapshotByID(context.Background(), 0); err == nil {
		t.Fatal("FindRawSnapshotByID() accepted zero snapshot ID")
	}
}

func TestRawSnapshotByIDQueryUsesBoundID(t *testing.T) {
	dao := NewMallWeatherDAO(dryRunWeatherDAOTestDB(t))
	var row model.ProviderRawSnapshot
	query := dao.rawSnapshotByIDQuery(context.Background(), 123, &row)
	if query.Error != nil {
		t.Fatalf("rawSnapshotByIDQuery() error=%v", query.Error)
	}
	statement := query.Statement.SQL.String()
	if !strings.Contains(statement, "provider_raw_snapshots") ||
		!strings.Contains(statement, "id = ?") ||
		strings.Contains(statement, "123") ||
		len(query.Statement.Vars) == 0 ||
		query.Statement.Vars[0] != uint(123) {
		t.Fatalf("statement=%s vars=%v", statement, query.Statement.Vars)
	}
}

func TestClassifyFetchAttemptStart(t *testing.T) {
	now := time.Date(2026, 7, 22, 3, 0, 0, 0, time.UTC)
	const staleAfter = 10 * time.Minute
	tests := []struct {
		name        string
		run         model.MallWeatherFetchRun
		attempt     *model.MallWeatherFetchAttempt
		want        FetchAttemptDisposition
		wantRecover bool
		wantError   bool
	}{
		{name: "pending is acquired", run: fetchRunState(0, "pending"), want: FetchAttemptDispositionAcquired},
		{
			name: "failed is acquired", run: fetchRunState(1, "failed"),
			attempt: fetchAttemptState(1, "transport_failed", now.Add(-time.Minute)), want: FetchAttemptDispositionAcquired,
		},
		{
			name: "success is terminal", run: fetchRunState(1, "success"),
			attempt: fetchAttemptState(1, "success", now.Add(-time.Minute)), want: FetchAttemptDispositionTerminal,
		},
		{
			name: "partial success is terminal", run: fetchRunState(1, "partial_success"),
			attempt: fetchAttemptState(1, "partial_success", now.Add(-time.Minute)), want: FetchAttemptDispositionTerminal,
		},
		{
			name: "fresh running attempt is busy", run: fetchRunState(2, "running"),
			attempt: fetchAttemptState(2, "running", now.Add(-time.Minute)), want: FetchAttemptDispositionBusy,
		},
		{
			name: "stale running attempt is recovered", run: fetchRunState(2, "running"),
			attempt: fetchAttemptState(2, "running", now.Add(-staleAfter)), want: FetchAttemptDispositionAcquired, wantRecover: true,
		},
		{name: "running without matching attempt is rejected", run: fetchRunState(2, "running"), wantError: true},
		{name: "unknown state is rejected", run: fetchRunState(1, "deleted"), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, recoverInterrupted, err := classifyFetchAttemptStart(&test.run, test.attempt, now, staleAfter)
			if (err != nil) != test.wantError {
				t.Fatalf("classifyFetchAttemptStart() error=%v wantError=%v", err, test.wantError)
			}
			if err == nil && (got != test.want || recoverInterrupted != test.wantRecover) {
				t.Fatalf("classifyFetchAttemptStart()=(%v,%v) want=(%v,%v)", got, recoverInterrupted, test.want, test.wantRecover)
			}
		})
	}
}

func fetchRunState(attemptCount int, status string) model.MallWeatherFetchRun {
	return model.MallWeatherFetchRun{BaseModel: model.BaseModel{ID: 7}, AttemptCount: attemptCount, Status: status}
}

func fetchAttemptState(attemptNo int, status string, startedAt time.Time) *model.MallWeatherFetchAttempt {
	return &model.MallWeatherFetchAttempt{
		BaseModel: model.BaseModel{ID: 11}, FetchRunID: 7, AttemptNo: attemptNo, StartedAt: startedAt, Status: status,
	}
}

func TestBuildHourlyQuery(t *testing.T) {
	start := time.Date(2026, 7, 17, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	end := start.Add(72 * time.Hour)
	asOf := start.Add(10 * time.Hour)
	cursor := start.Add(12 * time.Hour)
	issuedCursor := cursor.Add(-time.Hour)

	tests := []struct {
		name         string
		query        HourlyQuery
		contains     []string
		wantArgCount int
		wantError    bool
	}{
		{
			name:  "latest versions use window function",
			query: HourlyQuery{MallID: 1, StartUTC: start, EndUTC: end, Latest: true, Limit: 200},
			contains: []string{
				"SELECT weather.*",
				"SELECT w.id, w.forecast_time_utc, ROW_NUMBER() OVER",
				"PARTITION BY w.forecast_time_utc",
				"INNER JOIN mall_weather_hourly AS weather ON weather.id = ranked.id",
				"version_rank = 1",
				"LIMIT ?",
			},
			wantArgCount: 4,
		},
		{
			name: "overview latest prefers a version with temperature",
			query: HourlyQuery{
				MallID: 1, StartUTC: start, EndUTC: end, Latest: true,
				PreferNonNullTemperature: true, Limit: 24,
			},
			contains: []string{
				"ROW_NUMBER() OVER",
				"ORDER BY (w.temperature_c IS NULL) ASC, w.issued_at_utc DESC, w.id DESC",
			},
			wantArgCount: 4,
		},
		{
			name:         "as of and cursor stay parameterized",
			query:        HourlyQuery{MallID: 1, StartUTC: start, EndUTC: end, AsOfUTC: &asOf, QualityStatus: "valid", AfterForecastTime: &cursor, AfterID: 99, Limit: 900},
			contains:     []string{"w.issued_at_utc <= ?", "w.quality_status = ?", "ranked.forecast_time_utc > ?", "ROW_NUMBER() OVER"},
			wantArgCount: 9,
		},
		{
			name:         "all versions omit ranking",
			query:        HourlyQuery{MallID: 1, StartUTC: start, EndUTC: end},
			contains:     []string{"SELECT w.*", "ORDER BY w.forecast_time_utc ASC"},
			wantArgCount: 4,
		},
		{
			name:         "version history cursor includes issued at",
			query:        HourlyQuery{MallID: 1, StartUTC: start, EndUTC: end, AfterForecastTime: &cursor, AfterIssuedAtUTC: &issuedCursor, AfterID: 7},
			contains:     []string{"w.issued_at_utc < ?", "w.issued_at_utc = ?", "ORDER BY w.forecast_time_utc ASC"},
			wantArgCount: 10,
		},
		{
			name:      "version history rejects incomplete cursor",
			query:     HourlyQuery{MallID: 1, StartUTC: start, EndUTC: end, AfterForecastTime: &cursor},
			wantError: true,
		},
		{
			name:      "invalid range",
			query:     HourlyQuery{MallID: 1, StartUTC: end, EndUTC: start},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statement, args, err := buildHourlyQuery(tt.query)
			if (err != nil) != tt.wantError {
				t.Fatalf("buildHourlyQuery() error = %v, wantError %t", err, tt.wantError)
			}
			if err != nil {
				return
			}
			for _, fragment := range tt.contains {
				if !strings.Contains(statement, fragment) {
					t.Fatalf("query does not contain %q:\n%s", fragment, statement)
				}
			}
			if len(args) != tt.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(args), tt.wantArgCount)
			}
			if strings.Contains(statement, "valid") || strings.Contains(statement, "2026-") {
				t.Fatalf("query interpolated a caller value:\n%s", statement)
			}
			if (tt.query.Latest || tt.query.AsOfUTC != nil) && strings.Contains(statement, "SELECT w.*") {
				t.Fatalf("ranked subquery projects the full weather row:\n%s", statement)
			}
		})
	}
}

func TestChecksumAwareUpdateSetPreservesIdentityAndOrdersChecksumLast(t *testing.T) {
	db := dryRunWeatherDAOTestDB(t)
	updates, err := checksumAwareUpdateSet(db, &model.MallWeatherAlert{}, []string{"provider", "alert_id"}, []string{"first_seen_at"})
	if err != nil {
		t.Fatalf("checksumAwareUpdateSet() error=%v", err)
	}
	byName := make(map[string]clause.Assignment, len(updates))
	for _, update := range updates {
		byName[update.Column.Name] = update
	}
	for _, forbidden := range []string{"id", "provider", "alert_id", "first_seen_at", "created_at"} {
		if _, exists := byName[forbidden]; exists {
			t.Fatalf("immutable column %q is updated", forbidden)
		}
	}
	lastSeen, ok := byName["last_seen_at"].Value.(clause.Expr)
	if !ok || lastSeen.SQL != "GREATEST(`last_seen_at`, VALUES(`last_seen_at`))" {
		t.Fatalf("last_seen_at assignment=%+v", byName["last_seen_at"])
	}
	title, ok := byName["title"].Value.(clause.Expr)
	if !ok || !strings.Contains(title.SQL, "IF(`raw_checksum` = VALUES(`raw_checksum`)") {
		t.Fatalf("title assignment=%+v", byName["title"])
	}
	if updates[len(updates)-1].Column.Name != "raw_checksum" {
		t.Fatalf("last assignment=%s want raw_checksum", updates[len(updates)-1].Column.Name)
	}
}

func TestDeactivateMissingAlertRelationsIsScopedAndParameterized(t *testing.T) {
	dao := NewMallWeatherDAO(dryRunWeatherDAOTestDB(t))
	missingBefore := time.Date(2026, 7, 28, 1, 30, 0, 0, time.UTC)
	query := dao.deactivateMissingAlertRelationsQuery(
		context.Background(),
		7,
		"caiyun",
		[]uint{11, 13},
		missingBefore,
	)
	if query.Error != nil {
		t.Fatalf("deactivateMissingAlertRelationsQuery() error=%v", query.Error)
	}
	statement := query.Statement.SQL.String()
	for _, fragment := range []string{
		"UPDATE `mall_weather_alert_relations`",
		"mall_id = ?",
		"is_active = ?",
		"last_seen_at <= ?",
		"alert_pk IN (SELECT `id` FROM `mall_weather_alerts` WHERE provider = ?)",
		"alert_pk NOT IN (?,?)",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("query does not contain %q: %s", fragment, statement)
		}
	}
	if strings.Contains(statement, "caiyun") || strings.Contains(statement, "2026-") ||
		len(query.Statement.Vars) != 8 {
		t.Fatalf("statement=%s vars=%v", statement, query.Statement.Vars)
	}
}

func TestDeactivateMissingAlertRelationsAcceptsAnEmptyCurrentSet(t *testing.T) {
	dao := NewMallWeatherDAO(dryRunWeatherDAOTestDB(t))
	query := dao.deactivateMissingAlertRelationsQuery(
		context.Background(),
		7,
		"caiyun",
		nil,
		time.Date(2026, 7, 28, 1, 30, 0, 0, time.UTC),
	)
	if query.Error != nil || strings.Contains(query.Statement.SQL.String(), "NOT IN") {
		t.Fatalf("statement=%s vars=%v error=%v", query.Statement.SQL.String(), query.Statement.Vars, query.Error)
	}
}

func TestChecksumConflictPredicateIsParameterized(t *testing.T) {
	issuedAt := time.Date(2026, 7, 22, 3, 0, 0, 0, time.UTC)
	rows := []model.MallWeatherHourly{
		{
			MallID: 7, Provider: "caiyun", ForecastTimeUTC: issuedAt.Add(time.Hour), IssuedAtUTC: issuedAt,
			WeatherQualityFields: model.WeatherQualityFields{RawChecksum: strings.Repeat("a", 64)},
		},
		{
			MallID: 7, Provider: "caiyun", ForecastTimeUTC: issuedAt.Add(2 * time.Hour), IssuedAtUTC: issuedAt,
			WeatherQualityFields: model.WeatherQualityFields{RawChecksum: strings.Repeat("b", 64)},
		},
	}
	predicate, args, err := checksumConflictPredicate(context.Background(), dryRunWeatherDAOTestDB(t), rows,
		[]string{"mall_id", "provider", "forecast_time_utc", "issued_at_utc"})
	if err != nil {
		t.Fatalf("checksumConflictPredicate() error=%v", err)
	}
	if strings.Contains(predicate, strings.Repeat("a", 64)) || strings.Count(predicate, "`raw_checksum` <> ?") != 2 || len(args) != 10 {
		t.Fatalf("predicate=%s args=%v", predicate, args)
	}
	identityPredicate, identityArgs, err := checksumIdentityPredicate(context.Background(), dryRunWeatherDAOTestDB(t), rows,
		[]string{"mall_id", "provider", "forecast_time_utc", "issued_at_utc"})
	if err != nil {
		t.Fatalf("checksumIdentityPredicate() error=%v", err)
	}
	if strings.Contains(identityPredicate, strings.Repeat("a", 64)) || strings.Contains(identityPredicate, "raw_checksum") || len(identityArgs) != 8 {
		t.Fatalf("identity predicate=%s args=%v", identityPredicate, identityArgs)
	}
}

func TestChecksumIdentityPredicateBindsDateColumnsAsCalendarDates(t *testing.T) {
	forecastDate := time.Date(2026, 7, 27, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	issuedAt := time.Date(2026, 7, 27, 10, 4, 0, 0, time.UTC)
	rows := []model.MallWeatherDaily{{
		MallID: 3, Provider: "caiyun", ForecastDateLocal: forecastDate, IssuedAtUTC: issuedAt,
	}}
	_, args, err := checksumIdentityPredicate(context.Background(), dryRunWeatherDAOTestDB(t), rows,
		[]string{"mall_id", "provider", "forecast_date_local", "issued_at_utc"})
	if err != nil {
		t.Fatalf("checksumIdentityPredicate() error=%v", err)
	}
	if len(args) != 4 {
		t.Fatalf("identity args=%v", args)
	}
	if date, ok := args[2].(string); !ok || date != "2026-07-27" {
		t.Fatalf("forecast_date_local arg=%#v, want calendar date string", args[2])
	}
	if value, ok := args[3].(time.Time); !ok || !value.Equal(issuedAt) {
		t.Fatalf("issued_at_utc arg=%#v, want time.Time", args[3])
	}

	lifeRows := []model.MallWeatherLifeIndex{{
		MallID: 3, Provider: "caiyun", SourceAPI: "v26_daily", ForecastDateLocal: forecastDate,
		IndexType: 6, IssuedAtUTC: issuedAt,
	}}
	_, lifeArgs, err := checksumIdentityPredicate(context.Background(), dryRunWeatherDAOTestDB(t), lifeRows,
		[]string{"mall_id", "provider", "source_api", "forecast_date_local", "index_type", "issued_at_utc"})
	if err != nil {
		t.Fatalf("life checksumIdentityPredicate() error=%v", err)
	}
	if len(lifeArgs) != 6 || lifeArgs[3] != "2026-07-27" {
		t.Fatalf("life identity args=%v", lifeArgs)
	}
}

func TestChecksumAwareUpdateSetSupportsEmbeddedWeatherQualityFields(t *testing.T) {
	updates, err := checksumAwareUpdateSet(dryRunWeatherDAOTestDB(t), &model.MallWeatherHourly{},
		[]string{"mall_id", "provider", "forecast_time_utc", "issued_at_utc"}, nil)
	if err != nil {
		t.Fatalf("checksumAwareUpdateSet() error=%v", err)
	}
	if len(updates) == 0 || updates[len(updates)-1].Column.Name != "raw_checksum" {
		t.Fatalf("updates=%+v", updates)
	}
}

func dryRunWeatherDAOTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn: &sql.DB{}, SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("gorm.Open() error=%v", err)
	}
	return db
}
