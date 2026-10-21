package data_svc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"os"
	"strconv"
	"strings"
	"time"
)

type ReportBojunCatalogService struct{ repo *reportrepo.Repository }

func NewReportBojunCatalogService() *ReportBojunCatalogService {
	return &ReportBojunCatalogService{repo: reportrepo.New()}
}
func bojunNamespace() reportoracle.BojunNamespace {
	owner, link := os.Getenv("REPORT_BOJUN_OWNER"), os.Getenv("REPORT_BOJUN_DB_LINK")
	if owner == "" {
		owner = "BOSNDS3"
	}
	if link == "" {
		link = "Link_To_Bojun"
	}
	return reportoracle.BojunNamespace{Owner: owner, DBLink: link}
}

type bojunCatalogOracleLease struct {
	*reportoracle.Adapter
	release func()
}

func (l *bojunCatalogOracleLease) Close() error { l.release(); return nil }
func (s *ReportBojunCatalogService) open(ctx context.Context, ds uint) (*bojunCatalogOracleLease, error) {
	d, e := s.repo.GetReportDatasource(ctx, ds)
	if e != nil || !d.Enabled {
		return nil, ErrReportDatasourceNotFound
	}
	password, e := (reportsecret.EnvironmentKeyring{}).Decrypt(d.CredentialKeyVersion, d.PasswordCiphertext)
	if e != nil {
		return nil, ErrReportDatasourceCredentialUnavailable
	}
	a, release, e := acquireSelectOracle(ctx, *d, password)
	if e != nil {
		return nil, ErrReportDatasourceOracleUnavailable
	}
	return &bojunCatalogOracleLease{Adapter: a, release: release}, nil
}
func (s *ReportBojunCatalogService) Start(ctx context.Context, ds uint, scope string, tableID uint) (*model.ReportBojunSyncRun, error) {
	if _, e := s.repo.GetReportDatasource(ctx, ds); e != nil {
		return nil, ErrReportDatasourceNotFound
	}
	if scope == "" {
		scope = "ALL"
	}
	if scope != "ALL" && scope != "TABLE" || scope == "TABLE" && tableID == 0 {
		return nil, invalidReport("invalid sync scope")
	}
	return s.repo.StartBojunSync(ctx, ds, scope, tableID)
}
func (s *ReportBojunCatalogService) SyncStatus(ctx context.Context, ds, id uint) (*model.ReportBojunSyncRun, error) {
	return s.repo.GetBojunSync(ctx, ds, id)
}
func (s *ReportBojunCatalogService) ProcessSync(ctx context.Context, id uint) error {
	// The datasource row serializes catalog publications across all worker replicas.
	run, e := s.repo.GetBojunSyncByID(ctx, id)
	if e != nil {
		return e
	}
	if run.Status == "SUCCEEDED" {
		return nil
	}
	if e = s.repo.MarkBojunSync(ctx, id, "RUNNING", ""); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Minute)
	defer cancel()
	a, e := s.open(ctx, run.DatasourceID)
	if e == nil {
		defer a.Close()
		if run.Scope == "TABLE" {
			_, e = s.refresh(ctx, a.Adapter, run.DatasourceID, run.TableID)
		} else {
			e = s.repo.ResetBojunStaging(ctx, id)
			after := "0"
			for e == nil {
				var page []reportoracle.BojunTableMetadata
				page, e = a.ListBojunMetadata(ctx, bojunNamespace(), after, 200)
				if e != nil {
					break
				}
				if e = s.repo.StageBojunTables(ctx, id, page); e != nil {
					break
				}
				if len(page) < 200 {
					break
				}
				after = page[len(page)-1].ID
			}
			if e == nil {
				e = s.repo.FinishBojunSync(ctx, *run, bojunNamespace())
			}
		}
	}
	if e != nil {
		stateCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		_ = s.repo.MarkBojunSync(stateCtx, id, "FAILED", "字典同步失败，请检查数据源与远程字典权限")
		return fmt.Errorf("bojun catalog sync failed: %w", e)
	}
	if run.Scope == "TABLE" {
		return s.repo.MarkBojunSync(ctx, id, "SUCCEEDED", "")
	}
	return nil
}

type BojunTableDTO struct {
	reportdataset.Table
	ResolvedTable string     `json:"resolvedTable"`
	Enabled       bool       `json:"enabled"`
	LockVersion   uint64     `json:"lockVersion"`
	LastSyncedAt  *time.Time `json:"lastSyncedAt"`
}
type BojunTablePage struct {
	Items      []BojunTableDTO `json:"items"`
	HasMore    bool            `json:"hasMore"`
	NextAfter  string          `json:"nextAfter"`
	Generation uint64          `json:"generation"`
}

func catalogCursor(generation uint64, id uint) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", generation, id)))
}
func (s *ReportBojunCatalogService) List(ctx context.Context, ds uint, search, status, after string, limit int, availableOnly bool) (BojunTablePage, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var afterID uint
	var generation uint64
	if after != "" {
		b, e := base64.RawURLEncoding.DecodeString(after)
		if e != nil {
			return BojunTablePage{}, invalidReport("invalid catalog cursor")
		}
		if _, e = fmt.Sscanf(string(b), "%d:%d", &generation, &afterID); e != nil {
			return BojunTablePage{}, invalidReport("invalid catalog cursor")
		}
	}
	list, g, e := s.repo.ListBojunTables(ctx, ds, afterID, limit, strings.TrimSpace(search), status, availableOnly)
	if e != nil {
		return BojunTablePage{}, e
	}
	if after != "" && generation != g {
		return BojunTablePage{}, reportrepo.ErrDraftVersionConflict
	}
	out := BojunTablePage{Items: []BojunTableDTO{}, Generation: g, HasMore: len(list) > limit}
	if out.HasMore {
		list = list[:limit]
	}
	for _, t := range list {
		dto, e := s.dto(ctx, t)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, dto)
	}
	if out.HasMore {
		out.NextAfter = catalogCursor(g, list[len(list)-1].ID)
	}
	return out, nil
}
func (s *ReportBojunCatalogService) dto(ctx context.Context, t model.ReportBojunTable) (BojunTableDTO, error) {
	meta := reportdataset.Table{ID: strconv.FormatUint(uint64(t.ID), 10), BojunTableID: t.BojunTableID, LogicalName: t.LogicalName, Description: t.Description, Owner: t.ResolvedOwner, PhysicalTable: t.ResolvedTable, DBLink: t.DBLink, Columns: []reportdataset.Column{}, PrimaryKeys: []string{}, DisplayFields: []string{}, SearchFields: []string{}, UniqueKeys: [][]string{}}
	if len(t.MetadataJSON) > 0 {
		if e := json.Unmarshal([]byte(t.MetadataJSON), &meta); e != nil {
			return BojunTableDTO{}, e
		}
	}
	meta.ValidationStatus = t.ValidationStatus
	meta.ValidationReason = t.ValidationReason
	config, e := s.repo.GetBojunConfig(ctx, t.ID)
	if e != nil {
		return BojunTableDTO{}, e
	}
	if len(config.ConfigJSON) > 0 {
		if e = json.Unmarshal([]byte(config.ConfigJSON), &meta.Config); e != nil {
			return BojunTableDTO{}, e
		}
	}
	if meta.Config.PrimaryKeys != nil {
		meta.PrimaryKeys = *meta.Config.PrimaryKeys
	}
	if meta.Config.DisplayFields != nil {
		meta.DisplayFields = *meta.Config.DisplayFields
	}
	if meta.Config.SearchFields != nil {
		meta.SearchFields = *meta.Config.SearchFields
	}
	if config.LockVersion == 0 {
		config.LockVersion = 1
	}
	return BojunTableDTO{Table: meta, ResolvedTable: t.ResolvedTable, Enabled: meta.Config.Enabled, LockVersion: config.LockVersion, LastSyncedAt: t.LastSyncedAt}, nil
}
func (s *ReportBojunCatalogService) refresh(ctx context.Context, a *reportoracle.Adapter, ds, id uint) (BojunTableDTO, error) {
	record, e := s.repo.GetBojunTable(ctx, ds, id)
	if e != nil {
		return BojunTableDTO{}, e
	}
	all, e := s.repo.BojunSourceTables(ctx, ds)
	if e != nil {
		return BojunTableDTO{}, e
	}
	meta, e := a.InspectBojunTable(ctx, bojunNamespace(), all[record.BojunTableID], all)
	if e != nil {
		return BojunTableDTO{}, ErrReportDatasourceOracleUnavailable
	}
	if meta.ValidationStatus == "" {
		meta.ValidationStatus = "UNSUPPORTED"
	}
	if e = s.repo.SaveBojunMetadata(ctx, record, meta); e != nil {
		return BojunTableDTO{}, e
	}
	record, e = s.repo.GetBojunTable(ctx, ds, id)
	if e != nil {
		return BojunTableDTO{}, e
	}
	return s.dto(ctx, *record)
}
func (s *ReportBojunCatalogService) Get(ctx context.Context, ds, id uint, refresh bool) (BojunTableDTO, error) {
	t, e := s.repo.GetBojunTable(ctx, ds, id)
	if e != nil {
		return BojunTableDTO{}, e
	}
	if refresh || len(t.MetadataJSON) == 0 || t.ValidationStatus == "PENDING" {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		a, e := s.open(ctx, ds)
		if e != nil {
			return BojunTableDTO{}, e
		}
		defer a.Close()
		return s.refresh(ctx, a.Adapter, ds, id)
	}
	return s.dto(ctx, *t)
}
func (s *ReportBojunCatalogService) Configure(ctx context.Context, actor, ds, id uint, expected uint64, config reportdataset.TableConfig) (BojunTableDTO, error) {
	t, e := s.Get(ctx, ds, id, false)
	if e != nil {
		return t, e
	}
	valid := map[string]bool{}
	for _, c := range t.Columns {
		valid[c.ID] = c.IsPhysical && c.IsSelectable
	}
	for _, ids := range []*[]string{config.PrimaryKeys, config.DisplayFields, config.SearchFields, config.SelectableColumns, config.DetailColumns} {
		if ids != nil {
			for _, id := range *ids {
				if !valid[id] {
					return t, invalidReport("REPORT_METADATA_UNSUPPORTED: configured column is unavailable")
				}
			}
		}
	}
	if config.Enabled {
		if t.ValidationStatus != "VALID" {
			return t, invalidReport("table is not executable")
		}
		if config.DataScope.Mode != "GLOBAL_APPROVED" && config.DataScope.Mode != "SCOPE_COLUMN" {
			return t, invalidReport("explicit data scope approval is required")
		}
		if config.DataScope.Mode == "SCOPE_COLUMN" && !valid[config.DataScope.ColumnID] {
			return t, invalidReport("invalid scope column")
		}
	}
	if e = s.repo.SaveBojunConfig(ctx, actor, id, expected, config); e != nil {
		return t, e
	}
	return s.Get(ctx, ds, id, false)
}
func (s *ReportBojunCatalogService) LoadTables(ctx context.Context, ds uint, ids []string) (map[string]reportdataset.Table, error) {
	out := map[string]reportdataset.Table{}
	for _, id := range ids {
		if _, ok := out[id]; ok {
			continue
		}
		v, e := strconv.ParseUint(id, 10, 64)
		if e != nil || v == 0 {
			return nil, invalidReport("invalid catalog table id")
		}
		t, e := s.Get(ctx, ds, uint(v), false)
		if e != nil {
			return nil, e
		}
		out[id] = t.Table
	}
	return out, nil
}
