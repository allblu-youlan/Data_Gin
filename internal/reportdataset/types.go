package reportdataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Column struct {
	ID                 string   `json:"id"`
	BojunColumnID      string   `json:"bojunColumnId"`
	Name               string   `json:"name"`
	DBName             string   `json:"dbName"`
	Description        string   `json:"description"`
	OracleType         string   `json:"oracleType"`
	LogicalType        string   `json:"logicalType"`
	Precision          *int     `json:"precision,omitempty"`
	Scale              *int     `json:"scale,omitempty"`
	Nullable           bool     `json:"nullable"`
	IsPhysical         bool     `json:"isPhysical"`
	IsSelectable       bool     `json:"isSelectable"`
	RefTableID         string   `json:"refTableId,omitempty"`
	RefColumnID        string   `json:"refColumnId,omitempty"`
	Ordinal            int      `json:"ordinal"`
	SupportedOperators []string `json:"supportedOperators"`
}
type DataScope struct {
	Mode     string `json:"mode"`
	ColumnID string `json:"columnId,omitempty"`
	// UserValues is an administrator-controlled user-to-scope mapping; never accepted by run requests.
	UserValues map[string][]string `json:"userValues,omitempty"`
}
type TableConfig struct {
	PrimaryKeys           *[]string `json:"primaryKeys,omitempty"`
	DisplayFields         *[]string `json:"displayFields,omitempty"`
	SearchFields          *[]string `json:"searchFields,omitempty"`
	SelectableColumns     *[]string `json:"selectableColumns,omitempty"`
	DetailColumns         *[]string `json:"detailColumns,omitempty"`
	Enabled               bool      `json:"enabled"`
	DataScope             DataScope `json:"dataScope"`
	RequiredFilterColumns []string  `json:"requiredFilterColumns,omitempty"`
}
type Table struct {
	ID               string      `json:"id"`
	BojunTableID     string      `json:"bojunTableId"`
	LogicalName      string      `json:"logicalName"`
	Description      string      `json:"description"`
	Owner            string      `json:"owner"`
	PhysicalTable    string      `json:"physicalTable"`
	DBLink           string      `json:"dbLink"`
	PrimaryKeys      []string    `json:"primaryKeys"`
	DisplayFields    []string    `json:"displayFields"`
	SearchFields     []string    `json:"searchFields"`
	UniqueKeys       [][]string  `json:"uniqueKeys"`
	Columns          []Column    `json:"columns"`
	FixedCondition   *Condition  `json:"fixedCondition,omitempty"`
	Config           TableConfig `json:"config"`
	ValidationStatus string      `json:"validationStatus"`
	ValidationReason string      `json:"validationReason"`
}

func Hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var identifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]*$`)

func Identifier(v string) (string, error) {
	if !identifier.MatchString(v) || len(v) > 128 {
		return "", fmt.Errorf("REPORT_METADATA_UNSUPPORTED: invalid identifier")
	}
	return strings.ToUpper(v), nil
}
func Operators(t string) []string {
	switch t {
	case "integer", "decimal", "date", "datetime":
		return []string{"EQ", "NE", "GT", "GTE", "LT", "LTE", "BETWEEN", "IN", "NOT_IN", "IS_NULL", "IS_NOT_NULL"}
	default:
		return []string{"EQ", "NE", "IN", "NOT_IN", "PREFIX", "CONTAINS", "IS_NULL", "IS_NOT_NULL"}
	}
}
func LogicalType(t string, scale *int) string {
	switch {
	case t == "NUMBER":
		if scale != nil && *scale == 0 {
			return "integer"
		}
		return "decimal"
	case t == "DATE":
		return "date"
	case strings.HasPrefix(t, "TIMESTAMP"):
		return "datetime"
	default:
		return "string"
	}
}
func ColumnOperators(c Column) []string {
	if c.OracleType == "CLOB" || c.OracleType == "NCLOB" {
		return []string{"PREFIX", "CONTAINS", "IS_NULL", "IS_NOT_NULL"}
	}
	return Operators(c.LogicalType)
}
func SortableColumn(c Column) bool { return c.OracleType != "CLOB" && c.OracleType != "NCLOB" }
