package data_svc

import "gin-biz-web-api/internal/tableboard"

// Resolve cell dependencies only after all facts and detail rows have arrived.
// Every expression sees the same inputs that are sent to the snapshot exporter.
func resolveBoardExpressions(layout tableboard.Layout, results []BoardCellResult) {
	indices := map[[2]string]int{}
	for i, r := range results {
		indices[[2]string{r.SectionID, r.Address}] = i
	}
	for _, section := range layout.Sections {
		plan, err := tableboard.CompileExpressions(section)
		if err != nil {
			for y, row := range section.Cells {
				for x, c := range row {
					if c.Kind == "expression" {
						i := indices[[2]string{section.ID, tableboard.Address(y, x)}]
						results[i].Message = err.Error()
					}
				}
			}
			continue
		}
		computed := plan.Evaluate(func(pos tableboard.Position) tableboard.ExpressionValue {
			c := section.Cells[pos.Row][pos.Column]
			if c.Kind == "text" {
				return tableboard.ExpressionValue{Status: "ok", Value: c.Text}
			}
			address := tableboard.Address(pos.Row, pos.Column)
			r := results[indices[[2]string{section.ID, address}]]
			v := tableboard.ExpressionValue{Status: r.Status, Message: r.Message}
			if r.Status != "ok" {
				v.Message = address + "：" + r.Message
			}
			if r.Value != nil {
				v.Value = *r.Value
			}
			return v
		})
		for pos, v := range computed {
			r := &results[indices[[2]string{section.ID, tableboard.Address(pos.Row, pos.Column)}]]
			r.Status = v.Status
			r.Message = v.Message
			if v.Status == "ok" {
				r.Value = &v.Value
			}
		}
	}
}
