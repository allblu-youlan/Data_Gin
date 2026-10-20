package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"sort"
	"strings"
)

// The remote namespace is server configuration, never a client-supplied SQL fragment.
type BojunNamespace struct{ Owner, DBLink string }

func (n BojunNamespace) object(name string) (string, error) {
	owner, e := reportdataset.Identifier(n.Owner)
	if e != nil {
		return "", e
	}
	table, e := reportdataset.Identifier(name)
	if e != nil {
		return "", e
	}
	suffix := ""
	if n.DBLink != "" {
		link, e := reportdataset.Identifier(n.DBLink)
		if e != nil {
			return "", e
		}
		suffix = "@" + link
	}
	return owner + "." + table + suffix, nil
}
func (n BojunNamespace) dictionary(name string) (string, error) {
	obj, e := n.object(name)
	if e != nil {
		return "", e
	}
	return strings.TrimPrefix(obj, strings.ToUpper(n.Owner)+"."), nil
}

type BojunTableMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RealTableID string `json:"realTableId"`
	Filter      string `json:"filter"`
	PK          string `json:"pk"`
	AK          string `json:"ak"`
	DK          string `json:"dk"`
}

func (a *Adapter) ListBojunMetadata(ctx context.Context, n BojunNamespace, after string, limit int) ([]BojunTableMetadata, error) {
	obj, e := n.object("AD_TABLE")
	if e != nil {
		return nil, e
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	rows, e := a.db.QueryContext(ctx, `SELECT ID, NAME, DESCRIPTION, REALTABLE_ID, FILTER, PK_COLUMN_ID, AK_COLUMN_ID, DK_COLUMN_ID FROM (SELECT ID, NAME, DESCRIPTION, REALTABLE_ID, FILTER, PK_COLUMN_ID, AK_COLUMN_ID, DK_COLUMN_ID FROM `+obj+` WHERE ISACTIVE = 'Y' AND ID > :1 ORDER BY ID) WHERE ROWNUM <= :2`, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []BojunTableMetadata{}
	for rows.Next() {
		var t BojunTableMetadata
		var d, r, f, p, ak, dk sql.NullString
		if e = rows.Scan(&t.ID, &t.Name, &d, &r, &f, &p, &ak, &dk); e != nil {
			return nil, e
		}
		t.Description = d.String
		t.RealTableID = r.String
		t.Filter = f.String
		t.PK = p.String
		t.AK = ak.String
		t.DK = dk.String
		out = append(out, t)
	}
	return out, rows.Err()
}
func (a *Adapter) InspectBojunTable(ctx context.Context, n BojunNamespace, source BojunTableMetadata, all map[string]BojunTableMetadata) (reportdataset.Table, error) {
	t := reportdataset.Table{BojunTableID: source.ID, LogicalName: source.Name, Description: source.Description, Owner: strings.ToUpper(n.Owner), DBLink: n.DBLink, Columns: []reportdataset.Column{}, PrimaryKeys: []string{}, DisplayFields: []string{}, SearchFields: []string{}, UniqueKeys: [][]string{}}
	current := source
	seen := map[string]bool{}
	for {
		if seen[current.ID] {
			t.ValidationReason = "逻辑表映射存在循环"
			return t, nil
		}
		seen[current.ID] = true
		if strings.TrimSpace(current.Filter) != "" {
			t.ValidationReason = "逻辑表固有过滤条件需要配置支持"
			return t, nil
		}
		if current.RealTableID == "" || current.RealTableID == "0" || current.RealTableID == current.ID {
			break
		}
		var ok bool
		current, ok = all[current.RealTableID]
		if !ok {
			t.ValidationReason = "实际表映射不可用"
			return t, nil
		}
	}
	physical, e := reportdataset.Identifier(current.Name)
	if e != nil {
		t.ValidationReason = "物理表名称不支持"
		return t, nil
	}
	t.PhysicalTable = physical
	dict, e := n.dictionary("ALL_TAB_COLUMNS")
	if e != nil {
		return t, e
	}
	rows, e := a.db.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE, DATA_PRECISION, DATA_SCALE, NULLABLE FROM `+dict+` WHERE OWNER = :1 AND TABLE_NAME = :2`, strings.ToUpper(n.Owner), physical)
	if e != nil {
		return t, e
	}
	actual := map[string]reportdataset.Column{}
	for rows.Next() {
		var c reportdataset.Column
		var p, s sql.NullInt64
		var nullable string
		if e = rows.Scan(&c.DBName, &c.OracleType, &p, &s, &nullable); e != nil {
			rows.Close()
			return t, e
		}
		if p.Valid {
			v := int(p.Int64)
			c.Precision = &v
		}
		if s.Valid {
			v := int(s.Int64)
			c.Scale = &v
		}
		c.Nullable = nullable == "Y"
		c.IsPhysical = true
		c.IsSelectable = bojunSelectableType(c.OracleType)
		c.LogicalType = reportdataset.LogicalType(c.OracleType, c.Scale)
		c.SupportedOperators = reportdataset.Operators(c.LogicalType)
		actual[c.DBName] = c
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return t, e
	}
	columnsObject, _ := n.object("AD_COLUMN")
	rows, e = a.db.QueryContext(ctx, `SELECT ID, NAME, DBNAME, DESCRIPTION, ORDERNO, REF_TABLE_ID, REF_COLUMN_ID FROM `+columnsObject+` WHERE AD_TABLE_ID IN (:1, :2) ORDER BY ORDERNO, ID`, source.ID, current.ID)
	if e != nil {
		return t, e
	}
	sourceToDB := map[string]string{}
	for rows.Next() {
		var id, name string
		var db, d, ref, refCol sql.NullString
		var order sql.NullInt64
		if e = rows.Scan(&id, &name, &db, &d, &order, &ref, &refCol); e != nil {
			rows.Close()
			return t, e
		}
		c, ok := actual[strings.ToUpper(db.String)]
		if !ok {
			c = reportdataset.Column{DBName: db.String}
		}
		c.BojunColumnID = id
		c.Name = name
		c.Description = d.String
		c.Ordinal = int(order.Int64)
		c.RefTableID = ref.String
		c.RefColumnID = refCol.String
		t.Columns = append(t.Columns, c)
		sourceToDB[id] = c.DBName
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return t, e
	}
	constraints, _ := n.dictionary("ALL_CONSTRAINTS")
	consCols, _ := n.dictionary("ALL_CONS_COLUMNS")
	rows, e = a.db.QueryContext(ctx, `SELECT c.CONSTRAINT_NAME, cc.COLUMN_NAME FROM `+constraints+` c JOIN `+consCols+` cc ON cc.OWNER = c.OWNER AND cc.CONSTRAINT_NAME = c.CONSTRAINT_NAME WHERE c.OWNER = :1 AND c.TABLE_NAME = :2 AND c.CONSTRAINT_TYPE IN ('P','U') AND c.STATUS = 'ENABLED' AND c.VALIDATED = 'VALIDATED' ORDER BY c.CONSTRAINT_NAME, cc.POSITION`, strings.ToUpper(n.Owner), physical)
	if e != nil {
		return t, e
	}
	keys := map[string][]string{}
	order := []string{}
	for rows.Next() {
		var k, c string
		if e = rows.Scan(&k, &c); e != nil {
			rows.Close()
			return t, e
		}
		if _, ok := keys[k]; !ok {
			order = append(order, k)
		}
		keys[k] = append(keys[k], c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return t, e
	}
	for _, key := range order {
		valid := true
		for _, db := range keys[key] {
			c, ok := actual[db]
			if !ok || c.Nullable {
				valid = false
			}
		}
		if valid {
			t.UniqueKeys = append(t.UniqueKeys, keys[key])
		}
	}
	pk := sourceToDB[source.PK]
	for _, key := range t.UniqueKeys {
		if len(key) == 1 && key[0] == pk {
			t.PrimaryKeys = key
			break
		}
	}
	// Physical composite keys remain valid even when the UI dictionary names only one component.
	if len(t.PrimaryKeys) == 0 && len(t.UniqueKeys) > 0 {
		t.PrimaryKeys = t.UniqueKeys[0]
	}
	dk := sourceToDB[source.DK]
	if _, ok := actual[dk]; !ok {
		dk = sourceToDB[current.DK]
	}
	if _, ok := actual[dk]; ok {
		t.DisplayFields = []string{dk}
	} else {
		t.DisplayFields = append([]string{}, t.PrimaryKeys...)
	}
	if ak := sourceToDB[source.AK]; actual[ak].IsPhysical {
		t.SearchFields = []string{ak}
	} else {
		t.SearchFields = append([]string{}, t.DisplayFields...)
	}
	if len(actual) == 0 {
		t.ValidationReason = "未找到物理字段"
	} else if len(t.PrimaryKeys) == 0 {
		t.ValidationReason = "未验证非空唯一键"
	} else {
		t.ValidationStatus = "VALID"
	}
	return t, nil
}
func bojunSelectableType(t string) bool {
	switch t {
	case "NUMBER", "VARCHAR2", "NVARCHAR2", "CHAR", "NCHAR", "DATE", "CLOB", "NCLOB":
		return true
	default:
		return strings.HasPrefix(t, "TIMESTAMP")
	}
}
func (a *Adapter) ValidateBojunMetadata(ctx context.Context, t reportdataset.Table) error {
	n := BojunNamespace{Owner: t.Owner, DBLink: t.DBLink}
	dict, e := n.dictionary("ALL_TAB_COLUMNS")
	if e != nil {
		return e
	}
	rows, e := a.db.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE, DATA_PRECISION, DATA_SCALE FROM `+dict+` WHERE OWNER = :1 AND TABLE_NAME = :2`, t.Owner, t.PhysicalTable)
	if e != nil {
		return e
	}
	defer rows.Close()
	actual := map[string]string{}
	for rows.Next() {
		var name, typ string
		var p, s sql.NullInt64
		if e = rows.Scan(&name, &typ, &p, &s); e != nil {
			return e
		}
		actual[name] = fmt.Sprintf("%s:%v:%v", typ, p, s)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	for _, c := range t.Columns {
		if !c.IsPhysical || !c.IsSelectable {
			continue
		}
		p, s := sql.NullInt64{}, sql.NullInt64{}
		if c.Precision != nil {
			p = sql.NullInt64{Int64: int64(*c.Precision), Valid: true}
		}
		if c.Scale != nil {
			s = sql.NullInt64{Int64: int64(*c.Scale), Valid: true}
		}
		if actual[c.DBName] != fmt.Sprintf("%s:%v:%v", c.OracleType, p, s) {
			return fmt.Errorf("REPORT_METADATA_CHANGED: source schema changed")
		}
	}

	constraints, _ := n.dictionary("ALL_CONSTRAINTS")
	constraintColumns, _ := n.dictionary("ALL_CONS_COLUMNS")
	keyRows, err := a.db.QueryContext(ctx, `SELECT c.CONSTRAINT_NAME, cc.COLUMN_NAME FROM `+constraints+` c JOIN `+constraintColumns+` cc ON cc.OWNER = c.OWNER AND cc.CONSTRAINT_NAME = c.CONSTRAINT_NAME WHERE c.OWNER = :1 AND c.TABLE_NAME = :2 AND c.CONSTRAINT_TYPE IN ('P','U') AND c.STATUS = 'ENABLED' AND c.VALIDATED = 'VALIDATED' ORDER BY c.CONSTRAINT_NAME, cc.POSITION`, t.Owner, t.PhysicalTable)
	if err != nil {
		return err
	}
	defer keyRows.Close()
	liveKeys := map[string][]string{}
	for keyRows.Next() {
		var name, column string
		if err := keyRows.Scan(&name, &column); err != nil {
			return err
		}
		liveKeys[name] = append(liveKeys[name], column)
	}
	if err := keyRows.Err(); err != nil {
		return err
	}
	for _, required := range t.UniqueKeys {
		names := []string{}
		for _, id := range required {
			for _, col := range t.Columns {
				if col.ID == id {
					names = append(names, col.DBName)
				}
			}
		}
		valid := false
		sort.Strings(names)
		for _, current := range liveKeys {
			current = append([]string{}, current...)
			sort.Strings(current)
			if len(names) > 0 && len(names) == len(required) && strings.Join(names, "\x00") == strings.Join(current, "\x00") {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("REPORT_METADATA_CHANGED: required unique constraint changed")
		}
	}
	return nil
}
