package bootstrap

import (
	"reflect"
	"testing"

	"gin-biz-web-api/model"
)

func TestMallWeatherMigrationModelsAreUnique(t *testing.T) {
	models := mallWeatherMigrationModels()
	if len(models) != 24 {
		t.Fatalf("mallWeatherMigrationModels() count = %d, want 24", len(models))
	}

	seen := make(map[reflect.Type]struct{}, len(models))
	for _, model := range models {
		modelType := reflect.TypeOf(model)
		if _, exists := seen[modelType]; exists {
			t.Fatalf("duplicate migration model %v", modelType)
		}
		seen[modelType] = struct{}{}
	}
}

func TestReportCenterMigrationModelsAreUnique(t *testing.T) {
	models := reportCenterMigrationModels()
	if len(models) != 14 {
		t.Fatalf("reportCenterMigrationModels() count = %d, want 14", len(models))
	}

	seen := make(map[reflect.Type]struct{}, len(models))
	for _, model := range models {
		modelType := reflect.TypeOf(model)
		if _, exists := seen[modelType]; exists {
			t.Fatalf("duplicate migration model %v", modelType)
		}
		seen[modelType] = struct{}{}
	}
}

func TestSchemaMigrationVersionIncludesOfficeWebDAV(t *testing.T) {
	if schemaMigrationVersion != "2026-09-08-office-webdav-v22" {
		t.Fatalf("schemaMigrationVersion = %q", schemaMigrationVersion)
	}
	if previousSchemaMigrationVersion != "2026-09-04-report-category-access-v21" {
		t.Fatalf("previousSchemaMigrationVersion = %q", previousSchemaMigrationVersion)
	}
	if accessPermissionMigrationVersion != "2026-09-01-access-permission-v20" {
		t.Fatalf("accessPermissionMigrationVersion = %q", accessPermissionMigrationVersion)
	}
	if officeMessageScheduleMigrationVersion != "2026-09-01-office-message-schedule-v19" {
		t.Fatalf("officeMessageScheduleMigrationVersion = %q", officeMessageScheduleMigrationVersion)
	}
	if officeMessageFileMigrationVersion != "2026-09-01-office-message-file-v18" {
		t.Fatalf("officeMessageFileMigrationVersion = %q", officeMessageFileMigrationVersion)
	}
	if officeMessageBotMigrationVersion != "2026-09-01-office-message-bot-v17" {
		t.Fatalf("officeMessageBotMigrationVersion = %q", officeMessageBotMigrationVersion)
	}
	if officeMessageCompatMigrationVersion != "2026-09-01-office-message-compat-v16" {
		t.Fatalf("officeMessageCompatMigrationVersion = %q", officeMessageCompatMigrationVersion)
	}
	if officeMessagePreviousMigrationVersion != "2026-09-01-office-message-v14" {
		t.Fatalf("officeMessagePreviousMigrationVersion = %q", officeMessagePreviousMigrationVersion)
	}
	if officeMessageMigrationBaselineVersion != "2026-08-26-bojun-oracle-v13" {
		t.Fatalf("officeMessageMigrationBaselineVersion = %q", officeMessageMigrationBaselineVersion)
	}
	if bojunOracleMigrationBaselineVersion != "2026-08-25-report-center-v11" {
		t.Fatalf("bojunOracleMigrationBaselineVersion = %q", bojunOracleMigrationBaselineVersion)
	}
}

func TestOfficeMessagePreferredMigrationBaseline(t *testing.T) {
	tests := []struct {
		name            string
		appliedVersions []string
		wantVersion     string
		wantApplied     bool
	}{
		{
			name:            "v21 direct upgrade",
			appliedVersions: []string{"2026-09-04-report-category-access-v21"},
			wantVersion:     "2026-09-04-report-category-access-v21",
			wantApplied:     true,
		},
		{
			name:            "v20 direct upgrade",
			appliedVersions: []string{"2026-09-01-access-permission-v20"},
			wantVersion:     "2026-09-01-access-permission-v20",
			wantApplied:     true,
		},
		{
			name:            "v19 direct upgrade",
			appliedVersions: []string{"2026-09-01-office-message-schedule-v19"},
			wantVersion:     "2026-09-01-office-message-schedule-v19",
			wantApplied:     true,
		},
		{
			name:            "v18 direct upgrade",
			appliedVersions: []string{"2026-09-01-office-message-file-v18"},
			wantVersion:     "2026-09-01-office-message-file-v18",
			wantApplied:     true,
		},
		{
			name:            "v17 direct upgrade",
			appliedVersions: []string{"2026-09-01-office-message-bot-v17"},
			wantVersion:     "2026-09-01-office-message-bot-v17",
			wantApplied:     true,
		},
		{
			name:            "v16 direct upgrade",
			appliedVersions: []string{"2026-09-01-office-message-compat-v16"},
			wantVersion:     "2026-09-01-office-message-compat-v16",
			wantApplied:     true,
		},
		{
			name:            "v15 direct upgrade",
			appliedVersions: []string{"2026-09-01-office-message-ha-v15"},
			wantVersion:     "2026-09-01-office-message-ha-v15",
			wantApplied:     true,
		},
		{
			name:            "v14 skips v15",
			appliedVersions: []string{"2026-09-01-office-message-v14"},
			wantVersion:     "2026-09-01-office-message-v14",
			wantApplied:     true,
		},
		{
			name:            "v13 skips v14 and v15",
			appliedVersions: []string{"2026-08-26-bojun-oracle-v13"},
			wantVersion:     "2026-08-26-bojun-oracle-v13",
			wantApplied:     true,
		},
		{
			name: "latest compatible baseline wins",
			appliedVersions: []string{
				"2026-08-26-bojun-oracle-v13",
				"2026-09-01-office-message-v14",
				"2026-09-01-office-message-ha-v15",
				"2026-09-01-office-message-compat-v16",
				"2026-09-01-office-message-bot-v17",
				"2026-09-01-office-message-file-v18",
				"2026-09-01-office-message-schedule-v19",
				"2026-09-01-access-permission-v20",
				"2026-09-04-report-category-access-v21",
			},
			wantVersion: "2026-09-04-report-category-access-v21",
			wantApplied: true,
		},
		{
			name:            "v11 remains unsupported",
			appliedVersions: []string{"2026-08-25-report-center-v11"},
		},
	}

	candidates := schemaIncrementalMigrationBaselines()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotVersion, gotApplied := preferredSchemaMigrationBaseline(candidates, test.appliedVersions)
			if gotVersion != test.wantVersion || gotApplied != test.wantApplied {
				t.Fatalf("preferredSchemaMigrationBaseline() = (%q, %t), want (%q, %t)", gotVersion, gotApplied, test.wantVersion, test.wantApplied)
			}
		})
	}
}

func TestIncrementalSchemaMigrationIncludesReportCategoryAccessTables(t *testing.T) {
	models := incrementalSchemaMigrationModels()
	want := []reflect.Type{
		reflect.TypeOf(&model.OfficeMessage{}),
		reflect.TypeOf(&model.OfficePushTarget{}),
		reflect.TypeOf(&model.OfficePushSchedule{}),
		reflect.TypeOf(&model.OfficePushRun{}),
		reflect.TypeOf(&model.OfficeProcedureExportLock{}),
		reflect.TypeOf(&model.ReportCategoryAccess{}),
		reflect.TypeOf(&model.ReportCategoryGrant{}),
	}
	if len(models) != len(want) {
		t.Fatalf("incrementalSchemaMigrationModels() count = %d, want %d", len(models), len(want))
	}
	for index, migrationModel := range models {
		if gotType := reflect.TypeOf(migrationModel); gotType != want[index] {
			t.Fatalf("incrementalSchemaMigrationModels()[%d] = %v, want %v", index, gotType, want[index])
		}
	}
}

func TestOfficeMessageLegacySourceMigrationContract(t *testing.T) {
	if legacyOfficeMessageSourceOracle != "ORACLE" {
		t.Fatalf("legacyOfficeMessageSourceOracle = %q", legacyOfficeMessageSourceOracle)
	}
	if model.OfficeMessageSourceOracleProcedure != "ORACLE_PROCEDURE" {
		t.Fatalf("OfficeMessageSourceOracleProcedure = %q", model.OfficeMessageSourceOracleProcedure)
	}
}

func TestOfficeMessageMigrationModelsAreLimitedToOfficeMessageTables(t *testing.T) {
	models := officeMessageMigrationModels()
	want := []reflect.Type{
		reflect.TypeOf(&model.OfficeMessage{}),
		reflect.TypeOf(&model.OfficePushTarget{}),
		reflect.TypeOf(&model.OfficePushSchedule{}),
		reflect.TypeOf(&model.OfficePushRun{}),
		reflect.TypeOf(&model.OfficeProcedureExportLock{}),
	}
	if len(models) != len(want) {
		t.Fatalf("officeMessageMigrationModels() count = %d, want %d", len(models), len(want))
	}
	for index, migrationModel := range models {
		if gotType := reflect.TypeOf(migrationModel); gotType != want[index] {
			t.Fatalf("officeMessageMigrationModels()[%d] = %v, want %v", index, gotType, want[index])
		}
	}
}

func TestBojunOracleMigrationModelsAreLimitedToBojunTables(t *testing.T) {
	models := bojunOracleMigrationModels()
	want := []reflect.Type{
		reflect.TypeOf(&model.BojunRetailOrder{}),
		reflect.TypeOf(&model.BojunOracleSyncState{}),
	}
	if len(models) != len(want) {
		t.Fatalf("bojunOracleMigrationModels() count = %d, want %d", len(models), len(want))
	}
	for index, migrationModel := range models {
		if gotType := reflect.TypeOf(migrationModel); gotType != want[index] {
			t.Fatalf("bojunOracleMigrationModels()[%d] = %v, want %v", index, gotType, want[index])
		}
	}
}

func TestPreferredSchemaMigrationBaseline(t *testing.T) {
	tests := []struct {
		name            string
		appliedVersions []string
		wantVersion     string
		wantApplied     bool
	}{
		{
			name:            "v12 direct upgrade",
			appliedVersions: []string{"2026-08-26-bojun-oracle-v12"},
			wantVersion:     "2026-08-26-bojun-oracle-v12",
			wantApplied:     true,
		},
		{
			name:            "v11 skips v12",
			appliedVersions: []string{"2026-08-25-report-center-v11"},
			wantVersion:     "2026-08-25-report-center-v11",
			wantApplied:     true,
		},
		{
			name: "latest compatible baseline wins",
			appliedVersions: []string{
				"2026-08-25-report-center-v11",
				"2026-08-26-bojun-oracle-v12",
			},
			wantVersion: "2026-08-26-bojun-oracle-v12",
			wantApplied: true,
		},
		{
			name:            "unsupported older version uses full migration",
			appliedVersions: []string{"2026-08-14-report-center-v10"},
		},
		{
			name: "fresh database uses full migration",
		},
	}

	candidates := bojunOracleIncrementalMigrationBaselines()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVersion, gotApplied := preferredSchemaMigrationBaseline(candidates, tt.appliedVersions)
			if gotVersion != tt.wantVersion || gotApplied != tt.wantApplied {
				t.Fatalf(
					"preferredSchemaMigrationBaseline() = (%q, %t), want (%q, %t)",
					gotVersion,
					gotApplied,
					tt.wantVersion,
					tt.wantApplied,
				)
			}
		})
	}
}

func TestRunPendingSchemaMigrationUsesIncrementalPathFromPreviousVersion(t *testing.T) {
	incrementalCalls, fullCalls := 0, 0
	err := runPendingSchemaMigration(true, func() error {
		incrementalCalls++
		return nil
	}, func() error {
		fullCalls++
		return nil
	})
	if err != nil || incrementalCalls != 1 || fullCalls != 0 {
		t.Fatalf("runPendingSchemaMigration() error=%v incremental=%d full=%d", err, incrementalCalls, fullCalls)
	}
}

func TestRunPendingSchemaMigrationUsesFullPathWithoutPreviousVersion(t *testing.T) {
	incrementalCalls, fullCalls := 0, 0
	err := runPendingSchemaMigration(false, func() error {
		incrementalCalls++
		return nil
	}, func() error {
		fullCalls++
		return nil
	})
	if err != nil || incrementalCalls != 0 || fullCalls != 1 {
		t.Fatalf("runPendingSchemaMigration() error=%v incremental=%d full=%d", err, incrementalCalls, fullCalls)
	}
}
