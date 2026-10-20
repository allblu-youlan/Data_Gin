package reportdataset

import (
	"encoding/json"
	"fmt"
	"strings"
)

type RelationPlan struct {
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
	result := RelationPlan{ColumnNames: map[string]string{}, KeyColumns: target.PrimaryKeys, DisplayColumns: relation.Relation.DisplayColumnIDs}
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
		expr, _, e := b.column(relationID, id)
		if e != nil {
			return result, e
		}
		name := fmt.Sprintf("V%03d", len(selects)+1)
		result.ColumnNames[id] = name
		selects = append(selects, expr+" AS "+name)
		result.Plan.Columns = append(result.Plan.Columns, name)
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
	result.Plan.SQL = "SELECT " + strings.Join(selects, ",") + " FROM " + obj + " T0 WHERE (" + strings.Join(where, ") AND (") + ") ORDER BY " + strings.Join(orders, ",")
	result.Plan.Args = b.args
	return result, nil
}
