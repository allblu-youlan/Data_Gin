package data_svc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

var (
	ErrBusinessOverviewForbidden   = errors.New("business overview: forbidden")
	ErrBusinessOverviewInvalid     = errors.New("business overview: invalid query")
	ErrBusinessOverviewUnavailable = errors.New("business overview: unavailable")
)

var businessOverviewMallCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,63}$`)

type businessOverviewOracle interface {
	QueryBusinessOverviewPayments(context.Context, int, string) ([]reportoracle.BusinessOverviewPaymentRow, error)
	Close() error
}

type businessOverviewOracleOpener func(context.Context, reportoracle.Config) (businessOverviewOracle, error)

type businessOverviewMallScope interface {
	ConstrainMallCodes(context.Context, uint, []string) ([]string, error)
}

type businessOverviewMallLister interface {
	ListScopedIdentitiesAfterID(context.Context, uint, uint, int) ([]model.Mall, error)
}

type BusinessOverviewService struct {
	config          appConfig.ReportInputOracleConfig
	oracleConfigErr error
	open            businessOverviewOracleOpener
	mallScope       businessOverviewMallScope
	malls           businessOverviewMallLister
	mu              sync.Mutex
	oracle          businessOverviewOracle
}

type BusinessOverviewPaymentDTO struct {
	BillDate   int64   `json:"billDate"`
	StoreID    int64   `json:"storeId"`
	StoreName  string  `json:"storeName"`
	StoreCode  string  `json:"storeCode"`
	PaywayID   int64   `json:"paywayId"`
	PayAmount  float64 `json:"payAmount"`
	PaywayName string  `json:"paywayName"`
}

type BusinessOverviewPaymentResult struct {
	Date     string                       `json:"date"`
	MallCode string                       `json:"mallCode"`
	Items    []BusinessOverviewPaymentDTO `json:"items"`
}

type OpenBusinessOverviewPaymentDTO struct {
	PaymentMethodID   int64  `json:"paymentMethodId"`
	PaymentMethodName string `json:"paymentMethodName"`
	Amount            string `json:"amount"`
}

type OpenBusinessOverviewPaymentResult struct {
	Date           string                           `json:"date"`
	MallCode       string                           `json:"mallCode"`
	Currency       string                           `json:"currency"`
	BusinessAmount string                           `json:"businessAmount"`
	Payments       []OpenBusinessOverviewPaymentDTO `json:"payments"`
}

type BusinessOverviewMallDTO struct {
	ID       uint   `json:"id"`
	MallCode string `json:"mallCode"`
	NameCN   string `json:"nameCn"`
}

type BusinessOverviewMallListResult struct {
	Items       []BusinessOverviewMallDTO `json:"items"`
	NextAfterID uint                      `json:"nextAfterId"`
}

func NewBusinessOverviewService() (*BusinessOverviewService, error) {
	configured, configErr := appConfig.LoadReportInputQueryConfig()
	service := newBusinessOverviewService(
		configured.Oracle,
		func(ctx context.Context, config reportoracle.Config) (businessOverviewOracle, error) {
			return reportoracle.Open(ctx, config)
		},
		auth_svc.NewMallScopeService(database.DB),
		data_dao.NewMallDAO(database.DB),
	)
	if configErr != nil {
		service.oracleConfigErr = fmt.Errorf("%w: load default Oracle configuration: %v", ErrBusinessOverviewUnavailable, configErr)
	} else if !validDefaultOracleConfig(configured.Oracle) {
		service.oracleConfigErr = fmt.Errorf("%w: default Oracle configuration is incomplete", ErrBusinessOverviewUnavailable)
	}
	return service, nil
}

func newBusinessOverviewService(
	config appConfig.ReportInputOracleConfig,
	opener businessOverviewOracleOpener,
	mallScope businessOverviewMallScope,
	mallListers ...businessOverviewMallLister,
) *BusinessOverviewService {
	if opener == nil || mallScope == nil {
		panic("business overview service: dependencies are required")
	}
	malls := businessOverviewMallLister(data_dao.NewMallDAO(database.DB))
	if len(mallListers) > 0 {
		if mallListers[0] == nil {
			panic("business overview service: mall lister is required")
		}
		malls = mallListers[0]
	}
	return &BusinessOverviewService{config: config, open: opener, mallScope: mallScope, malls: malls}
}

func (service *BusinessOverviewService) QueryPayments(
	ctx context.Context,
	actor uint,
	date string,
	mallCode string,
) (*BusinessOverviewPaymentResult, error) {
	date = strings.TrimSpace(date)
	mallCode = strings.ToUpper(strings.TrimSpace(mallCode))
	billDate, err := parseBusinessOverviewDate(date)
	if service == nil || ctx == nil || actor == 0 || err != nil || !businessOverviewMallCodePattern.MatchString(mallCode) {
		return nil, ErrBusinessOverviewInvalid
	}
	if service.oracleConfigErr != nil {
		return nil, service.oracleConfigErr
	}
	allowed, err := service.mallScope.ConstrainMallCodes(ctx, actor, []string{mallCode})
	if err != nil {
		if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
			return nil, ErrBusinessOverviewForbidden
		}
		return nil, fmt.Errorf("%w: constrain mall scope: %v", ErrBusinessOverviewUnavailable, err)
	}
	if len(allowed) != 1 || allowed[0] != mallCode {
		return nil, ErrBusinessOverviewForbidden
	}

	queryCtx, cancel := context.WithTimeout(ctx, businessOverviewQueryTimeout(service.config.QueryTimeout))
	defer cancel()
	connection, err := service.connection(queryCtx)
	if err != nil {
		return nil, err
	}
	rows, err := connection.QueryBusinessOverviewPayments(queryCtx, billDate, mallCode)
	if err != nil {
		return nil, fmt.Errorf("%w: query default Oracle: %v", ErrBusinessOverviewUnavailable, err)
	}
	result := &BusinessOverviewPaymentResult{
		Date: date, MallCode: mallCode,
		Items: make([]BusinessOverviewPaymentDTO, 0, len(rows)),
	}
	for _, row := range rows {
		result.Items = append(result.Items, BusinessOverviewPaymentDTO{
			BillDate: row.BillDate, StoreID: row.StoreID, StoreName: row.StoreName, StoreCode: row.StoreCode,
			PaywayID: row.PaywayID, PayAmount: row.PayAmount, PaywayName: row.PaywayName,
		})
	}
	return result, nil
}

func (service *BusinessOverviewService) QueryOpenPayments(
	ctx context.Context,
	actor uint,
	request requestbody.OpenBusinessOverviewPaymentQueryRequest,
) (*OpenBusinessOverviewPaymentResult, error) {
	date := strings.TrimSpace(request.Date)
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return nil, ErrBusinessOverviewInvalid
	}
	result, err := service.QueryPayments(ctx, actor, parsed.Format("20060102"), request.MallCode)
	if err != nil {
		return nil, err
	}
	openResult := &OpenBusinessOverviewPaymentResult{
		Date: date, MallCode: result.MallCode, Currency: "CNY",
		Payments: make([]OpenBusinessOverviewPaymentDTO, 0, len(result.Items)),
	}
	var total float64
	for _, item := range result.Items {
		total += item.PayAmount
		openResult.Payments = append(openResult.Payments, OpenBusinessOverviewPaymentDTO{
			PaymentMethodID: item.PaywayID, PaymentMethodName: item.PaywayName,
			Amount: strconv.FormatFloat(item.PayAmount, 'f', 2, 64),
		})
	}
	openResult.BusinessAmount = strconv.FormatFloat(total, 'f', 2, 64)
	return openResult, nil
}

func (service *BusinessOverviewService) ListMalls(
	ctx context.Context,
	actor uint,
	afterID uint,
	limit int,
) (*BusinessOverviewMallListResult, error) {
	if service == nil || ctx == nil || actor == 0 || limit < 1 || limit > 200 {
		return nil, ErrBusinessOverviewInvalid
	}
	rows, err := service.malls.ListScopedIdentitiesAfterID(ctx, actor, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: list scoped malls: %v", ErrBusinessOverviewUnavailable, err)
	}
	result := &BusinessOverviewMallListResult{Items: make([]BusinessOverviewMallDTO, 0, len(rows))}
	for _, mall := range rows {
		result.Items = append(result.Items, BusinessOverviewMallDTO{ID: mall.ID, MallCode: mall.MallCode, NameCN: mall.NameCN})
	}
	if len(rows) > 0 {
		result.NextAfterID = rows[len(rows)-1].ID
	}
	return result, nil
}

func (service *BusinessOverviewService) connection(ctx context.Context) (businessOverviewOracle, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.oracle != nil {
		return service.oracle, nil
	}
	connection, err := service.open(ctx, defaultOracleAdapterConfig(service.config))
	if err != nil {
		return nil, fmt.Errorf("%w: open default Oracle: %v", ErrBusinessOverviewUnavailable, err)
	}
	service.oracle = connection
	return connection, nil
}

func parseBusinessOverviewDate(value string) (int, error) {
	parsed, err := time.Parse("20060102", value)
	if err != nil || parsed.Format("20060102") != value {
		return 0, ErrBusinessOverviewInvalid
	}
	return parsed.Year()*10000 + int(parsed.Month())*100 + parsed.Day(), nil
}

func businessOverviewQueryTimeout(configured time.Duration) time.Duration {
	if configured <= 0 {
		return 30 * time.Second
	}
	return configured
}

func validDefaultOracleConfig(config appConfig.ReportInputOracleConfig) bool {
	return strings.TrimSpace(config.Host) != "" && strings.TrimSpace(config.Username) != "" && config.Password != "" &&
		(strings.TrimSpace(config.ServiceName) != "" || strings.TrimSpace(config.SID) != "") &&
		!(strings.TrimSpace(config.ServiceName) != "" && strings.TrimSpace(config.SID) != "")
}

func defaultOracleAdapterConfig(config appConfig.ReportInputOracleConfig) reportoracle.Config {
	return reportoracle.Config{
		Host: config.Host, Port: config.Port, ServiceName: config.ServiceName, SID: config.SID,
		Username: config.Username, Password: config.Password, Timezone: config.Timezone,
		ConnectTimeout: config.ConnectTimeout, MaxOpenConnections: config.MaxOpenConnections,
		MaxIdleConnections: config.MaxIdleConnections, ConnectionLifetime: config.ConnectionLifetime,
		ConnectionIdleTime: config.ConnectionIdleTime, PrefetchRows: config.PrefetchRows, FetchArraySize: config.ArraySize,
	}
}
