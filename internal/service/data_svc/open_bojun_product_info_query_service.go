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
	ErrOpenBojunProductInfoForbidden    = errors.New("open bojun product info: forbidden")
	ErrOpenBojunProductInfoInvalidQuery = errors.New("open bojun product info: invalid query")
	ErrOpenBojunProductInfoUnavailable  = errors.New("open bojun product info: unavailable")
)

type OpenBojunProductInfoQueryService struct {
	provider    *OpenBojunOracleProvider
	permissions openBojunQueryPermissionReader
	now         func() time.Time
}

// OpenBojunProductInfoDTO 的字段含义以 DIM_PRODUCT_INFO_INTERFACE 的列备注为准。
// 数值以字符串返回，数据库 NULL 保留为 null，日期时间不附加数据库未提供的时区。
type OpenBojunProductInfoDTO struct {
	ID                      *string `json:"id"`                      // ID（数据库未设置备注）。
	NoIsActive              *string `json:"noIsActive"`              // 条码是否可用（NO_ISACTIVE）。
	ProductCode             *string `json:"productCode"`             // 商品编码（SKU）。
	NicknameDescription     *string `json:"nicknameDescription"`     // 吊牌商品名称（NICKNAME_DESCRIPTION）。
	StructureName           *string `json:"structureName"`           // 商品结构名称（PRO_JG_NAME）。
	ProductName             *string `json:"productName"`             // 款号（PRO_9_NAME）。
	ProductColor            *string `json:"productColor"`            // 款色号（SKC_13）。
	ColorName               *string `json:"colorName"`               // 颜色名称（COLOR_NAME）。
	ColorCode               *string `json:"colorCode"`               // 颜色编码（COLOR_CODE）。
	SizeName                *string `json:"sizeName"`                // 尺码名称（SIZE_NAME）。
	SizeCode                *string `json:"sizeCode"`                // 尺码编码（SIZE__NAME）。
	PriceList               *string `json:"priceList"`               // 吊牌价（PRICELIST）。
	ProductAttribute        *string `json:"productAttribute"`        // 产品属性（PRO_ATTR）。
	ProductLine             *string `json:"productLine"`             // 产品线（PRO_SERIESNAME_DIM3）。
	TopBottom               *string `json:"topBottom"`               // 上下装（PRO_SXZ_DIM4）。
	CategoryLarge           *string `json:"categoryLarge"`           // 大类（PRO_DA_DIM5）。
	CategoryMiddle          *string `json:"categoryMiddle"`          // 中类（PRO_Z_DIM6）。
	CategorySmall           *string `json:"categorySmall"`           // 小类（PRO_X_DIM7）。
	Year                    *string `json:"year"`                    // 年份（PRO_YEAR_DIM2）。
	Season                  *string `json:"season"`                  // 季节（PRO_SEASON_DC2）。
	Wave                    *string `json:"wave"`                    // 波段（PRO_WAVE_DC1）。
	MaterialName            *string `json:"materialName"`            // 面料名称（PRO_MATERIAL_DC3）。
	MarketDate              *string `json:"marketDate"`              // 上市日期，保留原数值（MARKETDATE）。
	ProductGender           *string `json:"productGender"`           // 款性别（PRO_GEN_DIM9）。
	ColorGender             *string `json:"colorGender"`             // 色性别（COLOR_GEN_DC4）。
	ProductSizeRange        *string `json:"productSizeRange"`        // 款尺码范围（PRO_SIZE_RANGE）。
	ColorSizeRange          *string `json:"colorSizeRange"`          // 色尺码范围（COLOR_SIZE_RANGE）。
	SeriesDescription       *string `json:"seriesDescription"`       // 推广系列（SERIES_DESCRIPTION）。
	CostPrice               *string `json:"costPrice"`               // 成本价（PERCOST）。
	CostRate                *string `json:"costRate"`                // 毛利率（RATE_COST）。
	SellingPointDescription *string `json:"sellingPointDescription"` // 核心卖点方向（SELLING_POINT_DESCRIPTION）。
	Spu8                    *string `json:"spu8"`                    // 8位款号（SPU_8）。
	Skc12                   *string `json:"skc12"`                   // 12位款色号（SKC_12）。
	Nickname                *string `json:"nickname"`                // Nickname（NICKNAME，原备注）。
	InnerOuter              *string `json:"innerOuter"`              // 内外搭（PRO_M_DIM11_ID）。
	AnalysisCategoryLarge   *string `json:"analysisCategoryLarge"`   // 分析大类（PRO_M_DIM12_ID）。
	AnalysisCategoryMiddle  *string `json:"analysisCategoryMiddle"`  // 分析中类（PRO_M_DIM13_ID）。
	SupplierName            *string `json:"supplierName"`            // 供应商名称（SUPPLIER_NAME）。
	OfflineWave             *string `json:"offlineWave"`             // 线下波段（OFFLINE_WAVE）。
	AgeRange                *string `json:"ageRange"`                // 年龄段（PRO_M_DIM14_ID）。
	ProductLevel            *string `json:"productLevel"`            // 产品层级（PRO_M_DIM15_ID）。
	ProductStyle            *string `json:"productStyle"`            // 产品风格（PRO_M_DIM16_ID）。
	Created                 *string `json:"created"`                 // CREATED（数据库未设置备注），YYYY-MM-DD HH:mm:ss。
	Modified                *string `json:"modified"`                // MODIFIED（数据库未设置备注），YYYY-MM-DD HH:mm:ss。
}

type OpenBojunProductInfoQueryDTO struct {
	Items      []OpenBojunProductInfoDTO `json:"items"`
	Pagination OpenBojunQueryPagination  `json:"pagination"`
}

func NewOpenBojunProductInfoQueryService(provider *OpenBojunOracleProvider) *OpenBojunProductInfoQueryService {
	return newOpenBojunProductInfoQueryService(provider, newAccountPermissionChecker(database.DB), time.Now)
}

func newOpenBojunProductInfoQueryService(provider *OpenBojunOracleProvider, permissions openBojunQueryPermissionReader, now func() time.Time) *OpenBojunProductInfoQueryService {
	if provider == nil || permissions == nil || now == nil {
		panic("open bojun product info query service: nil dependency")
	}
	return &OpenBojunProductInfoQueryService{provider: provider, permissions: permissions, now: now}
}

func (service *OpenBojunProductInfoQueryService) Query(ctx context.Context, actorUserID uint, request requestbody.OpenBojunProductInfoQueryRequest) (*OpenBojunProductInfoQueryDTO, error) {
	if ctx == nil {
		return nil, ErrOpenBojunProductInfoInvalidQuery
	}
	if actorUserID == 0 {
		return nil, ErrOpenBojunProductInfoForbidden
	}
	// 与现有商品查询保持相同权限。
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, model.PermissionBojunOrderRead, service.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %w", ErrOpenBojunProductInfoUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenBojunProductInfoForbidden
	}
	query := reportoracle.BojunProductInfoQuery{ProductCode: request.ProductCode, ProductColor: request.ProductColor, ProductName: request.ProductName}
	if len(query.ProductColor) > 20 || len(query.ProductName) > 20 {
		return nil, ErrOpenBojunProductInfoInvalidQuery
	}
	productFilters := 0
	for _, values := range []*[]string{&query.ProductCode, &query.ProductColor, &query.ProductName} {
		normalized, err := normalizeOpenBojunStockProductValues(*values)
		if err != nil {
			return nil, ErrOpenBojunProductInfoInvalidQuery
		}
		*values = normalized
		if len(normalized) > 0 {
			productFilters++
		}
	}
	if productFilters > 1 {
		return nil, ErrOpenBojunProductInfoInvalidQuery
	}
	// 全量商品可继续翻页，不受库存查询的第 1000 页上限限制。
	pagination, err := normalizeOpenBojunQueryPaginationWithMaxPage(request.Page, request.PageSize, int(^uint(0)>>1)/100)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenBojunProductInfoInvalidQuery, err)
	}
	query.Offset, query.PageSize = (pagination.Page-1)*pagination.PageSize, pagination.PageSize
	queryCtx, cancel := context.WithTimeout(ctx, service.provider.queryTimeout(10*time.Second))
	defer cancel()
	connection, err := service.provider.connection(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenBojunProductInfoUnavailable, err)
	}
	rows, err := connection.QueryBojunProductInfos(queryCtx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: query: %w", ErrOpenBojunProductInfoUnavailable, err)
	}
	pagination.HasMore = len(rows) > pagination.PageSize
	if pagination.HasMore {
		rows = rows[:pagination.PageSize]
	}
	items := make([]OpenBojunProductInfoDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, OpenBojunProductInfoDTO(row))
	}
	return &OpenBojunProductInfoQueryDTO{Items: items, Pagination: pagination}, nil
}
