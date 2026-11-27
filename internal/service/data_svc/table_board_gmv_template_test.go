package data_svc

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gin-biz-web-api/internal/tableboard"
)

// Compile the actual frontend builder into a temporary directory. This checks
// the JSON contract without a second copy of the template or its source workbook.
func gmvTestLayouts(t *testing.T) []tableboard.Layout {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("install Node and web dependencies for the GMV template integration")
	}
	web := filepath.Join("..", "..", "..", "web")
	tsc := filepath.Join(web, "node_modules", "typescript", "bin", "tsc")
	if _, err = os.Stat(tsc); os.IsNotExist(err) {
		t.Skip("install web dependencies for the GMV template integration")
	}
	dir := t.TempDir()
	compile := exec.CommandContext(t.Context(), node, tsc, filepath.Join(web, "src", "tableBoard", "gmvTemplate.ts"), "--outDir", dir, "--skipLibCheck", "--target", "ES2020", "--module", "CommonJS")
	if log, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile template: %v\n%s", err, log)
	}
	const generate = `const {buildGmvTemplate,roleFields,gmvTable}=require(process.argv[1]);
const source={datasourceId:7,owner:"OPS",tableName:gmvTable,fields:roleFields};
process.stdout.write(JSON.stringify([false,true].map(history=>buildGmvTemplate(source,"2026-11-15",history).layout)));`
	raw, err := exec.CommandContext(t.Context(), node, "-e", generate, filepath.Join(dir, "gmvTemplate.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("build template: %v\n%s", err, raw)
	}
	var layouts []tableboard.Layout
	if err = json.Unmarshal(raw, &layouts); err != nil {
		t.Fatal(err)
	}
	return layouts
}

func TestGMVGeneratedTemplateIntegration(t *testing.T) {
	for i, layout := range gmvTestLayouts(t) {
		history := i == 1
		if err := layout.Validate(); err != nil {
			t.Fatalf("template validation: %v", err)
		}
		calls := 0
		failed := false
		run := func(ctx context.Context, actor uint, r aggregateRequest, asOf time.Time) (map[string]BoardCellResult, error) {
			calls++
			start, end, err := r.metric.ReportBounds(r.period, asOf, r.reportDate, r.yearOffset)
			if err != nil {
				return nil, err
			}
			if r.groupField != "SECOND_LEVEL_CHANNEL" || len(r.groupValues) != 50 || r.reportDate != "2026-11-15" {
				t.Errorf("unexpected channel batch: %s count=%d date=%s", r.groupField, len(r.groupValues), r.reportDate)
			}
			amount, ok := map[string]int{"gmv": 200, "before-refund": 20, "after-refund": 10}[r.metric.ID]
			if !ok {
				t.Errorf("unexpected metric %q", r.metric.ID)
			}
			if r.yearOffset == -1 {
				amount /= 2
			}
			out := map[string]BoardCellResult{}
			for _, channel := range r.groupValues {
				n := amount
				if channel == "1.01 天猫店销（除购物金）" {
					n *= 2
					if failed && r.metric.ID == "gmv" && r.period == "YEAR_TO_DATE" {
						out[channel] = BoardCellResult{Status: "error", Message: "字段权限撤销"}
						continue
					}
				}
				raw := strconv.Itoa(n)
				out[channel] = BoardCellResult{Status: "ok", Value: &raw, Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339)}
			}
			return out, nil
		}
		now := time.Date(2026, 11, 28, 4, 0, 0, 0, time.UTC)
		cells := evaluateBoard(t.Context(), 1, layout, now, run, nil)
		snapshot, err := BuildBoardSnapshot(layout, cells)
		if err != nil {
			t.Fatal(err)
		}
		rows := snapshot.Sections[0].Rows
		for _, item := range []struct {
			col, row int
			want     string
		}{{1, 64, "10,200.00"}, {2, 64, "100.00%"}, {7, 64, "15.00%"}, {1, 71, "10,200.00"}, {1, 7, "600.00"}, {20, 64, "1,020.00"}, {21, 64, "510.00"}} {
			if got := rows[item.row-1][item.col]; got != item.want {
				t.Errorf("%s=%s want %s", tableboard.Address(item.row-1, item.col), got, item.want)
			}
		}
		if history {
			if rows[63][10] != "5,100.00" || rows[63][17] != "100.00%" || calls != 10 {
				t.Errorf("history K64=%s R64=%s queries=%d", rows[63][10], rows[63][17], calls)
			}
		} else {
			if calls != 6 {
				t.Errorf("no-history template queried %d batches; want 6", calls)
			}
			for _, col := range []int{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 22, 23} {
				if rows[63][col] != "待接入历史数据" {
					t.Fatalf("unexpected %s", rows[63][col])
				}
			}
		}
		failed = true
		cells = evaluateBoard(t.Context(), 1, layout, now, run, nil)
		status := map[string]string{}
		for _, cell := range cells {
			status[cell.Address] = cell.Status
		}
		for _, address := range []string{"B8", "B7", "H8", "B64", "H64", "B71"} {
			if status[address] != "error" {
				t.Errorf("%s did not propagate channel failure: %s", address, status[address])
			}
		}
		if status["B9"] != "ok" || status["U64"] != "ok" {
			t.Fatal("unrelated facts failed")
		}
	}
}
