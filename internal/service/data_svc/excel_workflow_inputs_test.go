package data_svc

import (
	"errors"
	"reflect"
	"testing"
)

func TestExcelWorkflowSourcesKeepExplicitStepAssociation(t *testing.T) {
	input := ExcelWorkflowConfig{SourceFiles: []string{"untrusted.xlsx"}, Steps: []ExcelWorkflowStep{
		{ID: "match_a", Operation: excelOperationExportMatch, SchemeID: 1, Source: "upload", SourceFiles: []string{"untrusted.xlsx"}},
		{ID: "match_b", Operation: excelOperationExportMatch, SchemeID: 2, Source: "upload"},
		{ID: "write", Operation: excelOperationImportUpdate, SchemeID: 3, Source: "previous", SourceStepID: "match_a"},
		{ID: "import", Operation: ExcelOperationOracleImport, SchemeID: 4, Source: "upload"},
	}}
	config, err := validateExcelWorkflow(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.SourceFiles) != 0 || len(config.Steps[0].SourceFiles) != 0 || config.Steps[2].SourceStepID != "match_a" {
		t.Fatalf("client paths accepted or source association changed: %+v", config)
	}
	var resolved []string
	config, sources, err := prepareExcelWorkflowUploads(config, []string{"must_not_be_used"}, map[string][]string{
		"match_a": {"match-a-file"}, "match_b": {"match-b-file"}, "import": {"oracle-file", "match-a-file"},
	}, func(id string) (excelJobSource, error) {
		resolved = append(resolved, id)
		return excelJobSource{fileName: id}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved, []string{"match-a-file", "match-b-file", "oracle-file"}) || len(sources) != 3 {
		t.Fatalf("files mixed or unnecessarily copied: %v %+v", resolved, sources)
	}
	want := [][]string{{"source_001.xlsx"}, {"source_002.xlsx"}, nil, {"source_003.xlsx", "source_001.xlsx"}}
	for index, files := range want {
		if !reflect.DeepEqual(config.Steps[index].SourceFiles, files) {
			t.Fatalf("step %d sources=%v want=%v", index, config.Steps[index].SourceFiles, files)
		}
	}
	if input.Steps[0].SourceFiles[0] != "untrusted.xlsx" {
		t.Fatal("validation mutated caller config")
	}
}

func TestExcelWorkflowUploadBindingsRejectMissingAndUnrelatedFiles(t *testing.T) {
	config := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{
		{ID: "match", Operation: excelOperationExportMatch, Source: "upload"},
		{ID: "oracle", Operation: ExcelOperationOracleImport, Source: "upload"},
		{ID: "procedure", Operation: ExcelOperationOracleProcedure},
	}}
	for _, test := range []struct {
		name        string
		uploads     map[string][]string
		invalidFile bool
	}{
		{name: "missing step file", uploads: map[string][]string{"match": {"a"}}},
		{name: "empty new upload map must not reuse legacy", uploads: map[string][]string{}},
		{name: "unknown step", uploads: map[string][]string{"unknown": {"a"}}},
		{name: "database step cannot have files", uploads: map[string][]string{"procedure": {"a"}}},
		{name: "duplicate within a step", uploads: map[string][]string{"match": {"a", "a"}, "oracle": {"b"}}},
		{name: "expired upload", uploads: map[string][]string{"match": {"a"}, "oracle": {"b"}}, invalidFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := prepareExcelWorkflowUploads(config, []string{"legacy"}, test.uploads, func(id string) (excelJobSource, error) {
				if test.invalidFile {
					return excelJobSource{}, errors.New("expired")
				}
				return excelJobSource{fileName: id}, nil
			})
			if err == nil {
				t.Fatal("accepted invalid bindings")
			}
		})
	}
}

func TestExcelWorkflowLegacyInputsGetStableIDsAndExplicitPrevious(t *testing.T) {
	config, err := validateExcelWorkflow(workflowTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if config.Steps[0].ID == "" || config.Steps[1].SourceStepID != config.Steps[0].ID {
		t.Fatalf("legacy association=%+v", config.Steps)
	}
	config, sources, err := prepareExcelWorkflowUploads(config, []string{"legacy-file"}, nil, func(id string) (excelJobSource, error) { return excelJobSource{fileName: id}, nil })
	if err != nil || len(sources) != 1 || !reflect.DeepEqual(config.Steps[0].SourceFiles, []string{"source_001.xlsx"}) {
		t.Fatalf("legacy config=%+v sources=%v err=%v", config, sources, err)
	}
}

func TestExcelWorkflowRejectsDeletedForwardOrDuplicateStepReferences(t *testing.T) {
	for _, test := range []struct {
		name  string
		steps []ExcelWorkflowStep
	}{
		{name: "future matching step", steps: []ExcelWorkflowStep{{ID: "write", Operation: excelOperationImportUpdate, SchemeID: 1, Source: "previous", SourceStepID: "match"}, {ID: "match", Operation: excelOperationExportMatch, SchemeID: 2}}},
		{name: "deleted step", steps: []ExcelWorkflowStep{{ID: "match", Operation: excelOperationExportMatch, SchemeID: 1}, {ID: "oracle", Operation: ExcelOperationOracleImport, SchemeID: 2, Source: "previous", SourceStepID: "deleted"}}},
		{name: "nonmatching source", steps: []ExcelWorkflowStep{{ID: "import", Operation: ExcelOperationOracleImport, SchemeID: 1}, {ID: "write", Operation: excelOperationImportUpdate, SchemeID: 2, Source: "previous", SourceStepID: "import"}}},
		{name: "duplicate ids", steps: []ExcelWorkflowStep{{ID: "same", Operation: excelOperationExportMatch, SchemeID: 1}, {ID: "same", Operation: ExcelOperationOracleImport, SchemeID: 2}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateExcelWorkflow(ExcelWorkflowConfig{Steps: test.steps}); err == nil {
				t.Fatal("accepted ambiguous source")
			}
		})
	}
}
