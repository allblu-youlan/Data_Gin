package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/tableboard"
	"github.com/godror/godror"
)

type aggregateRequest struct {
	metric      tableboard.Metric
	period      string
	filters     []tableboard.Filter
	groupField  string
	groupValues []string
}

func (r aggregateRequest) restrictionFields() []string {
	fields := []string{r.metric.TimeField}
	for _, f := range r.metric.Filters {
		fields = append(fields, f.Field)
	}
	for _, f := range r.filters {
		fields = append(fields, f.Field)
	}
	if r.groupField != "" {
		fields = append(fields, r.groupField)
	}
	return fields
}
func numericBoardType(t string) bool {
	return slices.Contains([]string{"NUMBER", "INTEGER", "FLOAT", "BINARY_FLOAT", "BINARY_DOUBLE"}, t)
}
func textBoardType(t string) bool {
	return slices.Contains([]string{"VARCHAR2", "NVARCHAR2", "CHAR", "NCHAR"}, t)
}
func quotedBoardField(f string) string { return `"` + f + `"` }
func validateMetricFields(m tableboard.Metric, rel boardRelation) error {
	for _, name := range m.Fields() {
		kind, err := reportoracle.NormalizeOperationDataType(rel.fieldType(name))
		if err != nil || kind == "CLOB" || kind == "NCLOB" || kind == "LONG" {
			return boardInvalid("字段 %s 已失效或类型不支持", name)
		}
	}
	if (m.Aggregate == "SUM" || m.Aggregate == "AVG") && !numericBoardType(rel.fieldType(m.ValueField)) {
		return boardInvalid("取值字段 %s 必须为数值类型", m.ValueField)
	}
	typ := rel.fieldType(m.TimeField)
	if m.TimeFormat == "YYYYMMDD" {
		if !numericBoardType(typ) && !textBoardType(typ) {
			return boardInvalid("营业日字段需要数值或文本类型")
		}
	} else if typ != "DATE" && !strings.HasPrefix(typ, "TIMESTAMP") {
		return boardInvalid("时间字段 %s 需要日期类型或显式营业日格式", m.TimeField)
	}
	for _, d := range m.Dimensions {
		if !textBoardType(rel.fieldType(d)) && !numericBoardType(rel.fieldType(d)) {
			return boardInvalid("维度 %s 需要文本或数值类型", d)
		}
	}
	return nil
}

type boardPredicate struct {
	parts []string
	args  []any
}

func (p *boardPredicate) bind(v any) string {
	name := "board" + strconv.Itoa(len(p.args)+1)
	p.args = append(p.args, sql.Named(name, v))
	return ":" + name
}
func (p *boardPredicate) where() string {
	if len(p.parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(p.parts, " AND ")
}
func boardFilters(rel boardRelation, filters []tableboard.Filter) (boardPredicate, error) {
	p := boardPredicate{args: slices.Clone(rel.args)}
	for _, filter := range filters {
		if filter.Op == "EQ" && rel.scopeField == filter.Field && !rel.scopeValues[boardGroupKey(filter.Value, rel.fieldType(filter.Field))] {
			return p, boardInvalid("筛选值不在当前账号的数据范围内")
		}
		op := "="
		if filter.Op == "NE" {
			op = "<>"
		}
		v, err := reportoracle.OperationValue(rel.fieldType(filter.Field), filter.Value)
		if err != nil {
			return p, boardInvalid("筛选字段 %s 的值格式无效", filter.Field)
		}
		p.parts = append(p.parts, quotedBoardField(filter.Field)+" "+op+" "+p.bind(v))
	}
	return p, nil
}

func boardDateExpression(m tableboard.Metric, rel boardRelation) string {
	field := quotedBoardField(m.TimeField)
	if m.TimeFormat == "" {
		return field
	}
	if numericBoardType(rel.fieldType(m.TimeField)) {
		field = "TO_CHAR(" + field + ", 'TM9')"
	}
	return "TO_DATE(" + field + ", 'FXYYYYMMDD')"
}

func addBoardTime(p *boardPredicate, m tableboard.Metric, rel boardRelation, start, end time.Time) {
	expr := boardDateExpression(m, rel)
	// DATE and unzoned TIMESTAMP store business-local wall time. Offset-aware
	// columns use explicit zoned bounds, independent of the Oracle session zone.
	bound := func(t time.Time) string {
		if strings.Contains(rel.fieldType(m.TimeField), "TIME ZONE") {
			return "TO_TIMESTAMP_TZ(" + p.bind(t.Format("2006-01-02 15:04:05.000000000 -07:00")) + ", 'YYYY-MM-DD HH24:MI:SS.FF9 TZH:TZM')"
		}
		return "TO_TIMESTAMP(" + p.bind(t.Format("2006-01-02 15:04:05.000000000")) + ", 'YYYY-MM-DD HH24:MI:SS.FF9')"
	}
	p.parts = append(p.parts, expr+" >= "+bound(start), expr+" < "+bound(end))
}
func compileBoardAggregate(r aggregateRequest, rel boardRelation, start, end time.Time) (string, []any, error) {
	m := r.metric
	if err := validateMetricFields(m, rel); err != nil {
		return "", nil, err
	}
	p, err := boardFilters(rel, append(slices.Clone(m.Filters), r.filters...))
	if err != nil {
		return "", nil, err
	}
	addBoardTime(&p, m, rel, start, end)
	agg := "COUNT(*)"
	switch m.Aggregate {
	case "SUM", "AVG":
		agg = m.Aggregate + "(" + quotedBoardField(m.ValueField) + ")"
	case "COUNT_DISTINCT":
		agg = "COUNT(DISTINCT " + quotedBoardField(m.ValueField) + ")"
	case "COUNT":
	default:
		return "", nil, boardInvalid("不支持的汇总方式")
	}
	projection := "COUNT(*), " + agg
	grouping := ""
	if r.groupField != "" {
		binds := []string{}
		for _, v := range r.groupValues {
			v, err := reportoracle.OperationValue(rel.fieldType(r.groupField), v)
			if err != nil {
				return "", nil, boardInvalid("维度值格式无效")
			}
			binds = append(binds, p.bind(v))
		}
		field := quotedBoardField(r.groupField)
		projection = field + ", " + projection
		p.parts = append(p.parts, field+" IN ("+strings.Join(binds, ", ")+")")
		grouping = " GROUP BY " + field
	}
	return "SELECT " + projection + " FROM " + rel.from + p.where() + grouping, p.args, nil
}

func checkBoardDaySamples(ctx context.Context, conn *reportoracle.Adapter, m tableboard.Metric, rel boardRelation, p boardPredicate) error {
	if m.TimeFormat == "" {
		return nil
	}
	field := quotedBoardField(m.TimeField)
	p.parts = append(slices.Clone(p.parts), field+" IS NOT NULL", "ROWNUM <= 32")
	rows, err := conn.QuerySelect(ctx, "SELECT "+field+" FROM "+rel.from+p.where(), append(p.args, godror.NumberAsString())...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var text string
		if err = rows.Scan(&text); err != nil {
			return err
		}
		if _, err = time.Parse("20060102", text); err != nil {
			return boardInvalid("字段 %s 的营业日样例不符合 YYYYMMDD", m.TimeField)
		}
	}
	return rows.Err()
}

func (r aggregateRequest) relation(ctx context.Context, conn *reportoracle.Adapter, actor uint) (boardRelation, error) {
	return NewReportBojunCatalogService().boardRelation(ctx, conn, actor, r.metric, r.metric.Fields(), r.restrictionFields(), false)
}
func boardGroupKey(value, typ string) string {
	if numericBoardType(typ) {
		if n, ok := new(big.Rat).SetString(value); ok {
			return n.RatString()
		}
	}
	if typ == "CHAR" || typ == "NCHAR" {
		return strings.TrimRight(value, " ")
	}
	return value
}
func (r aggregateRequest) execute(ctx context.Context, actor uint, asOf time.Time) (map[string]BoardCellResult, error) {
	start, end, err := r.metric.Bounds(r.period, asOf)
	if err != nil {
		return nil, err
	}
	var out map[string]BoardCellResult
	err = withBoardConnection(ctx, r.metric.DatasourceID, func(ctx context.Context, conn *reportoracle.Adapter) error {
		rel, err := r.relation(ctx, conn, actor)
		if err != nil {
			return err
		}
		// Keep permitted stores in this query even if a sibling cell lost access.
		denied := map[string]BoardCellResult{}
		if r.groupField != "" && r.groupField == rel.scopeField {
			var allowed []string
			for _, v := range r.groupValues {
				if rel.scopeValues[boardGroupKey(v, rel.fieldType(r.groupField))] {
					allowed = append(allowed, v)
				} else {
					denied[v] = BoardCellResult{Status: "error", Message: "门店不在当前账号的数据范围内"}
				}
			}
			r.groupValues = allowed
			if len(allowed) == 0 {
				out = denied
				return nil
			}
		}
		statement, args, err := compileBoardAggregate(r, rel, start, end)
		if err != nil {
			return err
		}
		rows, err := conn.QuerySelect(ctx, statement, append(args, godror.NumberAsString())...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out, err = readBoardAggregate(rows, r, rel, start, end)
		if err == nil {
			for k, v := range denied {
				out[k] = v
			}
		}
		return err
	})
	return out, err
}
func readBoardAggregate(rows *sql.Rows, r aggregateRequest, rel boardRelation, start, end time.Time) (map[string]BoardCellResult, error) {
	zero := "0"
	base := BoardCellResult{Status: "ok", Value: &zero, Start: start.Format(time.RFC3339Nano), End: end.Format(time.RFC3339Nano)}
	out := map[string]BoardCellResult{}
	var err error
	seen := map[string]BoardCellResult{}
	for rows.Next() {
		var k string
		var amount sql.NullString
		res := base
		dst := []any{&res.Matched, &amount}
		if r.groupField != "" {
			dst = append([]any{&k}, dst...)
		}
		if err = rows.Scan(dst...); err != nil {
			return nil, err
		}
		if amount.Valid {
			res.Value = &amount.String
		} else if res.Matched > 0 {
			res.Status = "null"
			res.Value = nil
			res.Message = "取值字段全部为空"
		}
		seen[boardGroupKey(k, rel.fieldType(r.groupField))] = res
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	keys := r.groupValues
	if r.groupField == "" {
		keys = []string{""}
	}
	for _, k := range keys {
		v, ok := seen[boardGroupKey(k, rel.fieldType(r.groupField))]
		if !ok {
			v = base
		}
		out[k] = v
	}
	return out, nil
}
func safeBoardMessage(err error) string {
	if errors.Is(err, ErrBoardSourceInvalid) {
		return err.Error()
	}
	return "读取失败，请检查数据源连接、字段与数据格式"
}
