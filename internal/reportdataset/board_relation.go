package reportdataset

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// BoardRelation projects only the selected physical fields from a managed
// table. Restrictions names the columns constrained by the enclosing query.
func BoardRelation(t Table, actor uint, fields []string, ids map[string]uint, restrictions []string) (Plan, error) {
	if t.ValidationStatus != "VALID" || !t.Config.Enabled {
		return Plan{}, queryError("目录表不可用")
	}
	obj, e := objectName(t)
	if e != nil {
		return Plan{}, e
	}
	b := sqlBuilder{aliases: map[string]Table{"base": t}, sqlAliases: map[string]string{"base": "B0"}}
	selected := []string{}
	seen := map[string]bool{}
	for _, field := range fields {
		if seen[field] {
			continue
		}
		seen[field] = true
		cid := strconv.FormatUint(uint64(ids[field]), 10)
		c, e := availableColumn(t, cid, false)
		if e != nil || c.DBName != field {
			return Plan{}, fmt.Errorf("字段 %s 已失效或无权使用", field)
		}
		selected = append(selected, "B0."+c.DBName)
	}
	for _, required := range t.Config.RequiredFilterColumns {
		ok := slices.ContainsFunc(restrictions, func(f string) bool { return ids[f] != 0 && strconv.FormatUint(uint64(ids[f]), 10) == required })
		if !ok {
			return Plan{}, queryError("缺少受管表必需筛选条件")
		}
	}
	actorID := strconv.FormatUint(uint64(actor), 10)
	if t.Config.DataScope.Mode == "SCOPE_COLUMN" && len(t.Config.DataScope.UserValues[actorID]) == 0 {
		return Plan{}, queryError("当前账号没有数据范围")
	}
	scope, e := b.scope("base", actorID)
	if e != nil {
		return Plan{}, e
	}
	fixed, e := b.fixed("base")
	if e != nil {
		return Plan{}, e
	}
	predicate := "(" + scope + ")"
	if fixed != "" {
		predicate += " AND (" + fixed + ")"
	}
	return Plan{SQL: "(SELECT " + strings.Join(selected, ", ") + " FROM " + obj + " B0 WHERE " + predicate + ")", Args: b.args}, nil
}
