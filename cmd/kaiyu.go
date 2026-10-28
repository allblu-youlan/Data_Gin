package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"gin-biz-web-api/bootstrap"
	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/kaiyu"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var KaiyuCmd = newKaiyuCommand()

func newKaiyuCommand() *cobra.Command {
	command := &cobra.Command{Use: "kaiyu", Short: "开域监控门店查询、客流初始化和小时补拉", Args: cobra.NoArgs, SilenceUsage: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		},
	}
	command.AddCommand(&cobra.Command{Use: "shops", Short: "查询监控平台全部店铺，供 Web 手动绑定 shopid", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := appConfig.LoadKaiyuConfig()
		if err != nil {
			return err
		}
		client, err := kaiyu.NewClient(cfg.Client)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
		defer cancel()
		shops, err := client.ListShops(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(shops)
	}})
	command.AddCommand(&cobra.Command{Use: "init", Short: "为现有门店补充开域编号字段，并初始化 YLZ_USER 客流表", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) (resultErr error) {
		bootstrap.InitializeMigration()
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
		defer cancel()
		store, err := data_svc.OpenKaiyuOracle(ctx)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
		if err := store.InitializeKaiyuPassengerStore(ctx); err != nil {
			return err
		}
		migrator := database.DB.WithContext(ctx).Migrator()
		if !migrator.HasColumn(&model.Mall{}, "KaiyuShopID") {
			if err := migrator.AddColumn(&model.Mall{}, "KaiyuShopID"); err != nil {
				return err
			}
		}
		if !migrator.HasIndex(&model.Mall{}, "KaiyuShopID") {
			if err := migrator.CreateIndex(&model.Mall{}, "KaiyuShopID"); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "开域客流存储已就绪；请在 Web 店铺信息填写开域店铺 ID。")
		return err
	}})
	var hourText string
	syncCmd := &cobra.Command{Use: "sync", Short: "同步所有已绑定门店的一个完整小时，默认上一小时（北京时间）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		hour := data_svc.KaiyuPreviousHour(time.Now())
		if hourText != "" {
			parsed, err := time.ParseInLocation("2006-01-02 15:04", hourText, time.FixedZone("Asia/Shanghai", 8*3600))
			if err != nil {
				return fmt.Errorf("hour 格式为 2026-10-28 09:00: %w", err)
			}
			hour = parsed
		}
		bootstrap.InitializeMigration()
		ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Minute)
		defer cancel()
		result, err := data_svc.RunKaiyuPassengerSync(ctx, hour)
		writeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		return errors.Join(err, writeErr)
	}}
	syncCmd.Flags().StringVar(&hourText, "hour", "", "北京时间小时，例如 2026-10-28 09:00")
	command.AddCommand(syncCmd)
	return command
}
