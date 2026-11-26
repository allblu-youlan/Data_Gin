package tableboard

import "testing"

func sampleLayout() Layout {
	return Layout{
		Foreground: "#171717", Background: "#ffffff",
		Sources:  []Source{{ID: "s1", Name: "订单", DatasourceID: 1, Owner: "SALES", TableName: "ORDERS", Fields: []string{"ID"}, OrderBy: "ID", Limit: 10}},
		Sections: []Section{{ID: "first", Title: "第一张表", Cells: [][]Cell{{{Kind: "text", Text: "订单号"}, {Kind: "field", SourceID: "s1", Field: "ID", Row: 0}}}}},
	}
}

func TestLayoutValidation(t *testing.T) {
	if err := sampleLayout().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		modify func(l *Layout)
	}{
		{"invalid color", func(l *Layout) { l.Background = "blue" }},
		{"missing source", func(l *Layout) { l.Sources = []Source{} }},
		{"unknown field", func(l *Layout) { l.Sections[0].Cells[0][1].Field = "NAME" }},
		{"row outside query", func(l *Layout) { l.Sections[0].Cells[0][1].Row = 10 }},
		{"uneven rows", func(l *Layout) { l.Sections[0].Cells = append(l.Sections[0].Cells, []Cell{}) }},
		{"duplicate section", func(l *Layout) { l.Sections = append(l.Sections, l.Sections[0]) }},
		{"duplicate source", func(l *Layout) { l.Sources = append(l.Sources, l.Sources[0]) }},
		{"unknown sort", func(l *Layout) { l.Sources[0].OrderBy = "NAME" }},
		{"invalid table name", func(l *Layout) { l.Sources[0].TableName = "SALES.ORDERS" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout := sampleLayout()
			tc.modify(&layout)
			if layout.Validate() == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
