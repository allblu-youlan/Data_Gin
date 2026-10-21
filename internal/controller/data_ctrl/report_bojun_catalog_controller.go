package data_ctrl

import (
	"errors"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type ReportBojunCatalogController struct {
	service *data_svc.ReportBojunCatalogService
}

func NewReportBojunCatalogController() *ReportBojunCatalogController {
	return &ReportBojunCatalogController{service: data_svc.NewReportBojunCatalogService()}
}
func writeBojunError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, gorm.ErrRecordNotFound), errors.Is(e, data_svc.ErrReportDatasourceNotFound):
		responses.New(c).ToSafeErrorResponse(errcode.NotFound, "目录记录不存在")
	case errors.Is(e, reportrepo.ErrDraftVersionConflict):
		responses.New(c).ToSafeErrorResponse(errcode.Conflict, "目录或配置已变化，请刷新重试")
	case errors.Is(e, data_svc.ErrReportInvalid):
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, e.Error())
	case errors.Is(e, data_svc.ErrReportDatasourceOracleUnavailable), errors.Is(e, data_svc.ErrReportDatasourceCredentialUnavailable):
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "伯俊数据源暂时不可用")
	default:
		writeReportError(c, e)
	}
}
func bojunIDs(c *gin.Context) (uint, uint, error) {
	ds, e := parseReportUint(c.Param("id"), "datasource id")
	if e != nil {
		return 0, 0, e
	}
	var id uint
	if c.Param("tableId") != "" {
		id, e = parseReportUint(c.Param("tableId"), "table id")
	}
	return ds, id, e
}
func (h *ReportBojunCatalogController) Start(c *gin.Context) {
	ds, _, e := bojunIDs(c)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	var req struct {
		Scope   string `json:"scope"`
		TableID uint   `json:"tableId"`
	}
	if e = decodeMallJSON(c, &req); e != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "同步参数无效")
		return
	}
	v, e := h.service.Start(c.Request.Context(), ds, req.Scope, req.TableID)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusAccepted, map[string]any{"syncRunId": v.ID, "status": v.Status})
}
func (h *ReportBojunCatalogController) Status(c *gin.Context) {
	ds, _, e := bojunIDs(c)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	id, e := parseReportUint(c.Param("syncId"), "sync id")
	if e != nil {
		writeBojunError(c, e)
		return
	}
	v, e := h.service.SyncStatus(c.Request.Context(), ds, id)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
func (h *ReportBojunCatalogController) List(c *gin.Context) {
	ds, _, e := bojunIDs(c)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	v, e := h.service.List(c.Request.Context(), ds, c.Query("search"), c.Query("status"), c.Query("after"), limit, c.Query("availableOnly") == "true")
	if e != nil {
		writeBojunError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
func (h *ReportBojunCatalogController) Get(c *gin.Context) {
	ds, id, e := bojunIDs(c)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	v, e := h.service.Get(c.Request.Context(), ds, id, false)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
func (h *ReportBojunCatalogController) Columns(c *gin.Context) {
	ds, id, e := bojunIDs(c)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	t, e := h.service.Get(c.Request.Context(), ds, id, false)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	after := c.Query("after")
	search := strings.ToUpper(c.Query("search"))
	items := []reportdataset.Column{}
	sort.Slice(t.Columns, func(i, j int) bool {
		a, _ := strconv.ParseUint(t.Columns[i].ID, 10, 64)
		b, _ := strconv.ParseUint(t.Columns[j].ID, 10, 64)
		return a < b
	})
	afterID, _ := strconv.ParseUint(after, 10, 64)
	for _, col := range t.Columns {
		columnID, _ := strconv.ParseUint(col.ID, 10, 64)
		if after != "" && columnID <= afterID {
			continue
		}
		if c.Query("selectableOnly") == "true" && !col.IsSelectable {
			continue
		}
		if search != "" && !strings.Contains(strings.ToUpper(col.Name+col.DBName+col.Description), search) {
			continue
		}
		items = append(items, col)
	}
	more := len(items) > limit
	next := ""
	if more {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	responses.New(c).ToResponse(map[string]any{"items": items, "hasMore": more, "nextAfter": next})
}
func (h *ReportBojunCatalogController) Configure(c *gin.Context) {
	ds, id, e := bojunIDs(c)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	var req struct {
		ExpectedLockVersion uint64 `json:"expectedLockVersion"`
		reportdataset.TableConfig
		Config *reportdataset.TableConfig `json:"config,omitempty"`
	}
	if e = decodeMallJSON(c, &req); e != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "配置参数无效")
		return
	}
	config := req.TableConfig
	if req.Config != nil {
		config = *req.Config
	}
	v, e := h.service.Configure(c.Request.Context(), auth.CurrentUserID(c), ds, id, req.ExpectedLockVersion, config)
	if e != nil {
		writeBojunError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
