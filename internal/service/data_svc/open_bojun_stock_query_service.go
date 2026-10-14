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
	ErrOpenBojunStockForbidden    = errors.New("open bojun stock: forbidden")
	ErrOpenBojunStockInvalidQuery = errors.New("open bojun stock: invalid query")
	ErrOpenBojunStockUnavailable  = errors.New("open bojun stock: unavailable")
)

type OpenBojunStockQueryService struct {
	provider    *OpenBojunOracleProvider
	permissions openBojunQueryPermissionReader
	now         func() time.Time
}

type OpenBojunStockDTO struct {
	StoreCode              string  `json:"storeCode"`
	StoreName              *string `json:"storeName"`
	ProductCode            string  `json:"productCode"`
	ProductColor           *string `json:"productColor"`
	ProductName            *string `json:"productName"`
	Value                  *string `json:"value"`
	Value1                 *string `json:"value1"`
	SizeCode               *string `json:"sizeCode"`
	Value2                 *string `json:"value2"`
	PriceList              *string `json:"priceList"`
	Qty                    *string `json:"qty"`
	AmtPriceQty            *string `json:"amtPriceQty"`
	QtyBas                 *string `json:"qtyBas"`
	QtyPreOut              *string `json:"qtyPreOut"`
	QtyBasPreOut           *string `json:"qtyBasPreOut"`
	AmtPriceQtyPreOut      *string `json:"amtPriceQtyPreOut"`
	QtyPreIn               *string `json:"qtyPreIn"`
	AmtPriceQtyPreIn       *string `json:"amtPriceQtyPreIn"`
	QtyFreeze              *string `json:"qtyFreeze"`
	QtyOms                 *string `json:"qtyOms"`
	QtyValid               *string `json:"qtyValid"`
	AmtPriceQtyValid       *string `json:"amtPriceQtyValid"`
	QtyConsign             *string `json:"qtyConsign"`
	JstPreOut              *string `json:"jstPreOut"`
	JstDiffQty             *string `json:"jstDiffQty"`
	QtyCan                 *string `json:"qtyCan"`
	AttributeSetInstanceID *int64  `json:"attributeSetInstanceId"`
	IsActive               *string `json:"isActive"`
}

type OpenBojunStockQueryDTO struct {
	Items      []OpenBojunStockDTO      `json:"items"`
	Pagination OpenBojunQueryPagination `json:"pagination"`
}

func NewOpenBojunStockQueryService(provider *OpenBojunOracleProvider) *OpenBojunStockQueryService {
	return newOpenBojunStockQueryService(provider, newAccountPermissionChecker(database.DB), time.Now)
}

func newOpenBojunStockQueryService(provider *OpenBojunOracleProvider, permissions openBojunQueryPermissionReader, now func() time.Time) *OpenBojunStockQueryService {
	if provider == nil || permissions == nil || now == nil {
		panic("open bojun stock query service: nil dependency")
	}
	return &OpenBojunStockQueryService{provider: provider, permissions: permissions, now: now}
}

func (service *OpenBojunStockQueryService) Query(ctx context.Context, actorUserID uint, request requestbody.OpenBojunStockQueryRequest) (*OpenBojunStockQueryDTO, error) {
	if ctx == nil {
		return nil, ErrOpenBojunStockInvalidQuery
	}
	if actorUserID == 0 {
		return nil, ErrOpenBojunStockForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, model.PermissionBojunStockRead, service.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %w", ErrOpenBojunStockUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenBojunStockForbidden
	}
	query := reportoracle.BojunStockQuery{ProductCode: strings.TrimSpace(request.ProductCode), StoreCode: strings.TrimSpace(request.StoreCode)}
	if query.ProductCode == "" || query.StoreCode == "" || len(query.ProductCode) > 80 || len(query.StoreCode) > 80 {
		return nil, ErrOpenBojunStockInvalidQuery
	}
	pagination, err := normalizeOpenBojunQueryPagination(request.Page, request.PageSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenBojunStockInvalidQuery, err)
	}
	query.Offset, query.PageSize = (pagination.Page-1)*pagination.PageSize, pagination.PageSize
	queryCtx, cancel := context.WithTimeout(ctx, service.provider.queryTimeout(10*time.Second))
	defer cancel()
	connection, err := service.provider.connection(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenBojunStockUnavailable, err)
	}
	rows, err := connection.QueryBojunStocks(queryCtx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: query: %w", ErrOpenBojunStockUnavailable, err)
	}
	pagination.HasMore = len(rows) > pagination.PageSize
	if pagination.HasMore {
		rows = rows[:pagination.PageSize]
	}
	items := make([]OpenBojunStockDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, OpenBojunStockDTO{
			StoreCode:              row.StoreCode,
			StoreName:              row.StoreName,
			ProductCode:            row.ProductCode,
			ProductColor:           row.ProductColor,
			ProductName:            row.ProductName,
			Value:                  row.Value,
			Value1:                 row.Value1,
			SizeCode:               row.SizeCode,
			Value2:                 row.Value2,
			PriceList:              row.PriceList,
			Qty:                    row.Qty,
			AmtPriceQty:            row.AmtPriceQty,
			QtyBas:                 row.QtyBas,
			QtyPreOut:              row.QtyPreOut,
			QtyBasPreOut:           row.QtyBasPreOut,
			AmtPriceQtyPreOut:      row.AmtPriceQtyPreOut,
			QtyPreIn:               row.QtyPreIn,
			AmtPriceQtyPreIn:       row.AmtPriceQtyPreIn,
			QtyFreeze:              row.QtyFreeze,
			QtyOms:                 row.QtyOms,
			QtyValid:               row.QtyValid,
			AmtPriceQtyValid:       row.AmtPriceQtyValid,
			QtyConsign:             row.QtyConsign,
			JstPreOut:              row.JstPreOut,
			JstDiffQty:             row.JstDiffQty,
			QtyCan:                 row.QtyCan,
			AttributeSetInstanceID: row.AttributeSetInstanceID,
			IsActive:               row.IsActive,
		})
	}
	return &OpenBojunStockQueryDTO{Items: items, Pagination: pagination}, nil
}
