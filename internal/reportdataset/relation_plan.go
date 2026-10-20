package reportdataset

import (
	"encoding/json"
	"fmt"
	"strings"
)

type RelationPlan struct {
	SnapshotNames  map[string]string
	TableIDs       []string
	Plan           Plan
	ColumnNames    map[string]string
	KeyColumns     []string
	DisplayColumns []string
}

func (c Compiled) BuildRelationPlan(relationID string, parameters map[string]json.RawMessage, source map[string]map[string]json.RawMessage, keys map[string]json.RawMessage, search string, after map[string]json.RawMessage, actor string, detail bool) (RelationPlan, error) {
	relation, ok := c.Relations[relationID]
	if !ok {
		return RelationPlan{}, queryError("unknown relation")
	}
	target := c.Tables[relation.Relation.TargetTableID]
	b := sqlBuilder{compiled: c, aliases: map[string]Table{"base": c.Tables[c.Query.Base.TableID]}, sqlAliases: map[string]string{relationID: "T0"}, parameters: parameters, boundColumns: source}
	for _, r := range c.Query.Relations {
		b.aliases[r.ID] = c.Tables[r.TargetTableID]
	}
	result := RelationPlan{SnapshotNames: map[string]string{}, TableIDs: []string{target.ID}, ColumnNames: map[string]string{}, KeyColumns: target.PrimaryKeys, DisplayColumns: relation.Relation.DisplayColumnIDs}
	ids := append([]string{}, target.PrimaryKeys...)
	for _, binding := range relation.Relation.KeyBindings {
		ids = append(ids, binding.TargetColumnID)
	}
	ids = append(ids, relation.Relation.DisplayColumnIDs...)
	if detail {
		for id := range relation.DetailNames {
			ids = append(ids, id)
		}
	}
	selects := []string{}
	for _, id := range ids {
		if _, ok := result.ColumnNames[id]; ok {
			continue
		}
		expr, _, e := b.projectionColumn(relationID, id)
		if e != nil {
			return result, e
		}
		name := fmt.Sprintf("V%03d", len(selects)+1)
		result.ColumnNames[id] = name
		selects = append(selects, expr+" AS "+name)
		result.Plan.Columns = append(result.Plan.Columns, name)
	}
	joins := ""
	if detail {
		var err error
		joins, err = c.detailJoins(&b, relationID, &selects, &result, actor)
		if err != nil {
			return result, err
		}
	}
	where := []string{}
	scope, e := b.scope(relationID, actor)
	if e != nil {
		return result, e
	}
	where = append(where, scope)
	fixed, e := b.fixed(relationID)
	if e != nil {
		return result, e
	}
	if fixed != "" {
		where = append(where, fixed)
	}
	match, e := b.condition(relation.Relation.MatchConditions, 0, false)
	if e != nil {
		return result, e
	}
	if match != "" {
		where = append(where, match)
	}
	if len(keys) > 0 {
		for _, binding := range relation.Relation.KeyBindings {
			raw, ok := keys[binding.TargetColumnID]
			if !ok {
				return result, queryError("incomplete record key")
			}
			expr, col, e := b.column(relationID, binding.TargetColumnID)
			if e != nil {
				return result, e
			}
			bind, e := b.bind(raw, col)
			if e != nil {
				return result, e
			}
			where = append(where, expr+" = "+bind)
		}
	}
	if search != "" {
		parts := []string{}
		for _, id := range target.SearchFields {
			expr, col, e := b.column(relationID, id)
			if e != nil {
				return result, e
			}
			if col.LogicalType != "string" {
				continue
			}
			value := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search) + "%"
			raw, _ := json.Marshal(value)
			arg, e := b.bind(raw, col)
			if e != nil {
				return result, e
			}
			parts = append(parts, expr+" LIKE "+arg+` ESCAPE '\'`)
		}
		if len(parts) == 0 {
			return result, queryError("relation has no approved text search field")
		}
		where = append(where, "("+strings.Join(parts, " OR ")+")")
	}
	if len(after) > 0 {
		parts := []string{}
		prefix := []string{}
		for _, id := range target.PrimaryKeys {
			raw, ok := after[id]
			if !ok {
				return result, queryError("incomplete options cursor")
			}
			expr, col, e := b.column(relationID, id)
			if e != nil {
				return result, e
			}
			arg, e := b.bind(raw, col)
			if e != nil {
				return result, e
			}
			terms := append(append([]string{}, prefix...), expr+" > "+arg)
			parts = append(parts, "("+strings.Join(terms, " AND ")+")")
			prefix = append(prefix, expr+" = "+arg)
		}
		where = append(where, "("+strings.Join(parts, " OR ")+")")
	}
	orders := []string{}
	for _, id := range target.PrimaryKeys {
		expr, _, e := b.column(relationID, id)
		if e != nil {
			return result, e
		}
		orders = append(orders, expr+" ASC NULLS LAST")
	}
	obj, e := objectName(target)
	if e != nil {
		return result, e
	}
	result.Plan.SQL = "SELECT " + strings.Join(selects, ",") + " FROM " + obj + " T0" + joins + " WHERE (" + strings.Join(where, ") AND (") + ") ORDER BY " + strings.Join(orders, ",")
	result.Plan.Args = b.args
	return result, nil
}

func (c Compiled) detailJoins(b *sqlBuilder, root string, selects *[]string, result *RelationPlan, actor string) (string, error) {
	joined := map[string]bool{root: true}
	sql := ""
	for _, r := range c.Query.Relations {
		if !joined[r.FromAliasID] || r.ID == root {
			continue
		}
		joined[r.ID] = true
		b.sqlAliases[r.ID] = fmt.Sprintf("J%d", len(joined))
		target := c.Tables[r.TargetTableID]
		result.TableIDs = append(result.TableIDs, target.ID)
		obj, e := objectName(target)
		if e != nil {
			return "", e
		}
		on := []string{}
		for _, key := range r.KeyBindings {
			source, _, e := b.column(r.FromAliasID, key.SourceColumnID)
			if e != nil {
				return "", e
			}
			dest, _, e := b.column(r.ID, key.TargetColumnID)
			if e != nil {
				return "", e
			}
			on = append(on, dest+" = "+source)
		}
		scope, e := b.scope(r.ID, actor)
		if e != nil {
			return "", e
		}
		on = append(on, scope)
		fixed, e := b.fixed(r.ID)
		if e != nil {
			return "", e
		}
		if fixed != "" {
			on = append(on, fixed)
		}
		match, e := b.condition(r.MatchConditions, 0, false)
		if e != nil {
			return "", e
		}
		if match != "" {
			on = append(on, match)
		}
		sql += " LEFT JOIN " + obj + " " + b.sqlAliases[r.ID] + " ON (" + strings.Join(on, " AND ") + ")"
		ids := append([]string{}, r.DisplayColumnIDs...)
		for _, key := range r.KeyBindings {
			ids = append(ids, key.TargetColumnID)
		}
		for _, id := range ids {
			for _, p := range c.Projections {
				if p.AliasID != r.ID || p.ColumnID != id {
					continue
				}
				if _, ok := result.SnapshotNames[p.Name]; ok {
					continue
				}
				expr, _, e := b.projectionColumn(r.ID, id)
				if e != nil {
					return "", e
				}
				name := fmt.Sprintf("V%03d", len(*selects)+1)
				*selects = append(*selects, expr+" AS "+name)
				result.Plan.Columns = append(result.Plan.Columns, name)
				result.SnapshotNames[p.Name] = name
			}
		}
	}
	return sql, nil
}
