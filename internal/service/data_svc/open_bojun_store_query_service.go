package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

var (
	ErrOpenBojunStoreForbidden    = errors.New("open bojun store: forbidden")
	ErrOpenBojunStoreInvalidQuery = errors.New("open bojun store: invalid query")
	ErrOpenBojunStoreUnavailable  = errors.New("open bojun store: unavailable")
)

type OpenBojunStoreQueryService struct {
	provider    *OpenBojunOracleProvider
	permissions openBojunQueryPermissionReader
	now         func() time.Time
}

type OpenBojunStoreDTO struct {
	StoreID   int64   `json:"storeId"`
	StoreCode string  `json:"storeCode"`
	StoreName string  `json:"storeName"`
	IsActive  string  `json:"isActive"`
	IsStop    *string `json:"isStop"`
}

type OpenBojunStoreQueryDTO struct {
	Items      []OpenBojunStoreDTO      `json:"items"`
	Pagination OpenBojunQueryPagination `json:"pagination"`
}

func NewOpenBojunStoreQueryService(provider *OpenBojunOracleProvider) *OpenBojunStoreQueryService {
	return newOpenBojunStoreQueryService(provider, newAccountPermissionChecker(database.DB), time.Now)
}

func newOpenBojunStoreQueryService(provider *OpenBojunOracleProvider, permissions openBojunQueryPermissionReader, now func() time.Time) *OpenBojunStoreQueryService {
	if provider == nil || permissions == nil || now == nil {
		panic("open bojun store query service: nil dependency")
	}
	return &OpenBojunStoreQueryService{provider: provider, permissions: permissions, now: now}
}

func (service *OpenBojunStoreQueryService) Query(ctx context.Context, actorUserID uint, request requestbody.OpenBojunStoreQueryRequest) (*OpenBojunStoreQueryDTO, error) {
	if ctx == nil {
		return nil, ErrOpenBojunStoreInvalidQuery
	}
	if actorUserID == 0 {
		return nil, ErrOpenBojunStoreForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, model.PermissionBojunStoreRead, service.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %w", ErrOpenBojunStoreUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenBojunStoreForbidden
	}
	query := reportoracle.BojunStoreQuery{
		StoreCode: strings.TrimSpace(request.StoreCode), NamePrefix: strings.TrimSpace(request.NamePrefix),
		IsActive: strings.TrimSpace(request.IsActive), IsStop: strings.TrimSpace(request.IsStop),
	}
	if query.IsActive == "" {
		query.IsActive = "Y"
	}
	if len(query.StoreCode) > 80 || len(query.NamePrefix) > 255 ||
		(query.IsActive != "Y" && query.IsActive != "N" && query.IsActive != "ALL") ||
		(query.IsStop != "" && query.IsStop != "Y" && query.IsStop != "N") {
		return nil, ErrOpenBojunStoreInvalidQuery
	}
	pagination, err := normalizeOpenBojunQueryPagination(request.Page, request.PageSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenBojunStoreInvalidQuery, err)
	}
	query.Offset, query.PageSize = (pagination.Page-1)*pagination.PageSize, pagination.PageSize
	queryCtx, cancel := context.WithTimeout(ctx, service.provider.queryTimeout(5*time.Second))
	defer cancel()
	connection, err := service.provider.connection(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenBojunStoreUnavailable, err)
	}
	rows, err := connection.QueryBojunStores(queryCtx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: query: %w", ErrOpenBojunStoreUnavailable, err)
	}
	pagination.HasMore = len(rows) > pagination.PageSize
	if pagination.HasMore {
		rows = rows[:pagination.PageSize]
	}
	items := make([]OpenBojunStoreDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, OpenBojunStoreDTO{
			StoreID:   row.StoreID,
			StoreCode: row.StoreCode,
			StoreName: row.StoreName,
			IsActive:  row.IsActive,
			IsStop:    row.IsStop,
		})
	}
	return &OpenBojunStoreQueryDTO{Items: items, Pagination: pagination}, nil
}
