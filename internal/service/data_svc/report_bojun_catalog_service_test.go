package data_svc

import (
	"context"
	"errors"
	"testing"

	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

func TestBojunReferenceMapsDictionaryIDToLocalTableInSameDatasource(t *testing.T) {
	var loaded []uint
	ctx := t.Context()
	result, err := resolveBojunReferenceTable(ctx, 7, 12, 31,
		func(received context.Context, ds, tableID uint, refresh bool) (BojunTableDTO, error) {
			if received != ctx || ds != 7 || refresh {
				t.Fatalf("detail context = %v, datasource = %d, refresh = %v", received, ds, refresh)
			}
			loaded = append(loaded, tableID)
			if tableID == 12 {
				return BojunTableDTO{Table: reportdataset.Table{Columns: []reportdataset.Column{{ID: "31", RefTableID: "00012345678901234567890"}}}}, nil
			}
			if tableID != 42 {
				t.Fatalf("loaded source dictionary ID as local table ID: %d", tableID)
			}
			return BojunTableDTO{Table: reportdataset.Table{ID: "42", LogicalName: "C_STORE", ValidationStatus: "VALID"}, Enabled: true}, nil
		},
		func(received context.Context, ds uint, sourceID string) (*model.ReportBojunTable, error) {
			if received != ctx || ds != 7 || sourceID != "00012345678901234567890" {
				t.Fatalf("reference lookup = %v, %d, %q", received, ds, sourceID)
			}
			return &model.ReportBojunTable{BaseModel: model.BaseModel{ID: 42}}, nil
		})
	if err != nil || result.ID != "42" || result.LogicalName != "C_STORE" || len(loaded) != 2 {
		t.Fatalf("reference result = %#v, error = %v, loaded = %v", result, err, loaded)
	}
}

func TestBojunReferenceRejectsForeignColumnOrUnlinkedFieldBeforeTargetRead(t *testing.T) {
	for _, test := range []struct {
		name     string
		columnID uint
		want     error
	}{
		{name: "column belongs to another table", columnID: 32, want: gorm.ErrRecordNotFound},
		{name: "field has no reference", columnID: 31, want: ErrReportInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveBojunReferenceTable(t.Context(), 7, 12, test.columnID,
				func(context.Context, uint, uint, bool) (BojunTableDTO, error) {
					return BojunTableDTO{Table: reportdataset.Table{Columns: []reportdataset.Column{{ID: "31", BojunColumnID: "32"}}}}, nil
				},
				func(context.Context, uint, string) (*model.ReportBojunTable, error) {
					t.Fatal("target read before checking source field ownership/reference")
					return nil, nil
				})
			if !errors.Is(err, test.want) {
				t.Fatalf("reference error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestBojunReferenceRejectsMissingInactiveDisabledOrUnsupportedTarget(t *testing.T) {
	for _, test := range []struct {
		name      string
		lookupErr error
		enabled   bool
		status    string
		want      error
	}{
		{name: "missing or inactive", lookupErr: gorm.ErrRecordNotFound, want: gorm.ErrRecordNotFound},
		{name: "disabled", status: "VALID", want: ErrReportInvalid},
		{name: "unsupported", enabled: true, status: "UNSUPPORTED", want: ErrReportInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveBojunReferenceTable(t.Context(), 7, 12, 31,
				func(_ context.Context, _, tableID uint, _ bool) (BojunTableDTO, error) {
					if tableID == 12 {
						return BojunTableDTO{Table: reportdataset.Table{Columns: []reportdataset.Column{{ID: "31", RefTableID: "9001"}}}}, nil
					}
					return BojunTableDTO{Table: reportdataset.Table{ID: "42", ValidationStatus: test.status}, Enabled: test.enabled}, nil
				},
				func(context.Context, uint, string) (*model.ReportBojunTable, error) {
					return &model.ReportBojunTable{BaseModel: model.BaseModel{ID: 42}}, test.lookupErr
				})
			if !errors.Is(err, test.want) {
				t.Fatalf("reference error = %v, want %v", err, test.want)
			}
		})
	}
}
