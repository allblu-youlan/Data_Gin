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
