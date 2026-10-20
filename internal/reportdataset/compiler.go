package reportdataset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"sort"
	"strings"
)

func Decode[T any](raw []byte) (T, error) {
	var v T
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	if e := d.Decode(&v); e != nil {
		return v, fmt.Errorf("REPORT_QUERY_INVALID: invalid query structure: %w", e)
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return v, fmt.Errorf("REPORT_QUERY_INVALID: unexpected trailing data")
	}
	return v, nil
}
func queryError(message string) error { return fmt.Errorf("REPORT_QUERY_INVALID: %s", message) }
func TableIDs(q QuerySpec) []string {
	ids := []string{q.Base.TableID}
	for _, r := range q.Relations {
		ids = append(ids, r.TargetTableID)
	}
	return ids
}
func Compile(q QuerySpec, tables map[string]Table, policy Policy) (Compiled, error) {
	c := Compiled{CompilerVersion: 1, Query: q, Tables: tables, Policy: policy, Relations: map[string]RelationSchema{}}
	if q.Version != 1 || q.Base.TableID == "" || q.Base.AliasID != "base" || len(q.Relations) > 8 || len(q.Columns) == 0 || len(q.Columns) > 100 {
		return c, queryError("query base, version or limits are invalid")
	}
	if policy.ExportPolicy != "MANUAL" || policy.PurgeAfterExport || policy.ResultRetentionHours < 1 || policy.ResultRetentionHours > 720 || policy.SnapshotFormatVersion != 2 {
		return c, queryError("invalid execution policy")
	}
	aliases := map[string]Table{}
	base, ok := tables[q.Base.TableID]
	if !ok {
		return c, queryError("base table is unavailable")
	}
	aliases["base"] = base
	depths := map[string]int{"base": 0}
	pending := append([]Relation{}, q.Relations...)
	sorted := []Relation{}
	ids := map[string]bool{"base": true}
	for _, r := range pending {
		if r.ID == "" || ids[r.ID] || len(r.ID) > 64 {
			return c, queryError("relation ids must be unique")
		}
		ids[r.ID] = true
	}
	for len(pending) > 0 {
		progress := false
		next := []Relation{}
		for _, r := range pending {
			source, ok := aliases[r.FromAliasID]
			if !ok {
				next = append(next, r)
				continue
			}
			target, ok := tables[r.TargetTableID]
			if !ok {
				return c, queryError("relation table is unavailable")
			}
			if r.JoinType != "LEFT" || r.Cardinality != "MANY_TO_ONE" && r.Cardinality != "ONE_TO_ONE" || len(r.KeyBindings) == 0 {
				return c, queryError("only unique-key LEFT relations are supported")
			}
			if depths[r.FromAliasID] >= 3 {
				return c, queryError("relation depth exceeds 3")
			}
			keys := []string{}
			sourceSeen := map[string]bool{}
			for _, b := range r.KeyBindings {
				sc, e := availableColumn(source, b.SourceColumnID, false)
				if e != nil {
					return c, e
				}
				tc, e := availableColumn(target, b.TargetColumnID, false)
				if e != nil {
					return c, e
				}
				if sc.LogicalType != tc.LogicalType && !(numeric(sc.LogicalType) && numeric(tc.LogicalType)) {
					return c, queryError("relation key types differ")
				}
				if sourceSeen[b.SourceColumnID] {
					return c, queryError("duplicate relation key binding")
				}
				sourceSeen[b.SourceColumnID] = true
				keys = append(keys, b.TargetColumnID)
			}
			if !uniqueKey(target, keys) {
				return c, fmt.Errorf("REPORT_RELATION_NOT_UNIQUE: target bindings must cover a verified non-null unique key")
			}
			if len(r.DisplayColumnIDs) == 0 {
				r.DisplayColumnIDs = append([]string{}, target.DisplayFields...)
			}
			if len(r.DisplayColumnIDs) == 0 {
				r.DisplayColumnIDs = append([]string{}, target.PrimaryKeys...)
			}
			aliases[r.ID] = target
			depths[r.ID] = depths[r.FromAliasID] + 1
			sorted = append(sorted, r)
			progress = true
		}
		if !progress {
			return c, queryError("relation graph has a cycle or unknown dependency")
		}
		pending = next
	}
	c.Query.Relations = sorted
	for alias, t := range aliases {
		if !t.Config.Enabled || t.ValidationStatus != "VALID" || len(t.PrimaryKeys) == 0 || !uniqueKey(t, t.PrimaryKeys) {
			return c, queryError("table is disabled or lacks a verified key")
		}
		if t.Config.DataScope.Mode != "GLOBAL_APPROVED" && t.Config.DataScope.Mode != "SCOPE_COLUMN" {
			return c, queryError("table needs an explicit data scope")
		}
		if _, e := Identifier(t.Owner); e != nil {
			return c, e
		}
		if _, e := Identifier(t.PhysicalTable); e != nil {
			return c, e
		}
		if t.DBLink != "" {
			if _, e := Identifier(t.DBLink); e != nil {
				return c, e
			}
		}
		for _, id := range t.PrimaryKeys {
			if _, e := availableColumn(t, id, false); e != nil {
				return c, e
			}
		}
		_ = alias
	}
	add := func(alias, id string, detail bool) (string, error) {
		if _, e := availableColumn(aliases[alias], id, detail); e != nil {
			return "", e
		}
		for _, p := range c.Projections {
			if p.AliasID == alias && p.ColumnID == id {
				return p.Name, nil
			}
		}
		name := fmt.Sprintf("C%03d", len(c.Projections)+1)
		c.Projections = append(c.Projections, Projection{Name: name, AliasID: alias, ColumnID: id})
		return name, nil
	}
	for _, id := range base.PrimaryKeys {
		if _, e := add("base", id, false); e != nil {
			return c, e
		}
	}
	for _, r := range sorted {
		schema := RelationSchema{Relation: r, DetailNames: map[string]string{}}
		for _, b := range r.KeyBindings {
			src, e := add(r.FromAliasID, b.SourceColumnID, false)
			if e != nil {
				return c, e
			}
			dst, e := add(r.ID, b.TargetColumnID, false)
			if e != nil {
				return c, e
			}
			schema.SourceKeys = append(schema.SourceKeys, src)
			schema.TargetKeys = append(schema.TargetKeys, dst)
		}
		for _, id := range r.DisplayColumnIDs {
			name, e := add(r.ID, id, false)
			if e != nil {
				return c, e
			}
			schema.DisplayNames = append(schema.DisplayNames, name)
			schema.DetailNames[id] = name
		}
		for _, id := range r.DetailColumnIDs {
			name, e := add(r.ID, id, true)
			if e != nil {
				return c, e
			}
			schema.DetailNames[id] = name
		}
		c.Relations[r.ID] = schema
	}
	fields, codes := map[string]bool{}, map[string]bool{}
	for i, col := range q.Columns {
		if _, e := uuid.Parse(col.FieldID); e != nil || fields[col.FieldID] {
			return c, queryError("fieldId must be a unique UUID")
		}
		fields[col.FieldID] = true
		if col.Code == "" {
			col.Code = "f_" + strings.ReplaceAll(col.FieldID, "-", "")
		}
		if _, e := Identifier(col.Code); e != nil || len(col.Code) > 64 || codes[strings.ToUpper(col.Code)] {
			return c, queryError("logical column codes must be unique")
		}
		codes[strings.ToUpper(col.Code)] = true
		alias, id := "base", col.Source.ColumnID
		out := Output{FieldID: col.FieldID, Code: col.Code, Header: strings.TrimSpace(col.Header), ValueMode: col.ValueMode, PreviewVisible: col.PreviewVisible == nil || *col.PreviewVisible, ExportVisible: col.ExportVisible == nil || *col.ExportVisible}
		if out.ValueMode == "" {
			out.ValueMode = "DISPLAY"
		}
		if out.ValueMode != "DISPLAY" && out.ValueMode != "RAW" {
			return c, queryError("invalid value mode")
		}
		switch col.Source.Kind {
		case "BASE_COLUMN":
			if col.Source.RelationID != "" {
				return c, queryError("base column cannot reference relation")
			}
		case "RELATION_COLUMN", "RELATION_DISPLAY":
			schema, ok := c.Relations[col.Source.RelationID]
			if !ok {
				return c, queryError("column relation is missing")
			}
			alias = col.Source.RelationID
			out.RelationID = alias
			if col.Source.Kind == "RELATION_DISPLAY" {
				id = schema.Relation.DisplayColumnIDs[0]
				out.DisplayNames = schema.DisplayNames
				out.RawName = schema.SourceKeys[0]
				if out.ValueMode == "RAW" {
					alias = schema.Relation.FromAliasID
					id = schema.Relation.KeyBindings[0].SourceColumnID
				}
			}
		default:
			return c, queryError("invalid column source")
		}
		column, e := availableColumn(aliases[alias], id, false)
		if e != nil {
			return c, e
		}
		out.OracleType = column.OracleType
		out.LogicalType = column.LogicalType
		out.Precision = column.Precision
		out.Scale = column.Scale
		out.Nullable = column.Nullable
		if len(out.DisplayNames) > 1 && out.ValueMode == "DISPLAY" {
			out.OracleType = "VARCHAR2"
			out.LogicalType = "string"
		}

		if col.Source.Kind == "RELATION_DISPLAY" && out.ValueMode == "RAW" && len(c.Relations[col.Source.RelationID].SourceKeys) > 1 {
			out.OracleType = "VARCHAR2"
			out.LogicalType = "string"
			out.Precision = nil
			out.Scale = nil
		}
		if col.ValueType != "" && col.ValueType != out.LogicalType {
			switch col.ValueType {
			case "string":
				out.OracleType = "VARCHAR2"
				out.LogicalType = "string"
				out.Precision = nil
				out.Scale = nil
			case "decimal":
				if out.LogicalType != "integer" {
					return c, queryError("output valueType is incompatible with source")
				}
				out.LogicalType = "decimal"
			case "datetime":
				if out.LogicalType != "date" {
					return c, queryError("output valueType is incompatible with source")
				}
				out.LogicalType = "datetime"
			default:
				return c, queryError("output valueType is incompatible with source")
			}
		}
		if out.Header == "" {
			out.Header = column.DBName
			if col.Source.Kind == "RELATION_DISPLAY" {
				r := c.Relations[col.Source.RelationID].Relation
				src, _ := availableColumn(aliases[r.FromAliasID], r.KeyBindings[0].SourceColumnID, false)
				out.Header = src.DBName
			}
		}
		if len(out.Header) > 255 {
			return c, queryError("column header is too long")
		}
		name, e := add(alias, id, false)
		if e != nil {
			return c, e
		}
		out.ValueName = name
		out.Name = fmt.Sprintf("O%03d", i+1)
		c.Outputs = append(c.Outputs, out)
		col.Header = out.Header
		col.ValueMode = out.ValueMode
		c.Query.Columns[i] = col
		if col.Source.Kind == "RELATION_COLUMN" {
			r := c.Relations[col.Source.RelationID]
			r.DetailNames[id] = name
			c.Relations[col.Source.RelationID] = r
		}
	}
	// Project values needed to replay row-field predicates for LIVE details.
	for _, r := range sorted {
		if e := walkCondition(r.MatchConditions, func(v Condition) error {
			if v.AliasID != r.ID {
				if _, e := add(v.AliasID, v.ColumnID, false); e != nil {
					return e
				}
			}
			if v.Value != nil && v.Value.Kind == "FIELD" {
				_, e := add(v.Value.AliasID, v.Value.ColumnID, false)
				return e
			}
			return nil
		}); e != nil {
			return c, e
		}
	}
	for _, f := range q.AllowedFilters {
		col, e := availableColumn(aliases[f.AliasID], f.ColumnID, false)
		if e != nil {
			return c, e
		}
		for _, op := range f.Operators {
			if !contains(ColumnOperators(col), op) {
				return c, queryError("unsupported allowed filter operator")
			}
		}
	}
	for _, o := range q.AllowedSortFields {
		if col, e := availableColumn(aliases[o.AliasID], o.ColumnID, false); e != nil {
			return c, e
		} else if !SortableColumn(col) {
			return c, queryError("LOB columns cannot be sorted")
		}
	}
	// Validate all fixed predicates without requiring runtime parameter values.
	if _, e := c.Build(nil, QueryInputs{}, "", true); e != nil {
		return c, e
	}
	if len(c.Projections)+len(c.Outputs) > 512 {
		return c, queryError("internal projection exceeds snapshot column limit")
	}
	c.SchemaHash = Hash(struct {
		Projections []Projection
		Outputs     []Output
		Relations   map[string]RelationSchema
	}{c.Projections, c.Outputs, c.Relations})
	return c, nil
}
func availableColumn(t Table, id string, detail bool) (Column, error) {
	for _, c := range t.Columns {
		if c.ID != id {
			continue
		}
		if !c.IsPhysical || !c.IsSelectable {
			return c, fmt.Errorf("REPORT_METADATA_UNSUPPORTED: virtual or unsupported column")
		}
		allowed := t.Config.SelectableColumns
		if detail && t.Config.DetailColumns != nil {
			allowed = t.Config.DetailColumns
		}
		if allowed != nil && !contains(*allowed, id) {
			return c, queryError("column is not approved")
		}
		if _, e := Identifier(c.DBName); e != nil {
			return c, e
		}
		return c, nil
	}
	return Column{}, queryError("column does not belong to table")
}
func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
func numeric(t string) bool { return t == "integer" || t == "decimal" }
func uniqueKey(t Table, ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	a := append([]string{}, ids...)
	sort.Strings(a)
	for _, k := range t.UniqueKeys {
		b := append([]string{}, k...)
		sort.Strings(b)
		if len(a) == len(b) && strings.Join(a, "\x00") == strings.Join(b, "\x00") {
			return true
		}
	}
	return false
}
func walkCondition(c *Condition, visit func(Condition) error) error {
	if c == nil {
		return nil
	}
	if c.Logic != "" {
		for _, item := range c.Items {
			if e := walkCondition(&item, visit); e != nil {
				return e
			}
		}
		return nil
	}
	return visit(*c)
}
