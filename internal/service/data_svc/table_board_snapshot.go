package data_svc

import (
	"bytes"
	"encoding/csv"
	"errors"
	"math/big"
	"strconv"
	"strings"

	"gin-biz-web-api/internal/tableboard"
)

type BoardCellResult struct {
	SectionID string  `json:"sectionId"`
	Address   string  `json:"address"`
	Status    string  `json:"status"`
	Value     *string `json:"value"`
	Display   string  `json:"display"`
	Message   string  `json:"message,omitempty"`
	Start     string  `json:"start,omitempty"`
	End       string  `json:"end,omitempty"`
	Matched   int64   `json:"matched"`
}

type boardNumber struct {
	Raw    string
	Format string
}

// Format from the Oracle decimal string, preserving precision through rounding.
func boardDisplay(result BoardCellResult, format string) string {
	if result.Status == "error" {
		return "读取失败"
	}
	if result.Status == "null" || result.Value == nil {
		return "—"
	}
	value := *result.Value
	if format != "money_2" && format != "integer" {
		return value
	}
	number, ok := new(big.Rat).SetString(value)
	if !ok {
		return value
	}
	digits := 0
	if format == "money_2" {
		digits = 2
	}
	whole, part, _ := strings.Cut(number.FloatString(digits), ".")
	sign := ""
	if strings.HasPrefix(whole, "-") {
		sign = "-"
		whole = whole[1:]
	}
	var display strings.Builder
	display.WriteString(sign)
	for i := range len(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			display.WriteByte(',')
		}
		display.WriteByte(whole[i])
	}
	if part != "" {
		display.WriteByte('.')
		display.WriteString(part)
	}
	return display.String()
}

// Assemble the downloadable rows and the API display values together. Legacy
// text and field cells retain their string type, including leading zeroes.
func BuildBoardSnapshot(layout tableboard.Layout, results []BoardCellResult) (BoardExport, error) {
	indices := make(map[[2]string]int, len(results))
	for i, r := range results {
		indices[[2]string{r.SectionID, r.Address}] = i
	}
	out := BoardExport{Foreground: layout.Foreground, Background: layout.Background}
	for _, section := range layout.Sections {
		sheet := BoardExportSheet{Title: section.Title, Numbers: map[string]boardNumber{}}
		for row, cells := range section.Cells {
			values := make([]string, len(cells))
			for col, cell := range cells {
				if cell.Kind == "text" {
					values[col] = cell.Text
					continue
				}
				address := tableboard.Address(row, col)
				i, ok := indices[[2]string{section.ID, address}]
				if !ok {
					return BoardExport{}, errors.New("单元格计算结果缺失")
				}
				result := &results[i]
				result.Display = boardDisplay(*result, cell.Format)
				values[col] = result.Display
				if cell.Kind == "formula" && result.Status == "ok" && result.Value != nil {
					raw := *result.Value
					// Excel numbers have at most 15 significant decimal digits. Use the
					// displayed text for larger values so reopening the workbook stays exact.
					mantissa, _, _ := strings.Cut(strings.ToUpper(raw), "E")
					significant := strings.Trim(strings.ReplaceAll(strings.TrimLeft(mantissa, "+-"), ".", ""), "0")
					if _, err := strconv.ParseFloat(raw, 64); err == nil && len(significant) <= 15 {
						sheet.Numbers[address] = boardNumber{Raw: raw, Format: cell.Format}
					}
				}
			}
			sheet.Rows = append(sheet.Rows, values)
		}
		out.Sections = append(out.Sections, sheet)
	}
	return out, out.Validate()
}

func RenderBoardCSV(input BoardExport) ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	writer := csv.NewWriter(&buf)
	for _, section := range input.Sections {
		if err := writer.Write([]string{section.Title}); err != nil {
			return nil, err
		}
		if err := writer.WriteAll(section.Rows); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buf.Bytes(), writer.Error()
}
