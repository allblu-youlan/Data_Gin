package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/godror/godror"
)

const BojunProductInfoTable = "YL_DBS.DIM_PRODUCT_INFO_INTERFACE"

const bojunProductInfoSelectSQL = `
SELECT a.ID,
       a.NO_ISACTIVE,
       a.SKU,
       a.NICKNAME_DESCRIPTION,
       a.PRO_JG_NAME,
       a.PRO_9_NAME,
       a.SKC_13,
       a.COLOR_NAME,
       a.COLOR_CODE,
       a.SIZE_NAME,
       a.SIZE__NAME,
       a.PRICELIST,
       a.PRO_ATTR,
       a.PRO_SERIESNAME_DIM3,
       a.PRO_SXZ_DIM4,
       a.PRO_DA_DIM5,
       a.PRO_Z_DIM6,
       a.PRO_X_DIM7,
       a.PRO_YEAR_DIM2,
       a.PRO_SEASON_DC2,
       a.PRO_WAVE_DC1,
       a.PRO_MATERIAL_DC3,
       a.MARKETDATE,
       a.PRO_GEN_DIM9,
       a.COLOR_GEN_DC4,
       a.PRO_SIZE_RANGE,
       a.COLOR_SIZE_RANGE,
       a.SERIES_DESCRIPTION,
       a.PERCOST,
       a.RATE_COST,
       a.SELLING_POINT_DESCRIPTION,
       a.SPU_8,
       a.SKC_12,
       a.NICKNAME,
       a.PRO_M_DIM11_ID,
       a.PRO_M_DIM12_ID,
       a.PRO_M_DIM13_ID,
       a.SUPPLIER_NAME,
       a.OFFLINE_WAVE,
       a.PRO_M_DIM14_ID,
       a.PRO_M_DIM15_ID,
       a.PRO_M_DIM16_ID,
       TO_CHAR(a.CREATED, 'YYYY-MM-DD HH24:MI:SS'),
       TO_CHAR(a.MODIFIED, 'YYYY-MM-DD HH24:MI:SS')
FROM ` + BojunProductInfoTable + ` a`

type BojunProductInfoQuery struct {
	ProductCode  []string
	ProductColor []string
	ProductName  []string
	PageSize     int
	Offset       int
}

type BojunProductInfoRow struct {
	ID                      *string
	NoIsActive              *string
	ProductCode             *string
	NicknameDescription     *string
	StructureName           *string
	ProductName             *string
	ProductColor            *string
	ColorName               *string
	ColorCode               *string
	SizeName                *string
	SizeCode                *string
	PriceList               *string
	ProductAttribute        *string
	ProductLine             *string
	TopBottom               *string
	CategoryLarge           *string
	CategoryMiddle          *string
	CategorySmall           *string
	Year                    *string
	Season                  *string
	Wave                    *string
	MaterialName            *string
	MarketDate              *string
	ProductGender           *string
	ColorGender             *string
	ProductSizeRange        *string
	ColorSizeRange          *string
	SeriesDescription       *string
	CostPrice               *string
	CostRate                *string
	SellingPointDescription *string
	Spu8                    *string
	Skc12                   *string
	Nickname                *string
	InnerOuter              *string
	AnalysisCategoryLarge   *string
	AnalysisCategoryMiddle  *string
	SupplierName            *string
	OfflineWave             *string
	AgeRange                *string
	ProductLevel            *string
	ProductStyle            *string
	Created                 *string
	Modified                *string
}

func (adapter *Adapter) QueryBojunProductInfos(ctx context.Context, query BojunProductInfoQuery) ([]BojunProductInfoRow, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query bojun product infos: adapter is closed")
	}
	if ctx == nil {
		return nil, fmt.Errorf("query bojun product infos: invalid context")
	}
	statement, arguments, err := buildBojunProductInfoQuery(query)
	if err != nil {
		return nil, err
	}
	arguments = append(arguments, godror.NumberAsString())
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query bojun product infos: %w", err)
	}
	defer rows.Close()

	result := make([]BojunProductInfoRow, 0, query.PageSize+1)
	for rows.Next() {
		var id, noIsActive, productCode, nicknameDescription, structureName sql.NullString
		var productName, productColor, colorName, colorCode, sizeName, sizeCode sql.NullString
		var priceList, productAttribute, productLine, topBottom sql.NullString
		var categoryLarge, categoryMiddle, categorySmall, year, season, wave sql.NullString
		var materialName, marketDate, productGender, colorGender, productSizeRange, colorSizeRange sql.NullString
		var seriesDescription, costPrice, costRate, sellingPointDescription, spu8, skc12, nickname sql.NullString
		var innerOuter, analysisCategoryLarge, analysisCategoryMiddle, supplierName, offlineWave sql.NullString
		var ageRange, productLevel, productStyle, created, modified sql.NullString
		if err := rows.Scan(
			&id, &noIsActive, &productCode, &nicknameDescription, &structureName,
			&productName, &productColor, &colorName, &colorCode, &sizeName, &sizeCode,
			&priceList, &productAttribute, &productLine, &topBottom,
			&categoryLarge, &categoryMiddle, &categorySmall, &year, &season, &wave,
			&materialName, &marketDate, &productGender, &colorGender, &productSizeRange, &colorSizeRange,
			&seriesDescription, &costPrice, &costRate, &sellingPointDescription, &spu8, &skc12, &nickname,
			&innerOuter, &analysisCategoryLarge, &analysisCategoryMiddle, &supplierName, &offlineWave,
			&ageRange, &productLevel, &productStyle, &created, &modified,
		); err != nil {
			return nil, fmt.Errorf("scan bojun product info: %w", err)
		}
		result = append(result, BojunProductInfoRow{
			ID: nullableBojunString(id), NoIsActive: nullableBojunString(noIsActive),
			ProductCode: nullableBojunString(productCode), NicknameDescription: nullableBojunString(nicknameDescription),
			StructureName: nullableBojunString(structureName), ProductName: nullableBojunString(productName),
			ProductColor: nullableBojunString(productColor), ColorName: nullableBojunString(colorName),
			ColorCode: nullableBojunString(colorCode), SizeName: nullableBojunString(sizeName),
			SizeCode: nullableBojunString(sizeCode), PriceList: nullableBojunString(priceList),
			ProductAttribute: nullableBojunString(productAttribute), ProductLine: nullableBojunString(productLine),
			TopBottom: nullableBojunString(topBottom), CategoryLarge: nullableBojunString(categoryLarge),
			CategoryMiddle: nullableBojunString(categoryMiddle), CategorySmall: nullableBojunString(categorySmall),
			Year: nullableBojunString(year), Season: nullableBojunString(season), Wave: nullableBojunString(wave),
			MaterialName: nullableBojunString(materialName), MarketDate: nullableBojunString(marketDate),
			ProductGender: nullableBojunString(productGender), ColorGender: nullableBojunString(colorGender),
			ProductSizeRange: nullableBojunString(productSizeRange), ColorSizeRange: nullableBojunString(colorSizeRange),
			SeriesDescription: nullableBojunString(seriesDescription), CostPrice: nullableBojunString(costPrice),
			CostRate: nullableBojunString(costRate), SellingPointDescription: nullableBojunString(sellingPointDescription),
			Spu8: nullableBojunString(spu8), Skc12: nullableBojunString(skc12), Nickname: nullableBojunString(nickname),
			InnerOuter: nullableBojunString(innerOuter), AnalysisCategoryLarge: nullableBojunString(analysisCategoryLarge),
			AnalysisCategoryMiddle: nullableBojunString(analysisCategoryMiddle), SupplierName: nullableBojunString(supplierName),
			OfflineWave: nullableBojunString(offlineWave), AgeRange: nullableBojunString(ageRange),
			ProductLevel: nullableBojunString(productLevel), ProductStyle: nullableBojunString(productStyle),
			Created: nullableBojunString(created), Modified: nullableBojunString(modified),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bojun product infos: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close bojun product infos: %w", err)
	}
	return result, nil
}

func buildBojunProductInfoQuery(query BojunProductInfoQuery) (string, []any, error) {
	if query.PageSize < 1 || query.PageSize > 100 || query.Offset < 0 {
		return "", nil, fmt.Errorf("query bojun product infos: invalid pagination")
	}
	var productValues []string
	var productColumn string
	productFilters := 0
	for _, filter := range []struct {
		values []string
		column string
		limit  int
	}{
		{query.ProductCode, "a.SKU", 100},
		{query.ProductColor, "a.SKC_13", 20},
		{query.ProductName, "a.PRO_9_NAME", 20},
	} {
		if len(filter.values) > filter.limit {
			return "", nil, fmt.Errorf("query bojun product infos: too many product filters")
		}
		if len(filter.values) > 0 {
			productFilters++
			productValues, productColumn = filter.values, filter.column
		}
	}
	if productFilters > 1 {
		return "", nil, fmt.Errorf("query bojun product infos: conflicting product filters")
	}

	statement := bojunProductInfoSelectSQL
	arguments := make([]any, 0, len(productValues)+2)
	if productFilters == 1 {
		placeholders := make([]string, 0, len(productValues))
		seen := make(map[string]struct{}, len(productValues))
		for _, value := range productValues {
			value = strings.TrimSpace(value)
			if value == "" || len(value) > 80 {
				return "", nil, fmt.Errorf("query bojun product infos: invalid product value")
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			arguments = append(arguments, value)
			placeholders = append(placeholders, ":"+strconv.Itoa(len(arguments)))
		}
		statement += "\nWHERE " + productColumn + " IN (" + strings.Join(placeholders, ", ") + ")"
	}
	// godror 按 SQL 中的位置绑定匿名参数，分页参数放在全部商品条件之后。
	statement += fmt.Sprintf("\nORDER BY a.ID\nOFFSET :%d ROWS FETCH NEXT :%d ROWS ONLY", len(arguments)+1, len(arguments)+2)
	arguments = append(arguments, query.Offset, query.PageSize+1)
	return statement, arguments, nil
}
