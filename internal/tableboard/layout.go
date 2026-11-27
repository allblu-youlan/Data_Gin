// Package tableboard defines the persisted layout and binding contract for personal boards.
package tableboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"gin-biz-web-api/internal/reportdataset"
)

type Layout struct {
	ReportDate string    `json:"reportDate,omitempty"`
	Metrics    []Metric  `json:"metrics,omitempty"`
	Sources    []Source  `json:"sources"`
	Sections   []Section `json:"sections"`
	Foreground string    `json:"foreground"`
	Background string    `json:"background"`
}

type Source struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	DatasourceID uint     `json:"datasourceId"`
	Owner        string   `json:"owner"`
	TableName    string   `json:"tableName"`
	Fields       []string `json:"fields"`
	OrderBy      string   `json:"orderBy"`
	Limit        int      `json:"limit"`
}

type Section struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Cells [][]Cell `json:"cells"`
}

type Cell struct {
	Expression string                     `json:"expression,omitempty"`
	YearOffset int                        `json:"yearOffset,omitempty"`
	MetricID   string                     `json:"metricId,omitempty"`
	Period     string                     `json:"period,omitempty"`
	Filters    []Filter                   `json:"filters,omitempty"`
	Format     string                     `json:"format,omitempty"`
	Overrides  map[string]json.RawMessage `json:"overrides,omitempty"`
	Kind       string                     `json:"kind"`
	Text       string                     `json:"text"`
	SourceID   string                     `json:"sourceId,omitempty"`
	Field      string                     `json:"field,omitempty"`
	Row        int                        `json:"row"`
}

var (
	color = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	id    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$`)
)

func validName(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= 100
}

func (s Source) Validate() error {
	if !id.MatchString(s.ID) || !validName(s.Name) || s.DatasourceID == 0 {
		return errors.New("请选择数据库连接并填写有效的数据源名称和标识")
	}
	if len(s.Fields) < 1 || len(s.Fields) > 32 || s.Limit < 1 || s.Limit > 100 {
		return errors.New("每个数据源需要选择 1 至 32 个字段，读取 1 至 100 行")
	}
	if _, err := reportdataset.Identifier(s.Owner); err != nil {
		return errors.New("表所属用户无效")
	}
	if _, err := reportdataset.Identifier(s.TableName); err != nil {
		return errors.New("表名无效")
	}
	seen := make(map[string]bool, len(s.Fields))
	for _, field := range s.Fields {
		normalized, err := reportdataset.Identifier(field)
		if err != nil || seen[normalized] {
			return errors.New("字段名无效或重复")
		}
		seen[normalized] = true
	}
	if !slices.Contains(s.Fields, s.OrderBy) {
		return errors.New("请从已选字段中选择排序字段")
	}
	return nil
}

func (l Layout) Validate() error {
	if err := ValidateReportDate(l.ReportDate); err != nil {
		return err
	}
	if !color.MatchString(l.Foreground) || !color.MatchString(l.Background) {
		return errors.New("颜色需要使用六位十六进制色值")
	}
	if len(l.Sections) < 1 || len(l.Sections) > 20 || l.Sources == nil || len(l.Sources) > 16 {
		return errors.New("看板需要 1 至 20 个区块和最多 16 个数据源")
	}
	sources := make(map[string]Source, len(l.Sources))
	for _, source := range l.Sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if _, exists := sources[source.ID]; exists {
			return errors.New("数据源标识重复")
		}
		sources[source.ID] = source
	}
	metrics := make(map[string]Metric, len(l.Metrics))
	if len(l.Metrics) > 128 {
		return errors.New("每个看板最多支持128个指标")
	}
	for _, m := range l.Metrics {
		if _, exists := metrics[m.ID]; exists {
			return errors.New("指标标识重复")
		}
		if err := m.Validate(); err != nil {
			return err
		}
		metrics[m.ID] = m
	}
	ids := make(map[string]bool, len(l.Sections))
	total := 0
	for _, section := range l.Sections {
		if ids[section.ID] || !id.MatchString(section.ID) || !validName(section.Title) {
			return errors.New("区块标识重复或区块名称无效")
		}
		ids[section.ID] = true
		if err := section.validateGrid(sources, metrics); err != nil {
			return fmt.Errorf("%s：%w", section.Title, err)
		}
		total += len(section.Cells) * len(section.Cells[0])
	}
	if total > 10000 {
		return errors.New("每个看板最多容纳 10000 个单元格")
	}
	return nil
}

func (s Section) validateGrid(sources map[string]Source, metrics map[string]Metric) error {
	if len(s.Cells) < 1 || len(s.Cells) > 100 || len(s.Cells[0]) < 1 || len(s.Cells[0]) > 32 {
		return errors.New("区块需要 1 至 100 行、1 至 32 列")
	}
	for r, row := range s.Cells {
		if len(row) != len(s.Cells[0]) {
			return errors.New("同一区块各行的列数需要一致")
		}
		for c, cell := range row {
			switch cell.Kind {
			case "text":
				if utf8.RuneCountInString(cell.Text) > 2000 {
					return fmt.Errorf("第 %d 行第 %d 列的文本超过 2000 字", r+1, c+1)
				}
			case "expression":
				if !validDisplayFormat(cell.Format) {
					return fmt.Errorf("%s 的展示格式无效", Address(r, c))
				}
			case "formula":
				if _, err := metrics[cell.MetricID].ForCell(cell); err != nil {
					return fmt.Errorf("%s：%w", Address(r, c), err)
				}
			case "field":
				source, exists := sources[cell.SourceID]
				if !exists || !slices.Contains(source.Fields, cell.Field) || cell.Row < 0 || cell.Row >= source.Limit {
					return fmt.Errorf("第 %d 行第 %d 列的绑定字段或数据行无效", r+1, c+1)
				}
			default:
				return errors.New("单元格类型无效")
			}
		}
	}
	_, err := CompileExpressions(s)
	return err
}
