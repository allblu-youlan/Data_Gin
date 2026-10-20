package reportdataset

import "encoding/json"

type Value struct {
	Kind     string          `json:"kind"`
	Value    json.RawMessage `json:"value,omitempty"`
	Code     string          `json:"code,omitempty"`
	AliasID  string          `json:"aliasId,omitempty"`
	ColumnID string          `json:"columnId,omitempty"`
}
type Condition struct {
	Logic    string      `json:"logic,omitempty"`
	Items    []Condition `json:"items,omitempty"`
	AliasID  string      `json:"aliasId,omitempty"`
	ColumnID string      `json:"columnId,omitempty"`
	Operator string      `json:"operator,omitempty"`
	Value    *Value      `json:"value,omitempty"`
}

type Base struct {
	TableID string `json:"tableId"`
	AliasID string `json:"aliasId"`
}
type KeyBinding struct {
	SourceColumnID string `json:"sourceColumnId"`
	TargetColumnID string `json:"targetColumnId"`
}
type Relation struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	FromAliasID      string       `json:"fromAliasId"`
	TargetTableID    string       `json:"targetTableId"`
	KeyBindings      []KeyBinding `json:"keyBindings"`
	Cardinality      string       `json:"cardinality"`
	JoinType         string       `json:"joinType"`
	DisplayColumnIDs []string     `json:"displayColumnIds"`
	DetailColumnIDs  []string     `json:"detailColumnIds"`
	MatchConditions  *Condition   `json:"matchConditions,omitempty"`
}
type Source struct {
	Kind       string `json:"kind"`
	ColumnID   string `json:"columnId,omitempty"`
	RelationID string `json:"relationId,omitempty"`
}
type QueryColumn struct {
	ValueType      string          `json:"valueType,omitempty"`
	FieldID        string          `json:"fieldId"`
	Code           string          `json:"code,omitempty"`
	Source         Source          `json:"source"`
	Header         string          `json:"header"`
	ValueMode      string          `json:"valueMode,omitempty"`
	PreviewVisible *bool           `json:"previewVisible,omitempty"`
	ExportVisible  *bool           `json:"exportVisible,omitempty"`
	Format         json.RawMessage `json:"format,omitempty"`
	ExcelWidth     float64         `json:"excelWidth,omitempty"`
}
type Order struct {
	AliasID   string `json:"aliasId"`
	ColumnID  string `json:"columnId"`
	Direction string `json:"direction"`
}
type AllowedFilter struct {
	Label     string   `json:"label,omitempty"`
	AliasID   string   `json:"aliasId"`
	ColumnID  string   `json:"columnId"`
	Operators []string `json:"operators"`
}
type QuerySpec struct {
	Version           int             `json:"version"`
	Base              Base            `json:"base"`
	Relations         []Relation      `json:"relations"`
	Columns           []QueryColumn   `json:"columns"`
	Where             *Condition      `json:"where,omitempty"`
	OrderBy           []Order         `json:"orderBy,omitempty"`
	AllowedFilters    []AllowedFilter `json:"allowedFilters,omitempty"`
	AllowedSortFields []Order         `json:"allowedSortFields,omitempty"`
}
type Policy struct {
	ExportPolicy          string `json:"exportPolicy"`
	ResultRetentionHours  int    `json:"resultRetentionHours"`
	PurgeAfterExport      bool   `json:"purgeAfterExport"`
	SnapshotFormatVersion int    `json:"snapshotFormatVersion"`
}

func DefaultPolicy() Policy {
	return Policy{ExportPolicy: "MANUAL", ResultRetentionHours: 24, SnapshotFormatVersion: 2}
}

type QueryInputs struct {
	Where   *Condition `json:"where,omitempty"`
	OrderBy []Order    `json:"orderBy,omitempty"`
}
type Projection struct {
	Name     string `json:"name"`
	AliasID  string `json:"aliasId"`
	ColumnID string `json:"columnId"`
}
type Output struct {
	ValueName      string   `json:"valueName"`
	FieldID        string   `json:"fieldId"`
	Code           string   `json:"code"`
	Header         string   `json:"header"`
	Name           string   `json:"name"`
	OracleType     string   `json:"oracleType"`
	LogicalType    string   `json:"logicalType"`
	Precision      *int     `json:"precision,omitempty"`
	Scale          *int     `json:"scale,omitempty"`
	Nullable       bool     `json:"nullable"`
	RelationID     string   `json:"relationId,omitempty"`
	RawName        string   `json:"rawName,omitempty"`
	DisplayNames   []string `json:"displayNames,omitempty"`
	ValueMode      string   `json:"valueMode"`
	PreviewVisible bool     `json:"previewVisible"`
	ExportVisible  bool     `json:"exportVisible"`
}
type RelationSchema struct {
	Relation     Relation          `json:"relation"`
	SourceKeys   []string          `json:"sourceKeys"`
	TargetKeys   []string          `json:"targetKeys"`
	DisplayNames []string          `json:"displayNames"`
	DetailNames  map[string]string `json:"detailNames"`
}
type Compiled struct {
	CompilerVersion int                       `json:"compilerVersion"`
	Query           QuerySpec                 `json:"query"`
	Tables          map[string]Table          `json:"tables"`
	Policy          Policy                    `json:"policy"`
	Projections     []Projection              `json:"projections"`
	Outputs         []Output                  `json:"outputs"`
	Relations       map[string]RelationSchema `json:"relations"`
	SchemaHash      string                    `json:"schemaHash"`
}
type Plan struct {
	SQL     string
	Args    []any
	Columns []string
}
