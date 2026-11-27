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
	Format       string          `json:"format,omitempty"`
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
	if !validDisplayFormat(m.Format) {
		return errors.New("指标展示格式无效")
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
	if !validDisplayFormat(cell.Format) {
		return Metric{}, errors.New("单元格展示格式无效")
	}
	if cell.Kind != "formula" || m.ID == "" || cell.MetricID != m.ID {
		return Metric{}, errors.New("指标引用无效")
	}
	if !validPeriod(cell.Period) || (cell.YearOffset != 0 && cell.YearOffset != -1) {
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
	return m.ReportBounds(period, asOf, "", 0)
}

func Address(row, column int) string {
	var letters string
	for n := column + 1; n > 0; n = (n - 1) / 26 {
		letters = string(rune('A'+(n-1)%26)) + letters
	}
	return strings.ToUpper(letters) + strconv.Itoa(row+1)
}

func validDisplayFormat(format string) bool {
	return slices.Contains([]string{"", "money_2", "integer", "number", "percent_2"}, format)
}

// DisplayFormat resolves explicit cell formatting before the shared default.
func (m Metric) DisplayFormat(cell Cell) string {
	if cell.Format != "" {
		return cell.Format
	}
	if m.Format != "" {
		return m.Format
	}
	if m.Aggregate == "SUM" || m.Aggregate == "AVG" {
		return "money_2"
	}
	return "integer"
}
