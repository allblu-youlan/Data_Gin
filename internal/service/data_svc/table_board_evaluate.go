package data_svc

import (
	"context"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/tableboard"
	"slices"
	"sync"
	"time"
)

type boardJob struct {
	request aggregateRequest
	indices map[string][]int
}
type boardAction func()
type boardRunner func(context.Context, uint, aggregateRequest, time.Time) (map[string]BoardCellResult, error)

func EvaluateBoard(ctx context.Context, actor uint, layout tableboard.Layout, asOf time.Time) []BoardCellResult {
	return evaluateBoard(ctx, actor, layout, asOf, func(ctx context.Context, actor uint, r aggregateRequest, t time.Time) (map[string]BoardCellResult, error) {
		return r.execute(ctx, actor, t)
	}, QueryBoardSource)
}

func evaluateBoard(ctx context.Context, actor uint, layout tableboard.Layout, asOf time.Time, run boardRunner, detail func(context.Context, uint, tableboard.Source) ([]map[string]*string, error)) []BoardCellResult {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	metrics := map[string]tableboard.Metric{}
	for _, m := range layout.Metrics {
		metrics[m.ID] = m
	}
	jobs := map[string]*boardJob{}
	type binding struct {
		index int
		cell  tableboard.Cell
	}
	details := map[string][]binding{}
	out := []BoardCellResult{}
	for _, section := range layout.Sections {
		for y, row := range section.Cells {
			for x, c := range row {
				if c.Kind == "text" {
					continue
				}
				i := len(out)
				out = append(out, BoardCellResult{SectionID: section.ID, Address: tableboard.Address(y, x), Status: "error", Message: "配置无效"})
				if c.Kind == "field" {
					details[c.SourceID] = append(details[c.SourceID], binding{i, c})
					continue
				}
				m, err := metrics[c.MetricID].ForCell(c)
				if err != nil {
					out[i].Message = err.Error()
					continue
				}
				r := aggregateRequest{metric: m, period: c.Period, filters: c.Filters}
				group := ""
				if len(c.Filters) == 1 && c.Filters[0].Op == "EQ" {
					r.groupField = c.Filters[0].Field
					group = c.Filters[0].Value
					r.filters = nil
				}
				key := reportdataset.Hash([]any{m, r.period, r.filters, r.groupField})
				job := jobs[key]
				if job == nil {
					job = &boardJob{r, map[string][]int{}}
					jobs[key] = job
				}
				if r.groupField != "" && job.indices[group] == nil {
					job.request.groupValues = append(job.request.groupValues, group)
				}
				job.indices[group] = append(job.indices[group], i)
			}
		}
	}
	put := func(i int, r BoardCellResult) {
		r.SectionID = out[i].SectionID
		r.Address = out[i].Address
		out[i] = r
	}
	// Run independent datasources concurrently, with serial work per datasource
	// to avoid competing with this same refresh for cross-replica slots.
	actions := map[uint][]boardAction{}
	for _, job := range jobs {
		groups := job.request.groupValues
		if len(groups) == 0 {
			groups = []string{""}
		}
		for chunk := range slices.Chunk(groups, 500) {
			r := job.request
			if r.groupField != "" {
				r.groupValues = chunk
			}
			actions[r.metric.DatasourceID] = append(actions[r.metric.DatasourceID], func() {
				result, err := run(ctx, actor, r, asOf)
				for _, group := range chunk {
					v := result[group]
					if err != nil {
						v = BoardCellResult{Status: "error", Message: safeBoardMessage(err)}
					}
					for _, i := range job.indices[group] {
						put(i, v)
					}
				}
			})
		}
	}
	for _, s := range layout.Sources {
		binds := details[s.ID]
		if len(binds) == 0 {
			continue
		}
		actions[s.DatasourceID] = append(actions[s.DatasourceID], func() {
			rows, err := detail(ctx, actor, s)
			for _, b := range binds {
				v := BoardCellResult{Status: "null"}
				if err != nil {
					v.Status = "error"
					v.Message = safeBoardMessage(err)
				} else if row := b.cell.Row; row >= 0 && row < len(rows) {
					v.Value = rows[row][b.cell.Field]
					if v.Value != nil {
						v.Status = "ok"
					}
				}
				put(b.index, v)
			}
		})
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, tasks := range actions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			for _, f := range tasks {
				f()
			}
		}()
	}
	wg.Wait()
	return out
}

func PreviewBoard(ctx context.Context, actor uint, metric tableboard.Metric, cell tableboard.Cell, asOf time.Time) (BoardCellResult, BoardCellResult, error) {
	if err := metric.Validate(); err != nil {
		return BoardCellResult{}, BoardCellResult{}, err
	}
	if _, err := metric.ForCell(cell); err != nil {
		return BoardCellResult{}, BoardCellResult{}, err
	}
	before := cell
	before.Overrides = nil
	layout := tableboard.Layout{Metrics: []tableboard.Metric{metric}, Sections: []tableboard.Section{{ID: "preview", Cells: [][]tableboard.Cell{{before, cell}}}}}
	results := EvaluateBoard(ctx, actor, layout, asOf)
	if len(results) != 2 {
		return BoardCellResult{}, BoardCellResult{}, fmt.Errorf("预览配置无效")
	}
	for i := range results {
		results[i].Display = boardDisplay(results[i], cell.Format)
	}
	return results[0], results[1], nil
}
