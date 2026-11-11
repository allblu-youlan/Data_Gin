package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

func (service *KaiyuRegionService) Export(ctx context.Context, actor uint, input KaiyuRegionQuery) (*KaiyuPassengerExport, error) {
	result, err := service.Query(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	status, err := service.Missing(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	content, err := renderKaiyuRegionWorkbook(ctx, result, status)
	if err != nil {
		return nil, err
	}
	return &KaiyuPassengerExport{FileName: fmt.Sprintf("kaiyu_region_%s_%s.xlsx", input.StartDate, input.EndDate), Content: content}, nil
}
func renderKaiyuRegionWorkbook(ctx context.Context, result *KaiyuRegionResult, status *KaiyuRegionMissingResult) (content []byte, resultErr error) {
	file := excelize.NewFile()
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	styles, err := newMallWeatherExportExcelStyles(file)
	if err != nil {
		return nil, err
	}
	sheets := []kaiyuPassengerSheet{
		{name: "区域明细", headers: []string{"门店ID", "门店名称", "开域店铺ID", "统计日期（上海）", "区域名称", "曝光人数", "驻足人数", "驻足率", "平均停留时长（秒）", "拉取时间（上海）"}, count: len(result.Items), percentColumns: map[int]bool{7: true}, row: func(i int) []any {
			row := result.Items[i]
			return []any{row.MallID, row.MallName, row.ShopID, row.StatDate, row.RegionName, row.ExposureCount, row.StayCount, row.StayRate, row.AvgStayTime, row.FetchedAt.In(kaiyuLocation).Format(time.DateTime)}
		}},
		{name: "同步状态", headers: []string{"门店ID", "门店名称", "开域店铺ID", "统计日期（上海）", "最近状态", "区域记录数", "最近拉取时间（上海）", "最近成功时间（上海）", "说明"}, count: len(status.Items), row: func(i int) []any {
			row := status.Items[i]
			return []any{row.MallID, row.MallName, row.ShopID, row.StatDate, kaiyuRegionStateLabel(row.State), row.RecordCount, kaiyuRegionOptionalTime(row.FetchedAt), kaiyuRegionOptionalTime(row.LastSuccessAt), row.ErrorMessage}
		}},
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
		if err = writeKaiyuPassengerSheet(ctx, file, sheet, styles); err != nil {
			return nil, err
		}
	}
	if err = file.SetDocProps(&excelize.DocProperties{Title: "店内区域监控", Subject: result.StartDate + " 至 " + result.EndDate + "（Asia/Shanghai）", Description: "人数按区域和日期累计，不代表去重人数；平均停留时长保留来源日指标，不作跨日平均。空响应和失败保留最近成功明细，请结合同步状态使用。", Creator: "Data Gin"}); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func kaiyuRegionOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(kaiyuLocation).Format(time.DateTime)
}
func kaiyuRegionStateLabel(state string) string {
	switch state {
	case "success":
		return "成功"
	case "empty":
		return "上游返回空区域"
	case "failed":
		return "失败"
	default:
		return "未查询"
	}
}
