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
	ErrOpenFeishuAttendanceForbidden    = errors.New("open feishu attendance: forbidden")
	ErrOpenFeishuAttendanceInvalidQuery = errors.New("open feishu attendance: invalid query")
	ErrOpenFeishuAttendanceUnavailable  = errors.New("open feishu attendance: unavailable")
)

const openFeishuAttendanceQueryTimeout = 10 * time.Second

type openFeishuAttendanceQuery func(context.Context, reportoracle.FeishuAttendanceDataset, reportoracle.FeishuAttendanceQuery) ([]reportoracle.FeishuAttendanceRow, error)

type OpenFeishuAttendanceQueryService struct {
	query       openFeishuAttendanceQuery
	permissions openBojunQueryPermissionReader
	now         func() time.Time
}

type OpenFeishuAttendanceQueryDTO struct {
	Items      []reportoracle.FeishuAttendanceRow `json:"items"`
	Pagination OpenBojunQueryPagination           `json:"pagination"`
}

func NewOpenFeishuAttendanceQueryService() *OpenFeishuAttendanceQueryService {
	return newOpenFeishuAttendanceQueryService(func(ctx context.Context, dataset reportoracle.FeishuAttendanceDataset, query reportoracle.FeishuAttendanceQuery) ([]reportoracle.FeishuAttendanceRow, error) {
		var rows []reportoracle.FeishuAttendanceRow
		// Reuse the environment Oracle pool, configuration rotation and release lifecycle.
		err := withOracleOperationConnection(ctx, openFeishuAttendanceQueryTimeout, func(queryCtx context.Context, adapter *reportoracle.Adapter) error {
			var err error
			rows, err = adapter.QueryFeishuAttendance(queryCtx, dataset, query)
			return err
		})
		return rows, err
	}, newAccountPermissionChecker(database.DB), time.Now)
}

func newOpenFeishuAttendanceQueryService(query openFeishuAttendanceQuery, permissions openBojunQueryPermissionReader, now func() time.Time) *OpenFeishuAttendanceQueryService {
	if query == nil || permissions == nil || now == nil {
		panic("open feishu attendance service: nil dependency")
	}
	return &OpenFeishuAttendanceQueryService{query: query, permissions: permissions, now: now}
}

func (service *OpenFeishuAttendanceQueryService) Query(ctx context.Context, actor uint, dataset reportoracle.FeishuAttendanceDataset, request requestbody.OpenFeishuAttendanceQueryRequest) (*OpenFeishuAttendanceQueryDTO, error) {
	if ctx == nil {
		return nil, ErrOpenFeishuAttendanceInvalidQuery
	}
	permission := ""
	switch dataset {
	case reportoracle.FeishuAttendanceDaily:
		permission = model.PermissionFeishuAttendanceDailyRead
	case reportoracle.FeishuAttendancePunch:
		permission = model.PermissionFeishuAttendancePunchRead
	case reportoracle.FeishuAttendanceItem:
		permission = model.PermissionFeishuAttendanceItemRead
	default:
		return nil, ErrOpenFeishuAttendanceInvalidQuery
	}
	if actor == 0 {
		return nil, ErrOpenFeishuAttendanceForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actor, permission, service.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %w", ErrOpenFeishuAttendanceUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenFeishuAttendanceForbidden
	}
	pagination, err := normalizeOpenBojunQueryPagination(request.Page, request.PageSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenFeishuAttendanceInvalidQuery, err)
	}
	query, err := reportoracle.NormalizeFeishuAttendanceQuery(dataset, reportoracle.FeishuAttendanceQuery{
		StartDate: request.StartDate, EndDate: request.EndDate, Filters: request.Filters,
		Offset: (pagination.Page - 1) * pagination.PageSize, PageSize: pagination.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenFeishuAttendanceInvalidQuery, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, openFeishuAttendanceQueryTimeout)
	defer cancel()
	rows, err := service.query(queryCtx, dataset, query)
	if err != nil {
		return nil, fmt.Errorf("%w: query: %w", ErrOpenFeishuAttendanceUnavailable, err)
	}
	pagination.HasMore = len(rows) > pagination.PageSize
	if pagination.HasMore {
		rows = rows[:pagination.PageSize]
	}
	if rows == nil {
		rows = []reportoracle.FeishuAttendanceRow{}
	}
	return &OpenFeishuAttendanceQueryDTO{Items: rows, Pagination: pagination}, nil
}
