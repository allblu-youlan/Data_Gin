package data_svc

import (
	"context"
	"strconv"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
)

const oracleExcelImportIdentifierKey = "excel_oracle_import_identifier"

func nextOracleExcelImportIdentifier(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	value, err := data_dao.NewRuntimeConfigDAO().NextSequence(ctx, oracleExcelImportIdentifierKey)
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(value, 10), nil
}
