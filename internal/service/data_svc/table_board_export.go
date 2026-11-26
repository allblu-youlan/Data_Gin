package data_svc

import (
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

type BoardExportSheet struct {
	Title string     `json:"title"`
	Rows  [][]string `json:"rows"`
}
type BoardExport struct {
	Foreground string             `json:"foreground"`
	Background string             `json:"background"`
	Sections   []BoardExportSheet `json:"sections"`
}

var boardExportColor = regexp.MustCompile("^#[0-9a-fA-F]{6}$")

func (input BoardExport) Validate() error {
	if len(input.Sections) < 1 || len(input.Sections) > 20 || !boardExportColor.MatchString(input.Foreground) || !boardExportColor.MatchString(input.Background) {
		return fmt.Errorf("区块数或看板配色无效")
	}
	count := 0
	for _, section := range input.Sections {
		if len(section.Rows) < 1 || len(section.Rows) > 100 || utf8.RuneCountInString(section.Title) > 100 {
			return fmt.Errorf("区块名称或行数超出限制")
		}
		for _, row := range section.Rows {
			count += len(row)
			if len(row) < 1 || len(row) > 32 || count > 10000 {
				return fmt.Errorf("看板单元格数量超出限制")
			}
			for _, v := range row {
				if utf8.RuneCountInString(v) > 32767 {
					return fmt.Errorf("单元格超过 Excel 字数限制")
				}
			}
		}
	}
	return nil
}

func RenderBoardXLSX(input BoardExport) ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	book := excelize.NewFile()
	defer book.Close()
	style, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Color: input.Foreground[1:]}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{input.Background[1:]}}, Alignment: &excelize.Alignment{WrapText: true}})
	if err != nil {
		return nil, err
	}
	for i, section := range input.Sections {
		// The numeric prefix disambiguates repeated titles even after sanitizing.
		name := truncateMallWeatherExportRunes(strconv.Itoa(i+1)+" "+sanitizeMallWeatherExportSheetName(section.Title), 31)
		if i == 0 {
			err = book.SetSheetName("Sheet1", name)
		} else {
			_, err = book.NewSheet(name)
		}
		if err != nil {
			return nil, err
		}
		width := 1
		for r, row := range section.Rows {
			if len(row) > width {
				width = len(row)
			}
			for c, value := range row {
				pos, _ := excelize.CoordinatesToCellName(c+1, r+1)
				if err := book.SetCellStr(name, pos, value); err != nil {
					return nil, err
				}
			}
		}
		end, _ := excelize.CoordinatesToCellName(width, len(section.Rows))
		last, _ := excelize.ColumnNumberToName(width)
		if err := book.SetColWidth(name, "A", last, 20); err != nil {
			return nil, err
		}
		if err := book.SetCellStyle(name, "A1", end, style); err != nil {
			return nil, err
		}
	}
	buf, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
