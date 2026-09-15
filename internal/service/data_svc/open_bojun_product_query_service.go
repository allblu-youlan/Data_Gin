package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

const (
	openBojunProductMaxCodeLength   = 100
	openBojunProductMaxQueryTimeout = 5 * time.Second
)

var (
	ErrOpenBojunProductForbidden    = errors.New("open bojun product: forbidden")
	ErrOpenBojunProductInvalidQuery = errors.New("open bojun product: invalid query")
	ErrOpenBojunProductNotFound     = errors.New("open bojun product: not found")
	ErrOpenBojunProductUnavailable  = errors.New("open bojun product: unavailable")
)

type openBojunProductOracle interface {
	QueryBojunProductByCode(context.Context, string) (*reportoracle.BojunProductRow, error)
}

type openBojunProductOracleOpener func(context.Context, reportoracle.Config) (openBojunProductOracle, error)

type openBojunProductPermissionReader interface {
	HasPermission(context.Context, uint, string, time.Time) (bool, error)
}

type OpenBojunProductQueryService struct {
	config          appConfig.ReportInputOracleConfig
	oracleConfigErr error
	open            openBojunProductOracleOpener
	permissions     openBojunProductPermissionReader
	now             func() time.Time
	mu              sync.Mutex
	oracle          openBojunProductOracle
}

type OpenBojunProductDTO struct {
	PriceList    string `json:"priceList"`
	ProductName  string `json:"productName"`
	Value        string `json:"value"`
	ProductCode  string `json:"productCode"`
	ProductColor string `json:"productColor"`
	Value1       string `json:"value1"`
	Value2       string `json:"value2"`
}

func NewOpenBojunProductQueryService() *OpenBojunProductQueryService {
	configured, configErr := appConfig.LoadReportInputQueryConfig()
	service := newOpenBojunProductQueryService(
		configured.Oracle,
		func(ctx context.Context, config reportoracle.Config) (openBojunProductOracle, error) {
			return reportoracle.Open(ctx, config)
		},
		data_dao.NewMallWeatherPermissionDAO(database.DB),
		time.Now,
	)
	if configErr != nil {
		service.oracleConfigErr = fmt.Errorf("%w: load default Oracle configuration: %v", ErrOpenBojunProductUnavailable, configErr)
	} else if !validDefaultOracleConfig(configured.Oracle) {
		service.oracleConfigErr = fmt.Errorf("%w: default Oracle configuration is incomplete", ErrOpenBojunProductUnavailable)
	}
	return service
}

func newOpenBojunProductQueryService(
	config appConfig.ReportInputOracleConfig,
	opener openBojunProductOracleOpener,
	permissions openBojunProductPermissionReader,
	now func() time.Time,
) *OpenBojunProductQueryService {
	if opener == nil || permissions == nil || now == nil {
		panic("open bojun product query service: nil dependency")
	}
	return &OpenBojunProductQueryService{
		config: config, open: opener, permissions: permissions, now: now,
	}
}

func (service *OpenBojunProductQueryService) Query(
	ctx context.Context,
	actorUserID uint,
	request requestbody.OpenBojunProductQueryRequest,
) (*OpenBojunProductDTO, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrOpenBojunProductInvalidQuery)
	}
	if err := service.authorize(ctx, actorUserID); err != nil {
		return nil, err
	}
	productCode, err := normalizeOpenBojunProductCode(request.ProductCode)
	if err != nil {
		return nil, err
	}
	if service.oracleConfigErr != nil {
		return nil, service.oracleConfigErr
	}

	queryCtx, cancel := context.WithTimeout(ctx, openBojunProductQueryTimeout(service.config.QueryTimeout))
	defer cancel()
	connection, err := service.connection(queryCtx)
	if err != nil {
		return nil, err
	}
	row, err := connection.QueryBojunProductByCode(queryCtx, productCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOpenBojunProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: query default Oracle: %v", ErrOpenBojunProductUnavailable, err)
	}
	if row == nil {
		return nil, fmt.Errorf("%w: query returned an empty row", ErrOpenBojunProductUnavailable)
	}
	return &OpenBojunProductDTO{
		PriceList: row.PriceList, ProductName: row.ProductName, Value: row.Value,
		ProductCode: row.ProductCode, ProductColor: row.ProdColor,
		Value1: row.Value1, Value2: row.Value2,
	}, nil
}

func (service *OpenBojunProductQueryService) authorize(ctx context.Context, actorUserID uint) error {
	if actorUserID == 0 {
		return ErrOpenBojunProductForbidden
	}
	allowed, err := service.permissions.HasPermission(
		ctx,
		actorUserID,
		model.PermissionBojunOrderRead,
		service.now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("open bojun product query: authorize: %w", err)
	}
	if !allowed {
		return ErrOpenBojunProductForbidden
	}
	return nil
}

func (service *OpenBojunProductQueryService) connection(ctx context.Context) (openBojunProductOracle, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.oracle != nil {
		return service.oracle, nil
	}
	connection, err := service.open(ctx, defaultOracleAdapterConfig(service.config))
	if err != nil {
		return nil, fmt.Errorf("%w: open default Oracle: %v", ErrOpenBojunProductUnavailable, err)
	}
	service.oracle = connection
	return connection, nil
}

func normalizeOpenBojunProductCode(value string) (string, error) {
	productCode := strings.TrimSpace(value)
	if productCode == "" || len([]rune(productCode)) > openBojunProductMaxCodeLength {
		return "", fmt.Errorf("%w: invalid productCode", ErrOpenBojunProductInvalidQuery)
	}
	return productCode, nil
}

func openBojunProductQueryTimeout(configured time.Duration) time.Duration {
	if configured <= 0 || configured > openBojunProductMaxQueryTimeout {
		return openBojunProductMaxQueryTimeout
	}
	return configured
}
