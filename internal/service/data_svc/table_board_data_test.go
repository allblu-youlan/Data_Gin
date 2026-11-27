package data_svc

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/tableboard"
	"github.com/xuri/excelize/v2"
)

func TestBoardSelectUsesMetadataAndStableBoundedWindow(t *testing.T) {
	source := tableboard.Source{ID: "store", Name: "销售", DatasourceID: 2, Owner: "OPS", TableName: "SALES", Fields: []string{"NAME", "ID"}, OrderBy: "ID", Limit: 20}
	schema := []reportoracle.ResultColumn{{Name: "ID", DataType: "NUMBER"}, {Name: "NAME", DataType: "VARCHAR2"}}
	query, err := boardSelect(source, schema)
	if err != nil || !strings.Contains(query, `ORDER BY "ID", "NAME") WHERE ROWNUM <= :board_limit`) {
		t.Fatalf("query %q, err %v", query, err)
	}
	if _, err := boardSelect(source, schema[:1]); !errors.Is(err, ErrBoardSourceInvalid) {
		t.Fatal("missing field accepted", err)
	}
	source.OrderBy = "unknown"
	if _, err := boardSelect(source, schema); !errors.Is(err, ErrBoardSourceInvalid) {
		t.Fatal("unknown ordering accepted", err)
	}
}

func TestBoardXLSXPreservesSectionValuesAndStyles(t *testing.T) {
	section := BoardExportSheet{Title: "销售/额", Rows: [][]string{{"=1+2", "0001"}, {"", "中文"}}}
	data, err := RenderBoardXLSX(BoardExport{Sections: []BoardExportSheet{section, section}, Foreground: "#112233", Background: "#aabbee"})
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	names := book.GetSheetList()
	if len(names) != 2 || names[0] == names[1] {
		t.Fatal(names)
	}
	for _, name := range names {
		value, _ := book.GetCellValue(name, "A1")
		formula, _ := book.GetCellFormula(name, "A1")
		id, _ := book.GetCellValue(name, "B1")
		if value != "=1+2" || formula != "" || id != "0001" {
			t.Fatal(value, formula, id)
		}
		styleID, err := book.GetCellStyle(name, "B2")
		if err != nil || styleID == 0 {
			t.Fatal("missing style", err)
		}
	}
}
