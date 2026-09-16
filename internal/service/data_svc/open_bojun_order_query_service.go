package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

const (
	openBojunOrderDateTimeFormat  = "2006-01-02 15:04:05"
	openBojunOrderDefaultPageSize = 50
	openBojunOrderMaxPageSize     = 100
	openBojunOrderMaxStoreCodes   = 20
	openBojunOrderMaxRangeDays    = 31
	openBojunOrderQueryTimeout    = 3 * time.Second
	openBojunOrderMaxLines        = 200
	openBojunOrderMaxItemsBytes   = 1 << 20
	openBojunDetailComplete       = "COMPLETE"
	openBojunDetailTruncated      = "TRUNCATED"
	openBojunDetailMissing        = "MISSING"
	openBojunDetailTooLarge       = "TOO_LARGE"
	openBojunDetailInvalid        = "INVALID_PAYLOAD"
	openBojunDetailCountMismatch  = "COUNT_MISMATCH"
)

var (
	ErrOpenBojunOrderForbidden    = errors.New("open bojun order: forbidden")
	ErrOpenBojunOrderInvalidQuery = errors.New("open bojun order: invalid query")
	openBojunStoreCodePattern     = regexp.MustCompile(`^[A-Z0-9_-]{1,100}$`)
)

type OpenBojunOrderQueryService struct {
	orders      openBojunOrderReader
	permissions openBojunOrderPermissionReader
	mallScope   *auth_svc.MallScopeService
	now         func() time.Time
}

type openBojunOrderReader interface {
	MaxOpenOrderID(context.Context, data_dao.OpenBojunOrderQuery) (uint, error)
	ListOpenOrders(context.Context, data_dao.OpenBojunOrderQuery) ([]model.BojunRetailOrder, error)
	CountOpenOrders(context.Context, data_dao.OpenBojunOrderQuery) (int64, error)
}

type openBojunOrderPermissionReader interface {
	HasPermission(context.Context, uint, string, time.Time) (bool, error)
}

type OpenBojunOrderQueryResult struct {
	Items      []OpenBojunOrderDTO      `json:"items"`
	Pagination OpenBojunOrderPagination `json:"pagination"`
}

type OpenBojunOrderPagination struct {
	OpenPagination
	CurrentItems int    `json:"currentItems"`
	NextCursor   string `json:"nextCursor"`
	HasMore      bool   `json:"hasMore"`
	SnapshotAt   string `json:"snapshotAt,omitempty"`
}

type OpenBojunOrderDTO struct {
	OrderNo         string                     `json:"orderNo"`
	ExternalOrderNo string                     `json:"externalOrderNo"`
	OrderPhone      string                     `json:"order_phone"`
	OrderDate       string                     `json:"orderDate"`
	CompletedAt     *string                    `json:"completedAt"`
	UpdatedAt       string                     `json:"updatedAt"`
	MallCode        string                     `json:"mallCode"`
	MallName        string                     `json:"mallName"`
	OrderTypeCode   string                     `json:"orderTypeCode"`
	OrderTypeName   string                     `json:"orderTypeName"`
	TotalLines      int                        `json:"totalLines"`
	TotalQuantity   int                        `json:"totalQuantity"`
	ListAmount      string                     `json:"listAmount"`
	ActualAmount    string                     `json:"actualAmount"`
	AverageDiscount string                     `json:"averageDiscount"`
	Currency        string                     `json:"currency"`
	RelatedOrderNo  string                     `json:"relatedOrderNo"`
	Items           []OpenBojunOrderLineDTO    `json:"items"`
	ItemsMeta       OpenBojunDetailMeta        `json:"itemsMeta"`
	Payments        []OpenBojunOrderPaymentDTO `json:"payments"`
	PaymentsMeta    OpenBojunDetailMeta        `json:"paymentsMeta"`
}

type OpenBojunDetailMeta struct {
	Complete      bool   `json:"complete"`
	Status        string `json:"status"`
	ExpectedCount *int   `json:"expectedCount"`
	SourceCount   *int   `json:"sourceCount"`
	ReturnedCount int    `json:"returnedCount"`
}

type OpenBojunOrderLineDTO struct {
	SKUNo           string  `json:"skuNo"`
	ProductName     string  `json:"productName"`
	Quantity        string  `json:"quantity"`
	ActualAmount    string  `json:"actualAmount"`
	Type            *string `json:"type"`
	DocNo           string  `json:"docNo"`
	AmtAcc          *string `json:"amtAcc"`
	Value1          string  `json:"value1"`
	Value2          string  `json:"value2"`
	MarkDis         *string `json:"markDis"`
	Discount        *string `json:"discount"`
	PriceList       *string `json:"priceList"`
	ProductColor    string  `json:"productColor"`
	TotalAmtAcc     *string `json:"totalAmtAcc"`
	TotalListAmount *string `json:"totalListAmount"`
	DMAmtRetail     *string `json:"dmAmtRetail"`
	ActualPrice     *string `json:"actualPrice"`
	ProductValue    string  `json:"productValue"`
}

type OpenBojunOrderPaymentDTO struct {
	PaymentMethodName string  `json:"paymentMethodName"`
	Amount            *string `json:"amount"`
}

type openBojunOrderCursor struct {
	Version         int    `json:"version,omitempty"`
	QueryHash       string `json:"queryHash,omitempty"`
	CompletedAtUnix int64  `json:"completedAtUnix,omitempty"`
	UpdatedAtUnix   int64  `json:"updatedAtUnix,omitempty"`
	BillDate        int    `json:"billDate,omitempty"`
	ID              uint   `json:"id"`
	Page            int    `json:"page,omitempty"`
	SnapshotMaxID   uint   `json:"snapshotMaxId,omitempty"`
	SnapshotAtUnix  int64  `json:"snapshotAtUnix,omitempty"`
}

func NewOpenBojunOrderQueryService() *OpenBojunOrderQueryService {
	service := newOpenBojunOrderQueryService(
		data_dao.NewBojunRetailOrderDAO(database.DB),
		data_dao.NewMallWeatherPermissionDAO(database.DB),
		time.Now,
	)
	service.mallScope = auth_svc.NewMallScopeService(database.DB)
	return service
}

func newOpenBojunOrderQueryService(
	orders openBojunOrderReader,
	permissions openBojunOrderPermissionReader,
	now func() time.Time,
) *OpenBojunOrderQueryService {
	if orders == nil || permissions == nil || now == nil {
		panic("open bojun order query service: nil dependency")
	}
	return &OpenBojunOrderQueryService{orders: orders, permissions: permissions, now: now}
}

func (service *OpenBojunOrderQueryService) Query(
	ctx context.Context,
	actorUserID uint,
	request requestbody.OpenBojunOrderQueryRequest,
) (*OpenBojunOrderQueryResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("open bojun order query: nil context")
	}
	if err := service.authorize(ctx, actorUserID); err != nil {
		return nil, err
	}
	query, page, pageSize, err := normalizeOpenBojunOrderQuery(request)
	if err != nil {
		return nil, err
	}
	if service.mallScope != nil {
		query.StoreCodes, err = service.mallScope.ConstrainMallCodes(ctx, actorUserID, query.StoreCodes)
		if err != nil {
			if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
				return nil, ErrOpenBojunOrderForbidden
			}
			return nil, fmt.Errorf("open bojun order query: constrain mall scope: %w", err)
		}
	}

	queryCtx, cancel := context.WithTimeout(ctx, openBojunOrderQueryTimeout)
	defer cancel()
	if query.SnapshotMaxID == nil && strings.TrimSpace(request.Cursor) == "" {
		snapshotMaxID, snapshotErr := service.orders.MaxOpenOrderID(queryCtx, query)
		if snapshotErr != nil {
			return nil, fmt.Errorf("open bojun order query: create snapshot: %w", snapshotErr)
		}
		query.SnapshotMaxID = &snapshotMaxID
		query.SnapshotAt = service.now().UTC()
	}
	totalItems, err := service.orders.CountOpenOrders(queryCtx, query)
	if err != nil {
		return nil, fmt.Errorf("open bojun order query: count orders: %w", err)
	}
	orders, err := service.orders.ListOpenOrders(queryCtx, query)
	if err != nil {
		return nil, fmt.Errorf("open bojun order query: list orders: %w", err)
	}

	hasMore := len(orders) > pageSize
	if hasMore {
		orders = orders[:pageSize]
	}
	items := make([]OpenBojunOrderDTO, 0, len(orders))
	for index := range orders {
		items = append(items, openBojunOrderDTO(&orders[index]))
	}
	nextCursor := ""
	if hasMore && len(orders) > 0 {
		nextPage, pageErr := nextOpenCursorPage(page)
		if pageErr != nil {
			return nil, pageErr
		}
		cursor := openBojunOrderCursor{
			Version:   2,
			QueryHash: openBojunOrderQueryHash(query, pageSize),
			ID:        orders[len(orders)-1].ID,
			Page:      nextPage,
		}
		if query.SnapshotMaxID != nil && !query.SnapshotAt.IsZero() {
			cursor.Version = 3
			cursor.SnapshotMaxID = *query.SnapshotMaxID
			cursor.SnapshotAtUnix = query.SnapshotAt.Unix()
		}
		if !query.StartCompletedAt.IsZero() {
			if orders[len(orders)-1].CompletedAt == nil {
				return nil, fmt.Errorf("open bojun order query: completed-at row has no completion time")
			}
			cursor.CompletedAtUnix = orders[len(orders)-1].CompletedAt.Unix()
		} else if query.StartUpdatedAt > 0 {
			cursor.UpdatedAtUnix = int64(orders[len(orders)-1].UpdatedAt)
		} else {
			cursor.BillDate = orders[len(orders)-1].BillDate
		}
		nextCursor, err = encodeOpenBojunOrderCursor(cursor)
		if err != nil {
			return nil, fmt.Errorf("open bojun order query: encode cursor: %w", err)
		}
	}
	return &OpenBojunOrderQueryResult{
		Items: items,
		Pagination: OpenBojunOrderPagination{
			OpenPagination: newOpenPagination(page, pageSize, totalItems),
			CurrentItems:   len(items), NextCursor: nextCursor, HasMore: hasMore,
			SnapshotAt: formatOpenBojunSnapshotAt(query.SnapshotAt),
		},
	}, nil
}

func (service *OpenBojunOrderQueryService) authorize(ctx context.Context, actorUserID uint) error {
	if actorUserID == 0 {
		return ErrOpenBojunOrderForbidden
	}
	allowed, err := service.permissions.HasPermission(
		ctx,
		actorUserID,
		model.PermissionBojunOrderRead,
		service.now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("open bojun order query: authorize: %w", err)
	}
	if !allowed {
		return ErrOpenBojunOrderForbidden
	}
	return nil
}

func normalizeOpenBojunOrderQuery(
	request requestbody.OpenBojunOrderQueryRequest,
) (data_dao.OpenBojunOrderQuery, int, int, error) {
	startTimeValue := strings.TrimSpace(request.StartTime)
	endTimeValue := strings.TrimSpace(request.EndTime)
	startDateValue := strings.TrimSpace(request.StartDate)
	endDateValue := strings.TrimSpace(request.EndDate)
	updatedStartValue := strings.TrimSpace(request.UpdatedStartTime)
	updatedEndValue := strings.TrimSpace(request.UpdatedEndTime)
	completedAtMode := startTimeValue != "" || endTimeValue != ""
	billDateMode := startDateValue != "" || endDateValue != ""
	updatedAtMode := updatedStartValue != "" || updatedEndValue != ""
	if (!completedAtMode && !billDateMode && !updatedAtMode) ||
		(completedAtMode && updatedAtMode) || (billDateMode && updatedAtMode) {
		return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid time range combination", ErrOpenBojunOrderInvalidQuery)
	}
	if len(request.MallCodes) > 0 && len(request.StoreCodes) > 0 {
		return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: mallCodes and storeCodes conflict", ErrOpenBojunOrderInvalidQuery)
	}
	mallCodes := request.MallCodes
	if len(mallCodes) == 0 {
		mallCodes = request.StoreCodes
	}
	storeCodes, err := normalizeOpenBojunMallCodes(mallCodes)
	if err != nil {
		return data_dao.OpenBojunOrderQuery{}, 0, 0, err
	}
	orderTypes, err := normalizeOpenBojunOrderTypes(request.OrderTypes)
	if err != nil {
		return data_dao.OpenBojunOrderQuery{}, 0, 0, err
	}
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = openBojunOrderDefaultPageSize
	}
	if pageSize < 1 || pageSize > openBojunOrderMaxPageSize {
		return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid pageSize", ErrOpenBojunOrderInvalidQuery)
	}

	query := data_dao.OpenBojunOrderQuery{
		StoreCodes: storeCodes,
		OrderTypes: orderTypes,
		Limit:      pageSize + 1,
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	var startCompletedAt, endCompletedAt time.Time
	var startBillDate, endBillDate int
	if completedAtMode {
		startCompletedAt, err = parseOpenBojunOrderTime(startTimeValue, location)
		if err != nil {
			return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid startTime", ErrOpenBojunOrderInvalidQuery)
		}
		endCompletedAt, err = parseOpenBojunOrderTime(endTimeValue, location)
		if err != nil || !endCompletedAt.After(startCompletedAt) ||
			endCompletedAt.Sub(startCompletedAt) > openBojunOrderMaxRangeDays*24*time.Hour {
			return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid endTime", ErrOpenBojunOrderInvalidQuery)
		}
		query.StartCompletedAt = startCompletedAt
		query.EndCompletedAt = endCompletedAt
	} else if updatedAtMode {
		startUpdatedAt, parseErr := parseOpenBojunOrderTime(updatedStartValue, location)
		if parseErr != nil {
			return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid updatedStartTime", ErrOpenBojunOrderInvalidQuery)
		}
		endUpdatedAt, parseErr := parseOpenBojunOrderTime(updatedEndValue, location)
		if parseErr != nil || !endUpdatedAt.After(startUpdatedAt) ||
			endUpdatedAt.Sub(startUpdatedAt) > openBojunOrderMaxRangeDays*24*time.Hour {
			return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid updatedEndTime", ErrOpenBojunOrderInvalidQuery)
		}
		query.StartUpdatedAt = startUpdatedAt.Unix()
		query.EndUpdatedAt = endUpdatedAt.Unix()
	}
	if billDateMode {
		start, startErr := time.Parse("2006-01-02", startDateValue)
		end, endErr := time.Parse("2006-01-02", endDateValue)
		if startErr != nil || endErr != nil || end.Before(start) ||
			end.Sub(start) > (openBojunOrderMaxRangeDays-1)*24*time.Hour {
			return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid legacy date range", ErrOpenBojunOrderInvalidQuery)
		}
		startBillDate, _ = strconv.Atoi(start.Format("20060102"))
		endBillDate, _ = strconv.Atoi(end.Format("20060102"))
		query.StartBillDate = startBillDate
		query.EndBillDate = endBillDate
	}
	page := 1
	if cursorValue := strings.TrimSpace(request.Cursor); cursorValue != "" {
		cursor, err := decodeOpenBojunOrderCursor(cursorValue)
		if err != nil {
			return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid cursor", ErrOpenBojunOrderInvalidQuery)
		}
		expectedQueryHash := openBojunOrderQueryHash(query, pageSize)
		if cursor.Version == 2 || cursor.Version == 3 {
			if cursor.QueryHash != expectedQueryHash {
				return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: cursor filters changed", ErrOpenBojunOrderInvalidQuery)
			}
		} else if completedAtMode || cursor.Version != 0 || cursor.QueryHash != "" {
			return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid cursor version", ErrOpenBojunOrderInvalidQuery)
		}
		if completedAtMode {
			before := time.Unix(cursor.CompletedAtUnix, 0).In(location)
			if cursor.CompletedAtUnix <= 0 || cursor.UpdatedAtUnix != 0 || cursor.BillDate != 0 ||
				before.Before(startCompletedAt) || !before.Before(endCompletedAt) {
				return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid cursor", ErrOpenBojunOrderInvalidQuery)
			}
			query.BeforeCompletedAt = &before
		} else if updatedAtMode {
			if cursor.UpdatedAtUnix < query.StartUpdatedAt || cursor.UpdatedAtUnix >= query.EndUpdatedAt ||
				cursor.CompletedAtUnix != 0 || cursor.BillDate != 0 {
				return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid cursor", ErrOpenBojunOrderInvalidQuery)
			}
			query.BeforeUpdatedAt = cursor.UpdatedAtUnix
		} else {
			if cursor.CompletedAtUnix != 0 || cursor.UpdatedAtUnix != 0 || cursor.BillDate < startBillDate || cursor.BillDate > endBillDate {
				return data_dao.OpenBojunOrderQuery{}, 0, 0, fmt.Errorf("%w: invalid cursor", ErrOpenBojunOrderInvalidQuery)
			}
			query.BeforeBillDate = cursor.BillDate
		}
		query.BeforeID = cursor.ID
		if cursor.Version == 3 {
			snapshotMaxID := cursor.SnapshotMaxID
			query.SnapshotMaxID = &snapshotMaxID
			query.SnapshotAt = time.Unix(cursor.SnapshotAtUnix, 0).UTC()
		}
		page = openCursorPage(cursor.Page, true)
	}
	return query, page, pageSize, nil
}

func parseOpenBojunOrderTime(value string, location *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation(openBojunOrderDateTimeFormat, value, location)
	if err != nil || parsed.Format(openBojunOrderDateTimeFormat) != value {
		return time.Time{}, ErrOpenBojunOrderInvalidQuery
	}
	return parsed, nil
}

func normalizeOpenBojunMallCodes(values []string) ([]string, error) {
	if len(values) > openBojunOrderMaxStoreCodes {
		return nil, fmt.Errorf("%w: invalid mallCodes", ErrOpenBojunOrderInvalidQuery)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if !openBojunStoreCodePattern.MatchString(value) {
			return nil, fmt.Errorf("%w: invalid mallCodes", ErrOpenBojunOrderInvalidQuery)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeOpenBojunOrderTypes(values []string) ([]string, error) {
	allowed := map[string]struct{}{"CMR": {}, "RET": {}, "EXP": {}}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if _, ok := allowed[value]; !ok {
			return nil, fmt.Errorf("%w: invalid orderTypes", ErrOpenBojunOrderInvalidQuery)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func openBojunOrderQueryHash(query data_dao.OpenBojunOrderQuery, pageSize int) string {
	mode := "completedAt"
	start := query.StartCompletedAt.Format(openBojunOrderDateTimeFormat)
	end := query.EndCompletedAt.Format(openBojunOrderDateTimeFormat)
	extra := ""
	if query.StartCompletedAt.IsZero() {
		if query.StartUpdatedAt > 0 {
			mode = "updatedAt"
			start = strconv.FormatInt(query.StartUpdatedAt, 10)
			end = strconv.FormatInt(query.EndUpdatedAt, 10)
		} else {
			mode = "billDate"
			start = strconv.Itoa(query.StartBillDate)
			end = strconv.Itoa(query.EndBillDate)
		}
	} else if query.StartBillDate > 0 {
		mode = "completedAt+billDate"
		extra = strconv.Itoa(query.StartBillDate) + ":" + strconv.Itoa(query.EndBillDate)
	}
	parts := []string{mode, start, end}
	if extra != "" {
		parts = append(parts, extra)
	}
	parts = append(parts, strings.Join(query.StoreCodes, ","), strings.Join(query.OrderTypes, ","), strconv.Itoa(pageSize))
	payload := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func openBojunOrderDTO(order *model.BojunRetailOrder) OpenBojunOrderDTO {
	items, itemsMeta := openBojunOrderLines(order.ItemsJSON)
	itemsMeta = reconcileOpenBojunItemCount(itemsMeta, order.TotalLines)
	payments, paymentsMeta := openBojunOrderPayments(order.PayItemsJSON)
	return OpenBojunOrderDTO{
		OrderNo:         order.DocNo,
		ExternalOrderNo: order.OtherDocNo,
		OrderPhone:      order.OrderPhone,
		OrderDate:       formatOpenBojunBillDate(order.BillDate),
		CompletedAt:     formatOpenBojunCompletedAt(order.CompletedAt),
		UpdatedAt:       formatOpenBojunUpdatedAt(order.UpdatedAt),
		MallCode:        order.StoreCode,
		MallName:        order.StoreName,
		OrderTypeCode:   order.OrderTypeCode,
		OrderTypeName:   order.OrderTypeName,
		TotalLines:      order.TotalLines,
		TotalQuantity:   order.TotalQty,
		ListAmount:      strconv.FormatFloat(order.TotalAmtList, 'f', 2, 64),
		ActualAmount:    strconv.FormatFloat(order.TotalAmtActual, 'f', 2, 64),
		AverageDiscount: strconv.FormatFloat(order.AvgDiscount, 'f', 4, 64),
		Currency:        "CNY",
		RelatedOrderNo:  order.RelatedNormalNo,
		Items:           items,
		ItemsMeta:       itemsMeta,
		Payments:        payments,
		PaymentsMeta:    paymentsMeta,
	}
}

func reconcileOpenBojunItemCount(meta OpenBojunDetailMeta, expected int) OpenBojunDetailMeta {
	meta.ExpectedCount = &expected
	if meta.Complete && meta.SourceCount != nil && *meta.SourceCount != expected {
		meta.Complete = false
		meta.Status = openBojunDetailCountMismatch
	}
	return meta
}

func openBojunOrderLines(raw string) ([]OpenBojunOrderLineDTO, OpenBojunDetailMeta) {
	values, meta := openBojunDetailValues(raw)
	if !meta.Complete && meta.Status != openBojunDetailTruncated {
		return []OpenBojunOrderLineDTO{}, meta
	}
	if len(values) > openBojunOrderMaxLines {
		values = values[:openBojunOrderMaxLines]
	}
	meta.ReturnedCount = len(values)

	items := make([]OpenBojunOrderLineDTO, 0, len(values))
	for _, value := range values {
		items = append(items, OpenBojunOrderLineDTO{
			SKUNo:           truncateOpenBojunOrderString(stringFromAny(value["no"]), 128),
			ProductName:     truncateOpenBojunOrderString(stringFromAny(value["mProductName"]), 500),
			Quantity:        formatOpenBojunOrderNumber(floatFromAny(value["qty"]), -1),
			ActualAmount:    formatOpenBojunOrderNumber(floatFromAny(value["totAmtActual"]), 2),
			Type:            formatOpenBojunOrderNullableNumber(value["type"], -1),
			DocNo:           truncateOpenBojunOrderString(stringFromAny(value["docno"]), 255),
			AmtAcc:          formatOpenBojunOrderNullableNumber(value["amtAcc"], 2),
			Value1:          truncateOpenBojunOrderString(stringFromAny(value["value1"]), 255),
			Value2:          truncateOpenBojunOrderString(stringFromAny(value["value2"]), 255),
			MarkDis:         formatOpenBojunOrderNullableNumber(value["markdis"], -1),
			Discount:        formatOpenBojunOrderNullableNumber(value["discount"], 4),
			PriceList:       formatOpenBojunOrderNullableNumber(value["pricelist"], 2),
			ProductColor:    truncateOpenBojunOrderString(stringFromAny(value["prodColor"]), 128),
			TotalAmtAcc:     formatOpenBojunOrderNullableNumber(value["totAmtAcc"], 2),
			TotalListAmount: formatOpenBojunOrderNullableNumber(value["totAmtList"], 2),
			DMAmtRetail:     formatOpenBojunOrderNullableNumber(value["dmAmtRetail"], 2),
			ActualPrice:     formatOpenBojunOrderNullableNumber(value["priceactual"], 2),
			ProductValue:    truncateOpenBojunOrderString(stringFromAny(value["productValue"]), 500),
		})
	}
	return items, meta
}

func openBojunOrderPayments(raw string) ([]OpenBojunOrderPaymentDTO, OpenBojunDetailMeta) {
	values, meta := openBojunDetailValues(raw)
	if !meta.Complete && meta.Status != openBojunDetailTruncated {
		return []OpenBojunOrderPaymentDTO{}, meta
	}
	if len(values) > openBojunOrderMaxLines {
		values = values[:openBojunOrderMaxLines]
	}
	meta.ReturnedCount = len(values)

	payments := make([]OpenBojunOrderPaymentDTO, 0, len(values))
	for _, value := range values {
		payments = append(payments, OpenBojunOrderPaymentDTO{
			PaymentMethodName: truncateOpenBojunOrderString(stringFromAny(value["cPaywayName"]), 255),
			Amount:            formatOpenBojunOrderNullableNumber(value["payamount"], 2),
		})
	}
	return payments, meta
}

func openBojunDetailValues(raw string) ([]map[string]interface{}, OpenBojunDetailMeta) {
	if len(raw) == 0 {
		return nil, OpenBojunDetailMeta{Status: openBojunDetailMissing}
	}
	if len(raw) > openBojunOrderMaxItemsBytes {
		return nil, OpenBojunDetailMeta{Status: openBojunDetailTooLarge}
	}
	var values []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, OpenBojunDetailMeta{Status: openBojunDetailInvalid}
	}
	if values == nil {
		return nil, OpenBojunDetailMeta{Status: openBojunDetailMissing}
	}
	sourceCount := len(values)
	meta := OpenBojunDetailMeta{
		Complete:      true,
		Status:        openBojunDetailComplete,
		SourceCount:   &sourceCount,
		ReturnedCount: sourceCount,
	}
	if len(values) > openBojunOrderMaxLines {
		meta.Complete = false
		meta.Status = openBojunDetailTruncated
		meta.ReturnedCount = openBojunOrderMaxLines
	}
	return values, meta
}

func formatOpenBojunOrderNullableNumber(value interface{}, precision int) *string {
	number, ok := parseOpenBojunOrderNumber(value)
	if !ok {
		return nil
	}
	formatted := strconv.FormatFloat(number, 'f', precision, 64)
	return &formatted
}

func parseOpenBojunOrderNumber(value interface{}) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case float64:
		number = typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func formatOpenBojunOrderNumber(value float64, precision int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', precision, 64)
}

func truncateOpenBojunOrderString(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func formatOpenBojunBillDate(value int) string {
	parsed, err := time.Parse("20060102", strconv.Itoa(value))
	if err != nil {
		return ""
	}
	return parsed.Format(openBojunOrderDateTimeFormat)
}

func formatOpenBojunCompletedAt(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	formatted := value.In(location).Format(openBojunOrderDateTimeFormat)
	return &formatted
}

func formatOpenBojunSnapshotAt(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	return value.In(location).Format(openBojunOrderDateTimeFormat)
}

func formatOpenBojunUpdatedAt(value int) string {
	if value <= 0 {
		return ""
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	return time.Unix(int64(value), 0).In(location).Format(openBojunOrderDateTimeFormat)
}

func encodeOpenBojunOrderCursor(cursor openBojunOrderCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeOpenBojunOrderCursor(value string) (openBojunOrderCursor, error) {
	if len(value) > 512 {
		return openBojunOrderCursor{}, ErrOpenBojunOrderInvalidQuery
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return openBojunOrderCursor{}, err
	}
	var cursor openBojunOrderCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return openBojunOrderCursor{}, ErrOpenBojunOrderInvalidQuery
	}
	hasCompletedAt := cursor.CompletedAtUnix > 0
	hasBillDate := cursor.BillDate > 0
	hasUpdatedAt := cursor.UpdatedAtUnix > 0
	validVersion := (cursor.Version == 0 && cursor.QueryHash == "" && cursor.UpdatedAtUnix == 0 && cursor.SnapshotMaxID == 0 && cursor.SnapshotAtUnix == 0) ||
		(cursor.Version == 2 && len(cursor.QueryHash) == sha256.Size*2 && cursor.UpdatedAtUnix == 0 && cursor.SnapshotMaxID == 0 && cursor.SnapshotAtUnix == 0) ||
		(cursor.Version == 3 && len(cursor.QueryHash) == sha256.Size*2 && cursor.SnapshotMaxID > 0 &&
			cursor.ID <= cursor.SnapshotMaxID && cursor.SnapshotAtUnix > 0)
	if !validVersion || countOpenBojunQueryModes(hasCompletedAt, hasBillDate, hasUpdatedAt) != 1 || cursor.ID == 0 || invalidOpenCursorPage(cursor.Page) {
		return openBojunOrderCursor{}, ErrOpenBojunOrderInvalidQuery
	}
	return cursor, nil
}

func countOpenBojunQueryModes(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}
