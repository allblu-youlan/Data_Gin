package reportdataset

// DependencyTables limits compatibility probes to fields and keys used by the
// published query, including runtime capabilities and relation detail fields.
func (c Compiled) DependencyTables() map[string]Table {
	aliases := map[string]string{"base": c.Query.Base.TableID}
	for _, r := range c.Query.Relations {
		aliases[r.ID] = r.TargetTableID
	}
	needed := map[string]map[string]bool{}
	keys := map[string][][]string{}
	add := func(alias, id string) {
		table := aliases[alias]
		if needed[table] == nil {
			needed[table] = map[string]bool{}
		}
		needed[table][id] = true
	}
	visit := func(v Condition) error {
		add(v.AliasID, v.ColumnID)
		if v.Value != nil && v.Value.Kind == "FIELD" {
			add(v.Value.AliasID, v.Value.ColumnID)
		}
		return nil
	}
	for _, p := range c.Projections {
		add(p.AliasID, p.ColumnID)
	}
	_ = walkCondition(c.Query.Where, visit)
	for _, r := range c.Query.Relations {
		_ = walkCondition(r.MatchConditions, visit)
		key := []string{}
		for _, binding := range r.KeyBindings {
			key = append(key, binding.TargetColumnID)
		}
		keys[r.TargetTableID] = append(keys[r.TargetTableID], key)
	}
	base := c.Tables[c.Query.Base.TableID]
	keys[base.ID] = append(keys[base.ID], base.PrimaryKeys)
	for alias, id := range aliases {
		t := c.Tables[id]
		if t.Config.DataScope.Mode == "SCOPE_COLUMN" {
			add(alias, t.Config.DataScope.ColumnID)
		}
	}
	for _, f := range c.Query.AllowedFilters {
		add(f.AliasID, f.ColumnID)
	}
	for _, o := range append(append([]Order{}, c.Query.OrderBy...), c.Query.AllowedSortFields...) {
		add(o.AliasID, o.ColumnID)
	}
	out := map[string]Table{}
	for id, t := range c.Tables {
		columns := []Column{}
		for _, col := range t.Columns {
			if needed[id][col.ID] {
				columns = append(columns, col)
			}
		}
		t.Columns = columns
		t.UniqueKeys = keys[id]
		out[id] = t
	}
	return out
}
