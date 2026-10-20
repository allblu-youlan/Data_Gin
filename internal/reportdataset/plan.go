package reportdataset

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type sqlBuilder struct {
	boundColumns map[string]map[string]json.RawMessage
	compiled     Compiled
	aliases      map[string]Table
	sqlAliases   map[string]string
	parameters   map[string]json.RawMessage
	args         []any
	validate     bool
	leaves       int
}

func (c Compiled) Build(parameters map[string]json.RawMessage, inputs QueryInputs, actor string, validate bool) (Plan, error) {
	b := sqlBuilder{compiled: c, aliases: map[string]Table{"base": c.Tables[c.Query.Base.TableID]}, sqlAliases: map[string]string{"base": "B0"}, parameters: parameters, validate: validate}
	for i, r := range c.Query.Relations {
		b.aliases[r.ID] = c.Tables[r.TargetTableID]
		b.sqlAliases[r.ID] = fmt.Sprintf("R%d", i+1)
	}
	for _, t := range b.aliases {
		if !t.Config.Enabled {
			return Plan{}, queryError("table is disabled")
		}
	}
	selects := []string{}
	names := []string{}
	for _, p := range c.Projections {
		expr, _, e := b.projectionColumn(p.AliasID, p.ColumnID)
		if e != nil {
			return Plan{}, e
		}
		selects = append(selects, expr+" AS "+p.Name)
		names = append(names, p.Name)
	}
	from, e := objectName(b.aliases["base"])
	if e != nil {
		return Plan{}, e
	}
	statement := "SELECT " + strings.Join(selects, ", ") + " FROM " + from + " B0"
	seen := map[string]bool{"base": true}
	for _, r := range c.Query.Relations {
		seen[r.ID] = true
		target, e := objectName(b.aliases[r.ID])
		if e != nil {
			return Plan{}, e
		}
		on := []string{}
		for _, k := range r.KeyBindings {
			src, _, e := b.column(r.FromAliasID, k.SourceColumnID)
			if e != nil {
				return Plan{}, e
			}
			dst, _, e := b.column(r.ID, k.TargetColumnID)
			if e != nil {
				return Plan{}, e
			}
			on = append(on, dst+" = "+src)
		}
		if e := walkCondition(r.MatchConditions, func(cond Condition) error {
			if !seen[cond.AliasID] || cond.Value != nil && cond.Value.Kind == "FIELD" && !seen[cond.Value.AliasID] {
				return queryError("relation predicate references a later relation")
			}
			return nil
		}); e != nil {
			return Plan{}, e
		}
		pred, e := b.condition(r.MatchConditions, 0, false)
		if e != nil {
			return Plan{}, e
		}
		if pred != "" {
			on = append(on, pred)
		}
		scope, e := b.scope(r.ID, actor)
		if e != nil {
			return Plan{}, e
		}
		on = append(on, scope)
		fixed, e := b.fixed(r.ID)
		if e != nil {
			return Plan{}, e
		}
		if fixed != "" {
			on = append(on, fixed)
		}
		statement += " LEFT JOIN " + target + " " + b.sqlAliases[r.ID] + " ON (" + strings.Join(on, " AND ") + ")"
	}
	where := []string{}
	scope, e := b.scope("base", actor)
	if e != nil {
		return Plan{}, e
	}
	where = append(where, scope)
	fixed, e := b.fixed("base")
	if e != nil {
		return Plan{}, e
	}
	if fixed != "" {
		where = append(where, fixed)
	}
	for _, condition := range []struct {
		value   *Condition
		runtime bool
	}{{c.Query.Where, false}, {inputs.Where, true}} {
		pred, e := b.condition(condition.value, 0, condition.runtime)
		if e != nil {
			return Plan{}, e
		}
		if pred != "" {
			where = append(where, pred)
		}
	}
	for alias, t := range b.aliases {
		for _, id := range t.Config.RequiredFilterColumns {
			found := false
			for _, group := range []*Condition{c.Query.Where, inputs.Where} {
				_ = walkCondition(group, func(cond Condition) error {
					if cond.AliasID == alias && cond.ColumnID == id && cond.Operator != "IS_NULL" && cond.Operator != "IS_NOT_NULL" {
						found = true
					}
					return nil
				})
			}
			if !found {
				return Plan{}, queryError("required source restriction is missing")
			}
		}
	}
	orders := c.Query.OrderBy
	if len(inputs.OrderBy) > 0 {
		orders = inputs.OrderBy
	}
	if len(orders) > 3 {
		return Plan{}, queryError("at most 3 sort fields are allowed")
	}
	orderSQL := []string{}
	used := map[string]bool{}
	for _, o := range orders {
		if len(inputs.OrderBy) > 0 {
			allowed := false
			for _, a := range c.Query.AllowedSortFields {
				if a.AliasID == o.AliasID && a.ColumnID == o.ColumnID {
					allowed = true
				}
			}
			if !allowed {
				return Plan{}, queryError("runtime sort field is not allowed")
			}
		}
		if o.Direction != "ASC" && o.Direction != "DESC" {
			return Plan{}, queryError("sort direction is invalid")
		}
		col, metadata, e := b.column(o.AliasID, o.ColumnID)
		if e != nil {
			return Plan{}, e
		}
		if !SortableColumn(metadata) {
			return Plan{}, queryError("LOB columns cannot be sorted")
		}
		if used[col] {
			return Plan{}, queryError("duplicate sort field")
		}
		used[col] = true
		orderSQL = append(orderSQL, col+" "+o.Direction+" NULLS LAST")
	}
	for _, id := range b.aliases["base"].PrimaryKeys {
		col, _, e := b.column("base", id)
		if e != nil {
			return Plan{}, e
		}
		if !used[col] {
			orderSQL = append(orderSQL, col+" ASC NULLS LAST")
		}
	}
	statement += " WHERE (" + strings.Join(where, ") AND (") + ") ORDER BY " + strings.Join(orderSQL, ", ")
	return Plan{SQL: statement, Args: b.args, Columns: names}, nil
}
func objectName(t Table) (string, error) {
	owner, e := Identifier(t.Owner)
	if e != nil {
		return "", e
	}
	table, e := Identifier(t.PhysicalTable)
	if e != nil {
		return "", e
	}
	if t.DBLink != "" {
		link, e := Identifier(t.DBLink)
		if e != nil {
			return "", e
		}
		table += "@" + link
	}
	return owner + "." + table, nil
}
func (b *sqlBuilder) column(alias, id string) (string, Column, error) {
	return b.columnAccess(alias, id, false)
}
func (b *sqlBuilder) projectionColumn(alias, id string) (string, Column, error) {
	return b.columnAccess(alias, id, true)
}
func (b *sqlBuilder) columnAccess(alias, id string, detail bool) (string, Column, error) {
	t, ok := b.aliases[alias]
	if !ok {
		return "", Column{}, queryError("unknown table alias")
	}
	c, e := availableColumn(t, id, false)
	if e != nil && detail {
		// Detail-only fields may be projected after explicit approval during compilation.
		c, e = availableColumn(t, id, true)
		if e != nil {
			return "", c, e
		}
	}
	if e != nil {
		return "", c, e
	}
	if b.sqlAliases[alias] == "" {
		raw, ok := b.boundColumns[alias][id]
		if !ok {
			return "", c, queryError("relation dependency value is required")
		}
		if string(raw) == "null" {
			name := "b" + strconv.Itoa(len(b.args)+1)
			b.args = append(b.args, sql.Named(name, nil))
			return ":" + name, c, nil
		}
		value, err := b.bind(raw, c)
		return value, c, err
	}
	return b.sqlAliases[alias] + "." + c.DBName, c, nil
}
func (b *sqlBuilder) fixed(alias string) (string, error) {
	t := b.aliases[alias]
	if t.FixedCondition == nil {
		return "", nil
	}
	raw, _ := json.Marshal(t.FixedCondition)
	var cond Condition
	_ = json.Unmarshal(raw, &cond)
	var apply func(*Condition)
	apply = func(c *Condition) {
		if c.Logic != "" {
			for i := range c.Items {
				apply(&c.Items[i])
			}
		} else {
			c.AliasID = alias
		}
	}
	apply(&cond)
	return b.condition(&cond, 0, false)
}
func (b *sqlBuilder) scope(alias, actor string) (string, error) {
	s := b.aliases[alias].Config.DataScope
	switch s.Mode {
	case "GLOBAL_APPROVED":
		return "1=1", nil
	case "SCOPE_COLUMN":
		col, c, e := b.column(alias, s.ColumnID)
		if e != nil {
			return "", e
		}
		vals := s.UserValues[actor]
		if len(vals) == 0 {
			return "1=0", nil
		}
		if len(vals) > 1000 {
			return "", queryError("data scope exceeds configured limit")
		}
		parts := []string{}
		for _, v := range vals {
			raw, _ := json.Marshal(v)
			bind, e := b.bind(raw, c)
			if e != nil {
				return "", e
			}
			parts = append(parts, bind)
		}
		return col + " IN (" + strings.Join(parts, ",") + ")", nil
	default:
		return "", queryError("unapproved data scope")
	}
}
func (b *sqlBuilder) condition(c *Condition, depth int, runtime bool) (string, error) {
	if c == nil {
		return "", nil
	}
	if depth > 3 {
		return "", queryError("condition depth exceeds 3")
	}
	if c.Logic != "" {
		if c.Logic != "AND" && c.Logic != "OR" || c.ColumnID != "" || c.Operator != "" {
			return "", queryError("invalid condition group")
		}
		parts := []string{}
		for _, item := range c.Items {
			pred, e := b.condition(&item, depth+1, runtime)
			if e != nil {
				return "", e
			}
			if pred != "" {
				parts = append(parts, pred)
			}
		}
		if len(parts) == 0 {
			return "", nil
		}
		return "(" + strings.Join(parts, " "+c.Logic+" ") + ")", nil
	}
	b.leaves++
	if b.leaves > 20 {
		return "", queryError("condition leaves exceed 20")
	}
	expr, col, e := b.column(c.AliasID, c.ColumnID)
	if e != nil {
		return "", e
	}
	if !contains(ColumnOperators(col), c.Operator) {
		return "", queryError("operator is incompatible with column")
	}
	if runtime {
		allowed := false
		for _, a := range b.compiled.Query.AllowedFilters {
			if a.AliasID == c.AliasID && a.ColumnID == c.ColumnID && contains(a.Operators, c.Operator) {
				allowed = true
			}
		}
		if !allowed {
			return "", queryError("runtime filter is not allowed")
		}
	}
	if c.Operator == "IS_NULL" || c.Operator == "IS_NOT_NULL" {
		if c.Value != nil {
			return "", queryError("NULL operators take no value")
		}
		if c.Operator == "IS_NULL" {
			return expr + " IS NULL", nil
		}
		return expr + " IS NOT NULL", nil
	}
	if c.Value == nil {
		return "", queryError("condition value is required")
	}
	v := c.Value
	if v.Kind == "FIELD" {
		if runtime || !contains([]string{"EQ", "NE", "GT", "GTE", "LT", "LTE"}, c.Operator) {
			return "", queryError("field comparison is not supported here")
		}
		rhs, target, e := b.column(v.AliasID, v.ColumnID)
		if e != nil {
			return "", e
		}
		if col.LogicalType != target.LogicalType && !(numeric(col.LogicalType) && numeric(target.LogicalType)) {
			return "", queryError("field comparison types differ")
		}
		return expr + " " + binaryOperator(c.Operator) + " " + rhs, nil
	}
	raw := v.Value
	if v.Kind == "PARAMETER" {
		var ok bool
		raw, ok = b.parameters[v.Code]
		if !ok {
			if !b.validate {
				return "", queryError("missing parameter " + v.Code)
			}
			if col.LogicalType == "integer" || col.LogicalType == "decimal" {
				raw = json.RawMessage(`"0"`)
			} else if col.LogicalType == "date" {
				raw = json.RawMessage(`"2000-01-01"`)
			} else if col.LogicalType == "datetime" {
				raw = json.RawMessage(`"2000-01-01T00:00:00Z"`)
			} else {
				raw = json.RawMessage(`""`)
			}
			if c.Operator == "IN" || c.Operator == "NOT_IN" || c.Operator == "BETWEEN" {
				raw = json.RawMessage("[" + string(raw) + "," + string(raw) + "]")
			}
		}
	} else if v.Kind != "CONSTANT" {
		return "", queryError("unknown value source")
	}
	if c.Operator == "IN" || c.Operator == "NOT_IN" || c.Operator == "BETWEEN" {
		var list []json.RawMessage
		if e := json.Unmarshal(raw, &list); e != nil || len(list) > 100 {
			return "", queryError("invalid condition collection")
		}
		if c.Operator == "BETWEEN" && len(list) != 2 {
			return "", queryError("BETWEEN requires two values")
		}
		if len(list) == 0 {
			if c.Operator == "IN" {
				return "1=0", nil
			}
			return "1=1", nil
		}
		args := []string{}
		for _, value := range list {
			arg, e := b.bind(value, col)
			if e != nil {
				return "", e
			}
			args = append(args, arg)
		}
		if c.Operator == "BETWEEN" {
			return expr + " BETWEEN " + args[0] + " AND " + args[1], nil
		}
		op := "IN"
		if c.Operator == "NOT_IN" {
			op = "NOT IN"
		}
		return expr + " " + op + " (" + strings.Join(args, ",") + ")", nil
	}
	if c.Operator == "PREFIX" || c.Operator == "CONTAINS" {
		var value string
		if e := json.Unmarshal(raw, &value); e != nil {
			return "", queryError("text search expects a string")
		}
		value = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
		if c.Operator == "CONTAINS" {
			value = "%" + value
		}
		value += "%"
		encoded, _ := json.Marshal(value)
		arg, e := b.bind(encoded, col)
		if e != nil {
			return "", e
		}
		return expr + " LIKE " + arg + ` ESCAPE '\'`, nil
	}
	arg, e := b.bind(raw, col)
	if e != nil {
		return "", e
	}
	return expr + " " + binaryOperator(c.Operator) + " " + arg, nil
}
func binaryOperator(op string) string {
	return map[string]string{"EQ": "=", "NE": "<>", "GT": ">", "GTE": ">=", "LT": "<", "LTE": "<="}[op]
}

var decimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`)
var integerPattern = regexp.MustCompile(`^[+-]?[0-9]+$`)

func (b *sqlBuilder) bind(raw json.RawMessage, col Column) (string, error) {
	value, e := TypedValue(raw, col)
	if e != nil {
		return "", e
	}
	name := "b" + strconv.Itoa(len(b.args)+1)
	b.args = append(b.args, sql.Named(name, value))
	return ":" + name, nil
}
func TypedValue(raw json.RawMessage, col Column) (any, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(&value); e != nil || value == nil {
		return nil, queryError("NULL must use a NULL operator")
	}
	text := ""
	switch v := value.(type) {
	case string:
		text = v
	case json.Number:
		text = string(v)
	case bool:
		text = strconv.FormatBool(v)
	default:
		return nil, queryError("scalar condition value expected")
	}
	switch col.LogicalType {
	case "integer", "decimal":
		if !decimalPattern.MatchString(text) || len(text) > 80 || col.LogicalType == "integer" && !integerPattern.MatchString(text) {
			return nil, queryError("invalid exact number")
		}
		if col.Scale != nil && *col.Scale >= 0 {
			parts := strings.Split(text, ".")
			if len(parts) > 1 && len(parts[1]) > *col.Scale {
				return nil, queryError("number exceeds column scale")
			}
		}
		return json.Number(text), nil
	case "date", "datetime":
		formats := []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02", "20060102", "20060102150405"}
		for _, format := range formats {
			if t, e := time.ParseInLocation(format, text, time.FixedZone("Asia/Shanghai", 8*3600)); e == nil {
				return t, nil
			}
		}
		return nil, queryError("invalid date value")
	default:
		if _, ok := value.(string); !ok {
			return nil, queryError("text value expected")
		}
		if len(text) > 4000 {
			return nil, queryError("text value exceeds limit")
		}
		return text, nil
	}
}
