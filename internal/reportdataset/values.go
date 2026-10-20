package reportdataset

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (c Compiled) Materialize(values map[string]any) map[string]any {
	for _, out := range c.Outputs {
		value := values[out.ValueName]
		if len(out.DisplayNames) > 0 && out.ValueMode == "DISPLAY" {
			relation := c.Relations[out.RelationID]
			value = c.DisplayValue(relation, values)
		}
		values[out.Name] = value
	}
	return values
}
func (c Compiled) DisplayValue(r RelationSchema, values map[string]any) any {
	for _, key := range r.TargetKeys {
		if values[key] == nil {
			return nil
		}
	}
	display := make([]any, 0, len(r.DisplayNames))
	nonempty := false
	for _, name := range r.DisplayNames {
		v := values[name]
		display = append(display, v)
		if v != nil && fmt.Sprint(v) != "" {
			nonempty = true
		}
	}
	if !nonempty {
		display = display[:0]
		for _, key := range r.TargetKeys {
			display = append(display, values[key])
		}
	}
	if len(display) == 1 {
		return display[0]
	}
	parts := []string{}
	for _, v := range display {
		if v != nil {
			parts = append(parts, fmt.Sprint(v))
		}
	}
	return strings.Join(parts, " · ")
}
func (c Compiled) RawValue(r RelationSchema, values map[string]any) any {
	if len(r.SourceKeys) == 1 {
		return values[r.SourceKeys[0]]
	}
	v := map[string]any{}
	for i, key := range r.SourceKeys {
		v[r.Relation.KeyBindings[i].TargetColumnID] = values[key]
	}
	return v
}
func (c Compiled) SnapshotColumns() []string {
	out := make([]string, 0, len(c.Projections)+len(c.Outputs))
	for _, p := range c.Projections {
		out = append(out, p.Name)
	}
	for _, o := range c.Outputs {
		out = append(out, o.Name)
	}
	return out
}
func (c Compiled) SnapshotFieldValues(values map[string]any) map[string]map[string]json.RawMessage {
	out := map[string]map[string]json.RawMessage{}
	for _, p := range c.Projections {
		if out[p.AliasID] == nil {
			out[p.AliasID] = map[string]json.RawMessage{}
		}
		b, _ := json.Marshal(values[p.Name])
		out[p.AliasID][p.ColumnID] = b
	}
	return out
}
