package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"
)

type KaiyuPassengerExport struct {
	FileName string
	Content  []byte
}

func (service *KaiyuPassengerQueryService) Export(ctx context.Context, actor uint, input KaiyuPassengerQuery) (*KaiyuPassengerExport, error) {
	result, err := service.Query(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	content, err := renderKaiyuPassengerWorkbook(ctx, result)
	if err != nil {
		return nil, fmt.Errorf("%w: render workbook: %v", ErrKaiyuPassengerUnavailable, err)
	}
	return &KaiyuPassengerExport{FileName: fmt.Sprintf("kaiyu_passenger_%s_%s.xlsx", input.StartDate, input.EndDate), Content: content}, nil
}

type kaiyuPassengerSheet struct {
	name           string
	headers        []string
	count          int
	row            func(int) []any
	percentColumns map[int]bool
}

func renderKaiyuPassengerWorkbook(ctx context.Context, result *KaiyuPassengerResult) (content []byte, resultErr error) {
	file := excelize.NewFile()
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	styles, err := newMallWeatherExportExcelStyles(file)
	if err != nil {
		return nil, err
	}
	sheets := []kaiyuPassengerSheet{
		{
			name: "门店汇总",
			headers: []string{"门店ID", "门店名称", "开域店铺ID", "开域店铺名称", "客流量", "入店人次",
				"入店人数（小时累计）", "入店批次数", "入店率", "小时记录数"},
			count: len(result.Malls), percentColumns: map[int]bool{8: true},
			row: func(i int) []any {
				mall := result.Malls[i]
				return []any{mall.MallID, mall.MallName, mall.ShopID, mall.ShopName, mall.Passby, mall.Traffic,
					mall.TrafficCount, mall.TrafficBatch, mall.CaptureRate, mall.RecordCount}
			},
		},
		{
			name:    "小时趋势",
			headers: []string{"小时（上海时间）", "客流量", "入店人次", "入店人数（小时累计）", "入店批次数", "入店率", "小时记录数"},
			count:   len(result.Trend), percentColumns: map[int]bool{5: true},
			row: func(i int) []any {
				hour := result.Trend[i]
				return []any{hour.HourStart.In(kaiyuLocation).Format(time.DateTime), hour.Passby, hour.Traffic,
					hour.TrafficCount, hour.TrafficBatch, hour.CaptureRate, hour.RecordCount}
			},
		},
		{
			name: "小时明细",
			headers: []string{"门店ID", "门店名称", "开域店铺ID", "开域店铺名称", "小时（上海时间）", "客流量",
				"入店人次", "入店人数", "入店批次数", "入店率", "平均驻店时长（秒）", "新客占比", "深度访问占比",
				"销售额（分）", "客单数", "客单价（分）", "转化率"},
			count: len(result.Items), percentColumns: map[int]bool{9: true, 11: true, 12: true, 16: true},
			row: func(i int) []any {
				hour := result.Items[i]
				return []any{hour.MallID, hour.MallName, hour.ShopID, hour.ShopName, hour.HourStart.In(kaiyuLocation).Format(time.DateTime),
					hour.Passby, hour.Traffic, hour.TrafficCount, hour.TrafficBatch, hour.CaptureRate, hour.AvgVisitDuration,
					hour.NewVisitorsRatio, hour.DeepVisitRatio, hour.NetSales, hour.SalesTransactions, hour.ATV, hour.ShopperYield}
			},
		},
	}
	for i, sheet := range sheets {
		if i == 0 {
			err = file.SetSheetName("Sheet1", sheet.name)
		} else {
			_, err = file.NewSheet(sheet.name)
		}
		if err != nil {
			return nil, err
		}
		if err := writeKaiyuPassengerSheet(ctx, file, sheet, styles); err != nil {
			return nil, err
		}
	}
	if err := file.SetDocProps(&excelize.DocProperties{
		Title: "监控客流", Subject: result.StartDate + " 至 " + result.EndDate + "（Asia/Shanghai）",
		Description: "按授权门店范围查询；入店率=入店人次/客流量；入店人数按小时累计，不代表跨小时去重人数。", Creator: "Data Gin",
	}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeKaiyuPassengerSheet(ctx context.Context, file *excelize.File, sheet kaiyuPassengerSheet, styles mallWeatherExportExcelStyles) error {
	writer, err := file.NewStreamWriter(sheet.name)
	if err != nil {
		return err
	}
	if err := writer.SetColWidth(1, len(sheet.headers), 22); err != nil {
		return err
	}
	if err := file.SetPanes(sheet.name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	header := make([]any, len(sheet.headers))
	for i, value := range sheet.headers {
		header[i] = excelize.Cell{StyleID: styles.Header, Value: value}
	}
	if err := writer.SetRow("A1", header); err != nil {
		return err
	}
	for i := 0; i < sheet.count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		values := sheet.row(i)
		for column, value := range values {
			style := styles.Integer
			switch value.(type) {
			case string:
				style = styles.Text
			case float64:
				style = styles.Decimal
			}
			if sheet.percentColumns[column] {
				style = styles.Percent
			}
			values[column] = excelize.Cell{StyleID: style, Value: value}
		}
		if err := writer.SetRow("A"+strconv.Itoa(i+2), values); err != nil {
			return err
		}
	}
	return writer.Flush()
}
