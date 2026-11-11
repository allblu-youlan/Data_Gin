package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/bootstrap"
	"gin-biz-web-api/internal/service/data_svc"
	"github.com/spf13/cobra"
)

func newKaiyuRegionCommand() *cobra.Command {
	command := &cobra.Command{Use: "region", Short: "店内区域日数据初始化、同步和核验", Args: cobra.NoArgs}
	command.AddCommand(&cobra.Command{Use: "init", Short: "显式初始化 YLZ_USER 区域明细和同步状态表，不修改门店绑定", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) (resultErr error) {
		bootstrap.InitializeMigration()
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
		defer cancel()
		store, err := data_svc.OpenKaiyuOracle(ctx)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
		if err = store.InitializeKaiyuRegionStore(ctx); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "开域区域日数据存储已就绪；沿用 Web 已填写的开域店铺 ID。")
		return err
	}})
	command.AddCommand(newKaiyuRegionRunCommand("sync"), newKaiyuRegionRunCommand("missing"), newKaiyuRegionRunCommand("backfill"))
	return command
}
func newKaiyuRegionRunCommand(name string) *cobra.Command {
	descriptions := map[string]string{"sync": "刷新当天及前3天已绑定门店的区域日数据", "missing": "核验店铺日期同步状态，默认当天及前3天", "backfill": "补拉未成功的店铺日期，可使用 force 刷新已有数据"}
	var startDate, endDate string
	var mallID uint
	var force bool
	command := &cobra.Command{Use: name, Short: descriptions[name], Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		start, end := data_svc.KaiyuRegionRollingRange(time.Now())
		if startDate != "" || endDate != "" {
			start, end = startDate, endDate
		}
		if name == "missing" {
			bootstrap.InitializeMigration()
		} else {
			bootstrap.InitializeProviderDatabaseCLI()
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Minute)
		defer cancel()
		missing, result, err := data_svc.RunKaiyuRegionJob(ctx, data_svc.KaiyuRegionQuery{StartDate: start, EndDate: end, MallID: mallID}, name != "missing", force || name == "sync")
		output := struct {
			Missing  *data_svc.KaiyuRegionMissingResult  `json:"missing"`
			Backfill *data_svc.KaiyuRegionBackfillResult `json:"backfill,omitempty"`
		}{Missing: missing}
		if name != "missing" {
			output.Backfill = result
		}
		return errors.Join(err, json.NewEncoder(cmd.OutOrStdout()).Encode(output))
	}}
	command.Flags().StringVar(&startDate, "start-date", "", "开始日期（含，YYYY-MM-DD），与 end-date 同用，最多31天")
	command.Flags().StringVar(&endDate, "end-date", "", "结束日期（含，YYYY-MM-DD，上海时间）")
	command.Flags().UintVar(&mallID, "mall-id", 0, "仅处理指定门店，默认所有已绑定门店")
	if name == "backfill" {
		command.Flags().BoolVar(&force, "force", false, "同时刷新已经成功的店铺日期")
	}
	return command
}
