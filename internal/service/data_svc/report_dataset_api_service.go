package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"time"
)

type ReportDatasetService struct{ repo *reportrepo.Repository }

func NewReportDatasetService() *ReportDatasetService {
	return &ReportDatasetService{repo: reportrepo.New()}
}

type ReportQueryIssue struct {
	Code      string `json:"code"`
	FieldPath string `json:"fieldPath"`
	Message   string `json:"message"`
}
type ReportQueryValidation struct {
	Valid    bool               `json:"valid"`
	Errors   []ReportQueryIssue `json:"errors"`
	Warnings []ReportQueryIssue `json:"warnings"`
}

func (s *ReportDatasetService) draft(ctx context.Context, actor, id uint, lock uint64) (*reportrepo.Draft, error) {
	d, e := s.repo.FindDraftByID(ctx, actor, id)
	if e != nil {
		return nil, classifyReportStoreError(e)
	}
	if d.LockVersion != lock {
		return nil, ErrReportConflict
	}
	if d.Version.ExecutionMode != model.ReportExecutionModeSelectSnapshot {
		return nil, invalidReport("only SELECT reports support query validation")
	}
	if !selectReportsEnabled() {
		return nil, invalidReport("自定义查询功能尚未启用")
	}
	return d, nil
}
func (s *ReportDatasetService) Validate(ctx context.Context, actor, id uint, lock uint64) (ReportQueryValidation, error) {
	d, e := s.draft(ctx, actor, id, lock)
	if e != nil {
		return ReportQueryValidation{}, e
	}
	out := ReportQueryValidation{Errors: []ReportQueryIssue{}, Warnings: []ReportQueryIssue{}}
	_, e = compileSelectDraft(ctx, d)
	if e != nil {
		out.Errors = append(out.Errors, ReportQueryIssue{Code: "REPORT_QUERY_INVALID", FieldPath: "querySpec", Message: safeQueryIssue(e)})
		return out, nil
	}
	out.Valid = true
	return out, nil
}
func safeQueryIssue(e error) string {
	if e == nil {
		return ""
	}
	message := e.Error()
	for _, prefix := range []string{"REPORT_QUERY_INVALID:", "REPORT_RELATION_NOT_UNIQUE:", "REPORT_METADATA_UNSUPPORTED:"} {
		if at := strings.Index(message, prefix); at >= 0 {
			return message[at:]
		}
	}
	return "查询定义或目录配置不完整，请核对字段授权、输入参数与数据源"
}

type ReportQueryPreview struct {
	SampleOnly bool                    `json:"sampleOnly"`
	ElapsedMS  int64                   `json:"elapsedMs"`
	Columns    []ReportResultColumnDTO `json:"columns"`
	Rows       []ReportResultRowDTO    `json:"rows"`
}

func (s *ReportDatasetService) Preview(ctx context.Context, actor, id uint, lock uint64, conditions map[string]json.RawMessage, inputs reportdataset.QueryInputs) (ReportQueryPreview, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d, e := s.draft(ctx, actor, id, lock)
	if e != nil {
		return ReportQueryPreview{}, e
	}
	c, e := compileSelectDraft(ctx, d)
	if e != nil {
		return ReportQueryPreview{}, e
	}
	c, _, e = selectAuthorization(ctx, d.Version.DatasourceID, c, actor)
	if e != nil {
		return ReportQueryPreview{}, e
	}
	parameters, e := normalizeSelectConditions(d.Version.InputSchemaJSON, conditions)
	if e != nil {
		return ReportQueryPreview{}, e
	}
	var values map[string]json.RawMessage
	_ = json.Unmarshal(parameters, &values)
	plan, e := c.Build(values, inputs, strconv.FormatUint(uint64(actor), 10), false)
	if e != nil {
		return ReportQueryPreview{}, invalidReport(e.Error())
	}
	release, e := s.repo.AcquireSelectDatasourceSlot(ctx, d.Version.DatasourceID)
	if e != nil {
		return ReportQueryPreview{}, ErrReportRunBusy
	}
	defer release()
	ds, e := s.repo.GetReportDatasource(ctx, d.Version.DatasourceID)
	if e != nil {
		return ReportQueryPreview{}, e
	}
	password, e := (reportsecret.EnvironmentKeyring{}).Decrypt(ds.CredentialKeyVersion, ds.PasswordCiphertext)
	if e != nil {
		return ReportQueryPreview{}, ErrReportDatasourceCredentialUnavailable
	}
	a, done, e := acquireSelectOracle(ctx, *ds, password)
	if e != nil {
		return ReportQueryPreview{}, fmt.Errorf("%w: acquire preview source: %w", ErrReportDatasourceOracleUnavailable, e)
	}
	defer done()
	if e = inspectSelectMetadata(ctx, a, c); e != nil {
		return ReportQueryPreview{}, e
	}
	_, rows, e := a.QuerySelectSample(ctx, plan, 100)
	if e != nil {
		return ReportQueryPreview{}, fmt.Errorf("%w: query preview sample: %w", ErrReportDatasourceOracleUnavailable, e)
	}
	out := ReportQueryPreview{SampleOnly: true, ElapsedMS: time.Since(start).Milliseconds(), Columns: selectResultColumns(c), Rows: []ReportResultRowDTO{}}
	for i, row := range rows {
		row = c.Materialize(row)
		dto := selectResultRow(c, row, fmt.Sprintf("sample-%d", i+1))
		for key, state := range dto.RelationStates {
			state.CanViewDetail = false
			dto.RelationStates[key] = state
		}
		out.Rows = append(out.Rows, dto)
	}
	s.audit(ctx, actor, "REPORT_QUERY_PREVIEW", "REPORT_DEFINITION", id, map[string]any{"rowCount": len(rows), "elapsedMs": out.ElapsedMS})
	return out, nil
}
func selectCursorKey() ([]byte, error) {
	key := config.GetString("cfg.jwt.key")
	if strings.TrimSpace(key) == "" {
		return nil, ErrReportRunResultTemporary
	}
	sum := sha256.Sum256([]byte("report-result-cursor\x1f" + key))
	return sum[:], nil
}
func (s *ReportDatasetService) audit(ctx context.Context, actor uint, action, target string, id uint, detail map[string]any) {
	raw, _ := json.Marshal(detail)
	_ = s.repo.WriteReportAudit(ctx, model.ReportAudit{ActorUserID: actor, Action: action, TargetType: target, TargetID: id, RequestID: uuid.NewString(), DetailJSON: model.JSONText(raw)})
}
