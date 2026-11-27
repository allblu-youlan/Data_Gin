package tableboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}
type Metric struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	DatasourceID uint            `json:"datasourceId"`
	Owner        string          `json:"owner"`
	TableName    string          `json:"tableName"`
	BojunTableID uint            `json:"bojunTableId,omitempty"`
	ColumnIDs    map[string]uint `json:"columnIds,omitempty"`
	Aggregate    string          `json:"aggregate"`
	ValueField   string          `json:"valueField,omitempty"`
	TimeField    string          `json:"timeField"`
	TimeFormat   string          `json:"timeFormat,omitempty"`
	Timezone     string          `json:"timezone,omitempty"`
	Filters      []Filter        `json:"filters,omitempty"`
	Dimensions   []string        `json:"dimensions,omitempty"`
	Currency     string          `json:"currency,omitempty"`
}

func (m Metric) Fields() []string {
	f := []string{m.TimeField}
	if m.Aggregate != "COUNT" {
		f = append(f, m.ValueField)
	}
	for _, filter := range m.Filters {
		f = append(f, filter.Field)
	}
	return append(f, m.Dimensions...)
}

func validField(f string) bool { v, e := reportdataset.Identifier(f); return e == nil && v == f }

func (m Metric) Validate() error {
	if !id.MatchString(m.ID) || !validName(m.Name) || m.DatasourceID == 0 || !validField(m.Owner) || !validField(m.TableName) {
		return errors.New("请选择有效的指标、连接与表")
	}
	if !slices.Contains([]string{"SUM", "AVG", "COUNT", "COUNT_DISTINCT"}, m.Aggregate) {
		return errors.New("不支持的汇总方式")
	}
	if m.TimeFormat != "" && m.TimeFormat != "YYYYMMDD" {
		return errors.New("不支持的营业日格式")
	}
	if m.Timezone != "" {
		if _, e := time.LoadLocation(m.Timezone); e != nil || m.Timezone == "Local" {
			return errors.New("业务时区无效")
		}
	}
	if len(m.Filters) > 32 || len(m.Dimensions) > 16 {
		return errors.New("筛选条件或维度过多")
	}
	for _, field := range m.Fields() {
		if !validField(field) {
			return fmt.Errorf("无效的字段映射 %q", field)
		}
	}
	return validateFilters(m.Filters)
}
func validateFilters(filters []Filter) error {
	for _, f := range filters {
		if !validField(f.Field) || (f.Op != "EQ" && f.Op != "NE") || len(f.Value) > 1024 || f.Value == "" {
			return errors.New("筛选条件无效")
		}
	}
	return nil
}

// Effective replaces whole mapping properties, including empty slices/maps when a cell clears inherited mappings.
func (m Metric) Effective(overrides map[string]json.RawMessage) (Metric, error) {
	if len(overrides) == 0 {
		return m, nil
	}
	base, _ := json.Marshal(m)
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(base, &fields); e != nil {
		return Metric{}, e
	}
	for k, v := range overrides {
		switch k {
		case "datasourceId", "owner", "tableName", "bojunTableId", "columnIds", "aggregate", "valueField", "timeField", "timeFormat", "timezone", "filters", "dimensions":
		default:
			return Metric{}, fmt.Errorf("不支持覆盖 %s", k)
		}
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return Metric{}, fmt.Errorf("%s 的覆盖无效", k)
		}
		fields[k] = v
	}
	raw, e := json.Marshal(fields)
	if e != nil {
		return Metric{}, e
	}
	var out Metric
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&out); e != nil {
		return Metric{}, errors.New("字段覆盖格式无效")
	}
	return out, nil
}

func (m Metric) ForCell(cell Cell) (Metric, error) {
	if cell.Kind != "formula" || m.ID == "" || cell.MetricID != m.ID {
		return Metric{}, errors.New("指标引用无效")
	}
	if !slices.Contains([]string{"TODAY", "MONTH_TO_DATE", "YEAR_TO_DATE", "LAST_MONTH"}, cell.Period) {
		return Metric{}, errors.New("统计期间无效")
	}
	effective, e := m.Effective(cell.Overrides)
	if e != nil {
		return Metric{}, e
	}
	if e = effective.Validate(); e != nil {
		return Metric{}, e
	}
	if len(cell.Filters) > 16 {
		return Metric{}, errors.New("单元格筛选条件过多")
	}
	if e = validateFilters(cell.Filters); e != nil {
		return Metric{}, e
	}
	for _, f := range cell.Filters {
		if !slices.Contains(effective.Dimensions, f.Field) {
			return Metric{}, errors.New("单元格筛选字段不是已选维度")
		}
	}
	return effective, nil
}

func (m Metric) Bounds(period string, asOf time.Time) (time.Time, time.Time, error) {
	zone := m.Timezone
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return time.Time{}, time.Time{}, e
	}
	end := asOf.In(loc)
	y, month, d := end.Date()
	switch period {
	case "YEAR_TO_DATE":
		month, d = time.January, 1
	case "MONTH_TO_DATE":
		d = 1
	case "TODAY":
	case "LAST_MONTH":
		end = time.Date(y, month, 1, 0, 0, 0, 0, loc)
		return end.AddDate(0, -1, 0), end, nil
	default:
		return time.Time{}, time.Time{}, errors.New("统计期间无效")
	}
	return time.Date(y, month, d, 0, 0, 0, 0, loc), end, nil
}

func Address(row, column int) string {
	var letters string
	for n := column + 1; n > 0; n = (n - 1) / 26 {
		letters = string(rune('A'+(n-1)%26)) + letters
	}
	return strings.ToUpper(letters) + strconv.Itoa(row+1)
}
