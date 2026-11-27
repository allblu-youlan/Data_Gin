package data_svc

import (
	"context"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/internal/tableboard"
	"strconv"
	"time"
)

func boardInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBoardSourceInvalid, fmt.Sprintf(format, args...))
}

func withBoardConnection(ctx context.Context, dsID uint, f func(context.Context, *reportoracle.Adapter) error) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	repo := reportrepo.New()
	ds, err := repo.GetReportDatasource(ctx, dsID)
	if err != nil {
		return classifyReportDatasourceStoreError(err)
	}
	if !ds.Enabled {
		return boardInvalid("连接已停用")
	}
	if ds.QueryTimeoutSeconds > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, time.Duration(ds.QueryTimeoutSeconds)*time.Second)
		defer stop()
	}
	release, err := repo.AcquireSelectDatasourceSlot(ctx, ds.ID)
	if err != nil {
		return err
	}
	defer release()
	password, err := (reportsecret.EnvironmentKeyring{}).Decrypt(ds.CredentialKeyVersion, ds.PasswordCiphertext)
	if err != nil {
		return ErrReportDatasourceCredentialUnavailable
	}
	conn, done, err := acquireSelectOracle(ctx, *ds, password)
	if err != nil {
		return ErrReportDatasourceOracleUnavailable
	}
	defer done()
	return f(ctx, conn)
}

type boardRelation struct {
	from   string
	args   []any
	schema []reportoracle.ResultColumn
}

func (r boardRelation) fieldType(name string) string {
	for _, col := range r.schema {
		if col.Name == name {
			return col.DataType
		}
	}
	return ""
}

func resolveBoardRelation(ctx context.Context, conn *reportoracle.Adapter, actor uint, m tableboard.Metric, fields, restricted []string, detail bool) (boardRelation, error) {
	var rel boardRelation
	records, err := reportrepo.New().BoardManagedTables(ctx, m.DatasourceID, m.Owner, m.TableName)
	if err != nil {
		return rel, err
	}
	if m.BojunTableID == 0 && len(records) == 0 {
		schema, err := conn.InspectResultTable(ctx, reportoracle.ResultTableRef{Owner: m.Owner, Name: m.TableName})
		if err != nil {
			return rel, err
		}
		rel = boardRelation{from: `"` + m.Owner + `"."` + m.TableName + `"`, schema: schema}
		return rel, nil
	}
	tableID := m.BojunTableID
	if detail && tableID == 0 && len(records) == 1 {
		tableID = records[0].ID
	}
	for _, row := range records {
		if row.ID != tableID {
			continue
		}
		if !row.SourceActive || row.ValidationStatus != "VALID" || row.SourceFilter != "" {
			break
		}
		dto, err := NewReportBojunCatalogService().dto(ctx, row)
		if err != nil {
			return rel, err
		}
		t := dto.Table
		if t.Owner != m.Owner || t.PhysicalTable != m.TableName {
			break
		}
		ids := m.ColumnIDs
		if detail {
			ids = map[string]uint{}
			for _, col := range t.Columns {
				if col.IsPhysical && col.IsSelectable {
					n, _ := strconv.ParseUint(col.ID, 10, 64)
					ids[col.DBName] = uint(n)
				}
			}
		}
		plan, err := reportdataset.BoardRelation(t, actor, fields, ids, restricted)
		if err != nil {
			return rel, boardInvalid("%s", err)
		}
		if err = conn.ValidateBojunMetadata(ctx, t); err != nil {
			return rel, boardInvalid("受管表字段已变化，请重新核对目录")
		}
		for _, col := range t.Columns {
			if col.IsPhysical && col.IsSelectable {
				rel.schema = append(rel.schema, reportoracle.ResultColumn{Name: col.DBName, DataType: col.OracleType})
			}
		}
		rel.from = plan.SQL
		rel.args = plan.Args
		return rel, nil
	}
	return rel, boardInvalid("请从有效的伯俊目录重新选择表和字段")
}
