package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

var (
	ErrOpenTmallForbidden    = errors.New("open tmall: forbidden")
	ErrOpenTmallInvalidQuery = errors.New("open tmall: invalid query")
	ErrOpenTmallUnavailable  = errors.New("open tmall: unavailable")
)

const openTmallQueryTimeout = 10 * time.Second

type openTmallQuery func(context.Context, reportoracle.TmallDataset, reportoracle.TmallQuery) ([]reportoracle.TmallRow, error)

type OpenTmallQueryService struct {
	query       openTmallQuery
	permissions openBojunQueryPermissionReader
	now         func() time.Time
}

type OpenTmallQueryDTO struct {
	Items      []reportoracle.TmallRow  `json:"items"`
	Pagination OpenBojunQueryPagination `json:"pagination"`
}

func NewOpenTmallQueryService() *OpenTmallQueryService {
	return newOpenTmallQueryService(func(ctx context.Context, dataset reportoracle.TmallDataset, query reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
		var rows []reportoracle.TmallRow
		// Reuse the environment Oracle pool, configuration rotation and release lifecycle.
		err := withOracleOperationConnection(ctx, openTmallQueryTimeout, func(queryCtx context.Context, adapter *reportoracle.Adapter) error {
			var err error
			rows, err = adapter.QueryTmall(queryCtx, dataset, query)
			return err
		})
		return rows, err
	}, newAccountPermissionChecker(database.DB), time.Now)
}

func newOpenTmallQueryService(query openTmallQuery, permissions openBojunQueryPermissionReader, now func() time.Time) *OpenTmallQueryService {
	if query == nil || permissions == nil || now == nil {
		panic("open tmall service: nil dependency")
	}
	return &OpenTmallQueryService{query: query, permissions: permissions, now: now}
}

func (service *OpenTmallQueryService) Query(ctx context.Context, actor uint, dataset reportoracle.TmallDataset, request requestbody.OpenTmallQueryRequest) (*OpenTmallQueryDTO, error) {
	if ctx == nil {
		return nil, ErrOpenTmallInvalidQuery
	}
	permission := ""
	switch dataset {
	case reportoracle.TmallSycm, reportoracle.TmallSycmHourly:
		permission = model.PermissionTmallSycmRead
	case reportoracle.TmallQztg, reportoracle.TmallQztgHourly:
		permission = model.PermissionTmallQztgRead
	case reportoracle.TmallOrders:
		permission = model.PermissionTmallOrdersRead
	case reportoracle.TmallTot:
		permission = model.PermissionTmallTotRead
	default:
		return nil, ErrOpenTmallInvalidQuery
	}
	if actor == 0 {
		return nil, ErrOpenTmallForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actor, permission, service.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %w", ErrOpenTmallUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenTmallForbidden
	}
	pagination, err := normalizeOpenBojunQueryPagination(request.Page, request.PageSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenTmallInvalidQuery, err)
	}
	query, err := reportoracle.NormalizeTmallQuery(dataset, reportoracle.TmallQuery{
		StartDate: request.StartDate, EndDate: request.EndDate, BatchNo: request.BatchNo, Filters: request.Filters,
		StartTime: request.StartTime, EndTime: request.EndTime, TimeField: request.TimeField,
		Offset: (pagination.Page - 1) * pagination.PageSize, PageSize: pagination.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenTmallInvalidQuery, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, openTmallQueryTimeout)
	defer cancel()
	rows, err := service.query(queryCtx, dataset, query)
	if err != nil {
		return nil, fmt.Errorf("%w: query: %w", ErrOpenTmallUnavailable, err)
	}
	pagination.HasMore = len(rows) > pagination.PageSize
	if pagination.HasMore {
		rows = rows[:pagination.PageSize]
	}
	if rows == nil {
		rows = []reportoracle.TmallRow{}
	}
	for _, row := range rows {
		delete(row, "ITEM_NO")
	}
	return &OpenTmallQueryDTO{Items: rows, Pagination: pagination}, nil
}
