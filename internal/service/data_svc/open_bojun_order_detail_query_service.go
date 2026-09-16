package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

const (
	openBojunOrderDetailDefaultPageSize = 100
	openBojunOrderDetailMaxPageSize     = 200
	openBojunOrderDetailCursorVersion   = 1
	openBojunOrderDetailCursorMaxBytes  = 1024
	openBojunOrderDetailHashChunkBytes  = 64 * 1024
	openBojunOrderDetailContextRows     = 64
)

var ErrOpenBojunOrderNotFound = errors.New("open bojun order: not found")

type openBojunOrderDetailReader interface {
	FindOpenOrderDetails(context.Context, string, []string) (*model.BojunRetailOrder, error)
}

type openBojunOrderDetailMallScope interface {
	ConstrainMallCodes(context.Context, uint, []string) ([]string, error)
}

type OpenBojunOrderDetailQueryService struct {
	orders      openBojunOrderDetailReader
	permissions openBojunOrderPermissionReader
	mallScope   openBojunOrderDetailMallScope
	now         func() time.Time
}

type OpenBojunOrderDetailQueryResult struct {
	OrderNo    string                         `json:"orderNo"`
	Items      []OpenBojunOrderLineDTO        `json:"items"`
	Payments   []OpenBojunOrderPaymentDTO     `json:"payments"`
	Pagination OpenBojunOrderDetailPagination `json:"pagination"`
}

type OpenBojunOrderDetailPagination struct {
	Page       int                                   `json:"page"`
	PageSize   int                                   `json:"pageSize"`
	Items      OpenBojunOrderDetailSectionPagination `json:"items"`
	Payments   OpenBojunOrderDetailSectionPagination `json:"payments"`
	NextCursor string                                `json:"nextCursor"`
	HasMore    bool                                  `json:"hasMore"`
}

type OpenBojunOrderDetailSectionPagination struct {
	CurrentItems int   `json:"currentItems"`
	TotalItems   int64 `json:"totalItems"`
	TotalPages   int64 `json:"totalPages"`
	HasMore      bool  `json:"hasMore"`
}

type openBojunOrderDetailCursor struct {
	Version     int    `json:"version"`
	QueryHash   string `json:"queryHash"`
	PayloadHash string `json:"payloadHash"`
	Page        int    `json:"page"`
}

func NewOpenBojunOrderDetailQueryService() *OpenBojunOrderDetailQueryService {
	return newOpenBojunOrderDetailQueryService(
		data_dao.NewBojunRetailOrderDAO(database.DB),
		data_dao.NewMallWeatherPermissionDAO(database.DB),
		auth_svc.NewMallScopeService(database.DB),
		time.Now,
	)
}

func newOpenBojunOrderDetailQueryService(
	orders openBojunOrderDetailReader,
	permissions openBojunOrderPermissionReader,
	mallScope openBojunOrderDetailMallScope,
	now func() time.Time,
) *OpenBojunOrderDetailQueryService {
	if orders == nil || permissions == nil || mallScope == nil || now == nil {
		panic("open bojun order detail query service: nil dependency")
	}
	return &OpenBojunOrderDetailQueryService{
		orders: orders, permissions: permissions, mallScope: mallScope, now: now,
	}
}

func (service *OpenBojunOrderDetailQueryService) Query(
	ctx context.Context,
	actorUserID uint,
	request requestbody.OpenBojunOrderDetailQueryRequest,
) (*OpenBojunOrderDetailQueryResult, error) {
	if ctx == nil || actorUserID == 0 {
		return nil, ErrOpenBojunOrderForbidden
	}
	allowed, err := service.permissions.HasPermission(
		ctx, actorUserID, model.PermissionBojunOrderRead, service.now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("open bojun order detail query: authorize: %w", err)
	}
	if !allowed {
		return nil, ErrOpenBojunOrderForbidden
	}
	normalized, page, pageSize, cursor, err := normalizeOpenBojunOrderDetailQuery(request)
	if err != nil {
		return nil, err
	}

	queryCtx, cancel := context.WithTimeout(ctx, openBojunOrderQueryTimeout)
	defer cancel()
	allowedCodes, err := service.mallScope.ConstrainMallCodes(queryCtx, actorUserID, nil)
	if err != nil {
		if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
			return nil, ErrOpenBojunOrderNotFound
		}
		return nil, fmt.Errorf("open bojun order detail query: constrain mall scope: %w", err)
	}
	order, err := service.orders.FindOpenOrderDetails(queryCtx, normalized.OrderNo, allowedCodes)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOpenBojunOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open bojun order detail query: find order: %w", err)
	}
	payloadHash, err := openBojunOrderDetailPayloadHash(queryCtx, order.ItemsJSON, order.PayItemsJSON)
	if err != nil {
		return nil, fmt.Errorf("open bojun order detail query: hash payload: %w", err)
	}
	if cursor != nil && cursor.PayloadHash != payloadHash {
		return nil, ErrOpenBojunOrderInvalidQuery
	}
	offset := (page - 1) * pageSize
	itemValues, itemTotal, err := pageOpenBojunOrderDetailValues(queryCtx, order.ItemsJSON, offset, pageSize)
	if err != nil {
		return nil, fmt.Errorf("open bojun order detail query: decode items: %w", err)
	}
	paymentValues, paymentTotal, err := pageOpenBojunOrderDetailValues(queryCtx, order.PayItemsJSON, offset, pageSize)
	if err != nil {
		return nil, fmt.Errorf("open bojun order detail query: decode payments: %w", err)
	}
	if page > 1 && offset >= itemTotal && offset >= paymentTotal {
		return nil, ErrOpenBojunOrderInvalidQuery
	}

	items := make([]OpenBojunOrderLineDTO, 0, len(itemValues))
	for _, value := range itemValues {
		items = append(items, openBojunOrderLineDTO(value))
	}
	payments := make([]OpenBojunOrderPaymentDTO, 0, len(paymentValues))
	for _, value := range paymentValues {
		payments = append(payments, openBojunOrderPaymentDTO(value))
	}
	itemPagination := openBojunOrderDetailSection(page, pageSize, len(items), itemTotal)
	paymentPagination := openBojunOrderDetailSection(page, pageSize, len(payments), paymentTotal)
	hasMore := itemPagination.HasMore || paymentPagination.HasMore
	nextCursor := ""
	if hasMore {
		nextPage, nextErr := nextOpenCursorPage(page)
		if nextErr != nil {
			return nil, ErrOpenBojunOrderInvalidQuery
		}
		nextCursor, err = encodeOpenBojunOrderDetailCursor(openBojunOrderDetailCursor{
			Version: openBojunOrderDetailCursorVersion, QueryHash: openBojunOrderDetailQueryHash(normalized.OrderNo, pageSize),
			PayloadHash: payloadHash, Page: nextPage,
		})
		if err != nil {
			return nil, fmt.Errorf("open bojun order detail query: encode cursor: %w", err)
		}
	}
	return &OpenBojunOrderDetailQueryResult{
		OrderNo: order.DocNo, Items: items, Payments: payments,
		Pagination: OpenBojunOrderDetailPagination{
			Page: page, PageSize: pageSize, Items: itemPagination, Payments: paymentPagination,
			NextCursor: nextCursor, HasMore: hasMore,
		},
	}, nil
}

func normalizeOpenBojunOrderDetailQuery(
	request requestbody.OpenBojunOrderDetailQueryRequest,
) (requestbody.OpenBojunOrderDetailQueryRequest, int, int, *openBojunOrderDetailCursor, error) {
	request.OrderNo = strings.TrimSpace(request.OrderNo)
	if request.OrderNo == "" || len(request.OrderNo) > 255 {
		return request, 0, 0, nil, ErrOpenBojunOrderInvalidQuery
	}
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = openBojunOrderDetailDefaultPageSize
	}
	if pageSize < 1 || pageSize > openBojunOrderDetailMaxPageSize {
		return request, 0, 0, nil, ErrOpenBojunOrderInvalidQuery
	}
	if strings.TrimSpace(request.Cursor) == "" {
		return request, 1, pageSize, nil, nil
	}
	cursor, err := decodeOpenBojunOrderDetailCursor(request.Cursor)
	if err != nil || cursor.Version != openBojunOrderDetailCursorVersion || invalidOpenCursorPage(cursor.Page) ||
		len(cursor.QueryHash) != sha256.Size*2 || len(cursor.PayloadHash) != sha256.Size*2 ||
		cursor.QueryHash != openBojunOrderDetailQueryHash(request.OrderNo, pageSize) {
		return request, 0, 0, nil, ErrOpenBojunOrderInvalidQuery
	}
	return request, cursor.Page, pageSize, &cursor, nil
}

func pageOpenBojunOrderDetailValues(
	ctx context.Context,
	raw string,
	offset int,
	pageSize int,
) ([]map[string]interface{}, int, error) {
	if ctx == nil || strings.TrimSpace(raw) == "" || offset < 0 || pageSize < 1 {
		return nil, 0, fmt.Errorf("detail payload is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, 0, fmt.Errorf("detail payload must be a json array")
	}
	values := make([]map[string]interface{}, 0, pageSize)
	total := 0
	for decoder.More() {
		if total%openBojunOrderDetailContextRows == 0 {
			if err := ctx.Err(); err != nil {
				return nil, 0, err
			}
		}
		var value map[string]interface{}
		if err := decoder.Decode(&value); err != nil {
			return nil, 0, err
		}
		if total >= offset && len(values) < pageSize {
			values = append(values, value)
		}
		total++
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if _, err := decoder.Token(); err != nil {
		return nil, 0, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, 0, fmt.Errorf("detail payload contains trailing data")
		}
		return nil, 0, err
	}
	return values, total, nil
}

func openBojunOrderDetailSection(
	page int,
	pageSize int,
	currentItems int,
	totalItems int,
) OpenBojunOrderDetailSectionPagination {
	totalPages := int64(0)
	if totalItems > 0 {
		totalPages = (int64(totalItems) + int64(pageSize) - 1) / int64(pageSize)
	}
	return OpenBojunOrderDetailSectionPagination{
		CurrentItems: currentItems,
		TotalItems:   int64(totalItems),
		TotalPages:   totalPages,
		HasMore:      int64(page) < totalPages,
	}
}

func openBojunOrderDetailQueryHash(orderNo string, pageSize int) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(orderNo) + "|" + fmt.Sprintf("%d", pageSize)))
	return hex.EncodeToString(sum[:])
}

func openBojunOrderDetailPayloadHash(
	ctx context.Context,
	itemsJSON string,
	paymentsJSON string,
) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("detail payload hash: nil context")
	}
	hasher := sha256.New()
	if err := writeOpenBojunOrderDetailHash(ctx, hasher, itemsJSON); err != nil {
		return "", err
	}
	if _, err := hasher.Write([]byte{0}); err != nil {
		return "", fmt.Errorf("detail payload hash: write separator: %w", err)
	}
	if err := writeOpenBojunOrderDetailHash(ctx, hasher, paymentsJSON); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func writeOpenBojunOrderDetailHash(ctx context.Context, writer io.Writer, value string) error {
	for start := 0; start < len(value); start += openBojunOrderDetailHashChunkBytes {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + openBojunOrderDetailHashChunkBytes
		if end > len(value) {
			end = len(value)
		}
		if _, err := io.WriteString(writer, value[start:end]); err != nil {
			return fmt.Errorf("detail payload hash: write content: %w", err)
		}
	}
	return ctx.Err()
}

func encodeOpenBojunOrderDetailCursor(cursor openBojunOrderDetailCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeOpenBojunOrderDetailCursor(value string) (openBojunOrderDetailCursor, error) {
	if len(value) > openBojunOrderDetailCursorMaxBytes {
		return openBojunOrderDetailCursor{}, ErrOpenBojunOrderInvalidQuery
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return openBojunOrderDetailCursor{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.DisallowUnknownFields()
	var cursor openBojunOrderDetailCursor
	if err := decoder.Decode(&cursor); err != nil {
		return openBojunOrderDetailCursor{}, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return openBojunOrderDetailCursor{}, ErrOpenBojunOrderInvalidQuery
		}
		return openBojunOrderDetailCursor{}, err
	}
	return cursor, nil
}
