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
	ErrOpenYouzanForbidden    = errors.New("open youzan: forbidden")
	ErrOpenYouzanInvalidQuery = errors.New("open youzan: invalid query")
	ErrOpenYouzanUnavailable  = errors.New("open youzan: unavailable")
)

const openYouzanQueryTimeout = 10 * time.Second

type openYouzanQuery func(context.Context, reportoracle.YouzanDataset, reportoracle.YouzanQuery) ([]reportoracle.YouzanRow, error)

type OpenYouzanQueryService struct {
	query       openYouzanQuery
	permissions openBojunQueryPermissionReader
	now         func() time.Time
}

type OpenYouzanQueryDTO struct {
	Items      []reportoracle.YouzanRow `json:"items"`
	Pagination OpenBojunQueryPagination `json:"pagination"`
}

func NewOpenYouzanQueryService() *OpenYouzanQueryService {
	return newOpenYouzanQueryService(func(ctx context.Context, dataset reportoracle.YouzanDataset, query reportoracle.YouzanQuery) ([]reportoracle.YouzanRow, error) {
		var rows []reportoracle.YouzanRow
		// Reuse the environment Oracle pool, configuration rotation and release lifecycle.
		err := withOracleOperationConnection(ctx, openYouzanQueryTimeout, func(queryCtx context.Context, adapter *reportoracle.Adapter) error {
			var err error
			rows, err = adapter.QueryYouzan(queryCtx, dataset, query)
			return err
		})
		return rows, err
	}, newAccountPermissionChecker(database.DB), time.Now)
}

func newOpenYouzanQueryService(query openYouzanQuery, permissions openBojunQueryPermissionReader, now func() time.Time) *OpenYouzanQueryService {
	if query == nil || permissions == nil || now == nil {
		panic("open youzan service: nil dependency")
	}
	return &OpenYouzanQueryService{query: query, permissions: permissions, now: now}
}

func (service *OpenYouzanQueryService) Query(ctx context.Context, actor uint, dataset reportoracle.YouzanDataset, request requestbody.OpenYouzanQueryRequest) (*OpenYouzanQueryDTO, error) {
	if ctx == nil {
		return nil, ErrOpenYouzanInvalidQuery
	}
	permission := ""
	switch dataset {
	case reportoracle.YouzanOrders:
		permission = model.PermissionYouzanOrdersRead
	case reportoracle.YouzanOrderItems:
		permission = model.PermissionYouzanOrderItemsRead
	default:
		return nil, ErrOpenYouzanInvalidQuery
	}
	if actor == 0 {
		return nil, ErrOpenYouzanForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actor, permission, service.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %w", ErrOpenYouzanUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenYouzanForbidden
	}
	pagination, err := normalizeOpenBojunQueryPagination(request.Page, request.PageSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenYouzanInvalidQuery, err)
	}
	query, err := reportoracle.NormalizeYouzanQuery(dataset, reportoracle.YouzanQuery{
		Filters:   request.Filters,
		StartTime: request.StartTime, EndTime: request.EndTime, TimeField: request.TimeField,
		Offset: (pagination.Page - 1) * pagination.PageSize, PageSize: pagination.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenYouzanInvalidQuery, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, openYouzanQueryTimeout)
	defer cancel()
	rows, err := service.query(queryCtx, dataset, query)
	if err != nil {
		return nil, fmt.Errorf("%w: query: %w", ErrOpenYouzanUnavailable, err)
	}
	pagination.HasMore = len(rows) > pagination.PageSize
	if pagination.HasMore {
		rows = rows[:pagination.PageSize]
	}
	if rows == nil {
		rows = []reportoracle.YouzanRow{}
	}
	return &OpenYouzanQueryDTO{Items: rows, Pagination: pagination}, nil
}
