package data_svc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"
)

func TestReportVersionServiceBuildsSafeSummaryDiff(t *testing.T) {
	hash := func(char string) string {
		value := ""
		for range 64 {
			value += char
		}
		return value
	}
	store := &fakeVersionStore{items: map[uint]reportrepo.VersionSummary{
		11: {Version: model.ReportVersion{BaseModel: model.BaseModel{ID: 11}, VersionNumber: 1, Status: model.ReportVersionStatusPublished, ProcedureSignatureHash: hash("a"), ParameterSchemaHash: hash("b"), ResultSchemaHash: hash("c"), ExportSchemaHash: hash("d"), PermissionHash: hash("e"), ContractHash: hash("f")}, ParameterCount: 2, ColumnCount: 4, GrantCount: 1},
		12: {Version: model.ReportVersion{BaseModel: model.BaseModel{ID: 12}, VersionNumber: 2, Status: model.ReportVersionStatusPublished, ProcedureSignatureHash: hash("a"), ParameterSchemaHash: hash("9"), ResultSchemaHash: hash("8"), ExportSchemaHash: hash("7"), PermissionHash: hash("e"), ContractHash: hash("6")}, ParameterCount: 3, ColumnCount: 5, GrantCount: 1},
	}}
	result, err := NewReportVersionService(store).Diff(t.Context(), 17, 9, 11, 12)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if store.actor != 17 || store.definitionID != 9 || len(result.Sections) != 5 {
		t.Fatalf("scope=%d/%d result=%#v", store.actor, store.definitionID, result)
	}
	for _, section := range result.Sections {
		for _, change := range section.Changes {
			if before, ok := change.Before.(string); ok && len(strings.TrimSuffix(before, "…")) > 12 {
				t.Fatalf("diff leaked full hash: %q", before)
			}
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	publicJSON := string(encoded)
	for _, privateField := range []string{"publishedBy", "schemaValidatedAt", `"contractHash":`, `"parameterSchemaHash":`, `"procedureSignatureHash":`, `"resultSchemaHash":`, `"permissionHash":`, `"exportSchemaHash":`} {
		if strings.Contains(publicJSON, privateField) {
			t.Fatalf("diff response exposed private field %q: %s", privateField, publicJSON)
		}
	}
	if strings.Contains(publicJSON, hash("a")) || strings.Contains(publicJSON, hash("6")) {
		t.Fatalf("diff response exposed full contract hash: %s", publicJSON)
	}
}

type fakeVersionStore struct {
	items               map[uint]reportrepo.VersionSummary
	draft               *reportrepo.Draft
	actor, definitionID uint
}

func (store *fakeVersionStore) ListPublishedVersions(context.Context, uint, uint, reportrepo.VersionListQuery) (reportrepo.VersionPage, error) {
	return reportrepo.VersionPage{}, nil
}
func (store *fakeVersionStore) FindPublishedVersionSummary(_ context.Context, actor, definitionID, versionID uint) (reportrepo.VersionSummary, error) {
	store.actor, store.definitionID = actor, definitionID
	return store.items[versionID], nil
}
func (store *fakeVersionStore) FindPublishedVersion(context.Context, uint, uint, uint) (*reportrepo.Draft, error) {
	if store.draft == nil {
		return nil, reportrepo.ErrDraftNotFound
	}
	return store.draft, nil
}

func TestReportVersionServiceReturnsReadOnlyConfiguration(t *testing.T) {
	publishedAt := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	draft := publicationDraft()
	draft.Version.Status = model.ReportVersionStatusPublished
	draft.Version.PublishedAt = &publishedAt
	draft.Parameters[0].Sensitive = true
	draft.Parameters[0].DefaultValueJSON = model.JSONText(`"secret"`)
	result, err := NewReportVersionService(&fakeVersionStore{draft: draft}).Get(t.Context(), 17, 9, 23)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result.Summary.ID != 23 || result.Summary.Version != 3 || result.Configuration.DatasourceID != 4 || len(result.Configuration.Columns) != 1 {
		t.Fatalf("detail = %#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, forbidden := range []string{"compiledSpec", "schemaProbeToken", "publishedBy", "secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("detail leaked %q: %s", forbidden, encoded)
		}
	}
}
