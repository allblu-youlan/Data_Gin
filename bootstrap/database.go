package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	// GORM 的 MySQL 数据库驱动导入
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"
	"gin-biz-web-api/pkg/credential"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
)

const schemaMigrationVersion = "2026-09-08-office-webdav-v22"
const previousSchemaMigrationVersion = "2026-09-04-report-category-access-v21"
const accessPermissionMigrationVersion = "2026-09-01-access-permission-v20"
const officeMessageScheduleMigrationVersion = "2026-09-01-office-message-schedule-v19"
const officeMessageFileMigrationVersion = "2026-09-01-office-message-file-v18"
const officeMessageBotMigrationVersion = "2026-09-01-office-message-bot-v17"
const officeMessageCompatMigrationVersion = "2026-09-01-office-message-compat-v16"
const officeMessageHACompatMigrationVersion = "2026-09-01-office-message-ha-v15"
const officeMessagePreviousMigrationVersion = "2026-09-01-office-message-v14"
const officeMessageMigrationBaselineVersion = "2026-08-26-bojun-oracle-v13"
const legacyOfficeMessageSourceOracle = "ORACLE"
const bojunOraclePreviousMigrationVersion = "2026-08-26-bojun-oracle-v12"
const bojunOracleMigrationBaselineVersion = "2026-08-25-report-center-v11"
const schemaMigrationLockName = "data_gin_schema_migration_v1"

type schemaMigrationRecord struct {
	Version   string    `gorm:"column:version;primaryKey;size:64"`
	AppliedAt time.Time `gorm:"column:applied_at;not null"`
}

func (schemaMigrationRecord) TableName() string { return "app_schema_versions" }

// setupDB 初始化数据库和 ORM
func setupDB() {
	setupDBConnection()
	if autoMigrateOnStartup() {
		if err := ApplySchemaMigrations(); err != nil {
			console.Exit("database schema migration failed: %v", err)
		}
	}
}

func autoMigrateOnStartup() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("AUTO_MIGRATE_ON_STARTUP")))
	return value == "1" || value == "true" || value == "yes"
}

// ApplySchemaMigrations runs schema and access-control migrations explicitly.
// The HTTP server skips this expensive path by default so it can become ready quickly.
func ApplySchemaMigrations() (resultErr error) {
	db := database.DB
	if db == nil {
		return fmt.Errorf("database connection is unavailable")
	}
	if database.SQLDB == nil {
		return fmt.Errorf("database SQL connection is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := database.SQLDB.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	conn, err := database.SQLDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire schema migration connection: %w", err)
	}
	defer conn.Close()
	locked, err := acquireSchemaMigrationLock(ctx, conn)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("another schema migration is running")
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer releaseCancel()
		if releaseErr := releaseSchemaMigrationLock(releaseCtx, conn); releaseErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
		}
	}()
	if err := db.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		return fmt.Errorf("prepare schema migration marker: %w", err)
	}
	currentApplied, err := schemaMigrationApplied(db, schemaMigrationVersion)
	if err != nil {
		return err
	}
	if currentApplied {
		console.Info("database schema is current: %s", schemaMigrationVersion)
		return nil
	}
	baselineVersion, baselineApplied, err := appliedSchemaMigrationBaseline(
		db,
		schemaIncrementalMigrationBaselines(),
	)
	if err != nil {
		return err
	}
	if baselineApplied {
		console.Info("applying incremental database schema migration: %s -> %s", baselineVersion, schemaMigrationVersion)
	}
	if err := runPendingSchemaMigration(
		baselineApplied,
		func() error {
			return db.AutoMigrate(incrementalSchemaMigrationModels()...)
		},
		autoMigrateTables,
	); err != nil {
		return err
	}
	if err := finalizeOfficeMessageMigration(db); err != nil {
		return err
	}
	marker := schemaMigrationRecord{Version: schemaMigrationVersion, AppliedAt: time.Now().UTC()}
	if err := db.Create(&marker).Error; err != nil {
		return fmt.Errorf("write schema migration marker: %w", err)
	}
	return nil
}

func finalizeOfficeMessageMigration(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("finalize office message migration: database is unavailable")
	}
	if err := db.Model(&model.OfficeMessage{}).
		Where("source_type = ?", legacyOfficeMessageSourceOracle).
		Update("source_type", model.OfficeMessageSourceOracleProcedure).Error; err != nil {
		return fmt.Errorf("migrate legacy office message source type: %w", err)
	}
	if appID := strings.TrimSpace(global.Credentials.FeishuAppID()); appID != "" && global.Credentials.Configured(credential.EnvFeishuAppSecret) {
		if err := db.Model(&model.OfficePushTarget{}).
			Where("bot_app_id = ?", "").
			Update("bot_app_id", appID).Error; err != nil {
			return fmt.Errorf("backfill office push target bot app id: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := auth_svc.SyncAccessControlSeeds(ctx, db); err != nil {
		return fmt.Errorf("sync access control seeds: %w", err)
	}
	console.Success("账号权限种子同步完成")
	return nil
}

func schemaMigrationApplied(db *gorm.DB, version string) (bool, error) {
	var count int64
	if err := db.Model(&schemaMigrationRecord{}).Where("version = ?", version).Count(&count).Error; err != nil {
		return false, fmt.Errorf("read schema migration marker %s: %w", version, err)
	}
	return count == 1, nil
}

func bojunOracleIncrementalMigrationBaselines() []string {
	return []string{
		bojunOraclePreviousMigrationVersion,
		bojunOracleMigrationBaselineVersion,
	}
}

func bojunOracleMigrationModels() []interface{} {
	return []interface{}{
		&model.BojunRetailOrder{},
		&model.BojunOracleSyncState{},
	}
}

func schemaIncrementalMigrationBaselines() []string {
	return []string{
		previousSchemaMigrationVersion,
		accessPermissionMigrationVersion,
		officeMessageScheduleMigrationVersion,
		officeMessageFileMigrationVersion,
		officeMessageBotMigrationVersion,
		officeMessageCompatMigrationVersion,
		officeMessageHACompatMigrationVersion,
		officeMessagePreviousMigrationVersion,
		officeMessageMigrationBaselineVersion,
	}
}

func incrementalSchemaMigrationModels() []interface{} {
	models := append([]interface{}{}, officeMessageMigrationModels()...)
	return append(models,
		&model.ReportCategoryAccess{},
		&model.ReportCategoryGrant{},
	)
}

func officeMessageMigrationModels() []interface{} {
	return []interface{}{
		&model.OfficeMessage{},
		&model.OfficePushTarget{},
		&model.OfficePushSchedule{},
		&model.OfficePushRun{},
		&model.OfficeProcedureExportLock{},
	}
}

func appliedSchemaMigrationBaseline(db *gorm.DB, candidates []string) (string, bool, error) {
	var appliedVersions []string
	if err := db.Model(&schemaMigrationRecord{}).
		Where("version IN ?", candidates).
		Pluck("version", &appliedVersions).Error; err != nil {
		return "", false, fmt.Errorf("read compatible schema migration markers: %w", err)
	}
	version, applied := preferredSchemaMigrationBaseline(candidates, appliedVersions)
	return version, applied, nil
}

func preferredSchemaMigrationBaseline(candidates, appliedVersions []string) (string, bool) {
	applied := make(map[string]struct{}, len(appliedVersions))
	for _, version := range appliedVersions {
		applied[version] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, exists := applied[candidate]; exists {
			return candidate, true
		}
	}
	return "", false
}

func runPendingSchemaMigration(previousApplied bool, incremental, full func() error) error {
	if previousApplied {
		return incremental()
	}
	return full()
}

func acquireSchemaMigrationLock(ctx context.Context, conn *sql.Conn) (bool, error) {
	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 60)", schemaMigrationLockName).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire schema migration lock: %w", err)
	}
	return locked.Valid && locked.Int64 == 1, nil
}

func releaseSchemaMigrationLock(ctx context.Context, conn *sql.Conn) error {
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", schemaMigrationLockName).Scan(&released); err != nil {
		return fmt.Errorf("release schema migration lock: %w", err)
	}
	if !released.Valid || released.Int64 != 1 {
		return fmt.Errorf("schema migration lock was not released")
	}
	return nil
}

func setupDBConnection() {
	setupDBConnectionWithIOTimeout(0)
}

func setupDBConnectionWithIOTimeout(ioTimeout time.Duration) {

	console.Info("init database ...")

	switch config.GetString("cfg.database.driver") {
	case "mysql":
		if err := setupDBMySQL(ioTimeout); err != nil {
			console.Exit("database initialization failed: %v", err)
		}
	default:
		console.Exit("database driver not supported")
	}

}

func setupDBMySQL(ioTimeout time.Duration) error {

	configs := config.Get("cfg.database.mysql")

	dbConfigs := make(map[string]*database.DBClientConfig)

	groups, ok := configs.(map[string]interface{})
	if !ok || len(groups) == 0 {
		return fmt.Errorf("mysql configuration is empty")
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return fmt.Errorf("load mysql timezone: %w", err)
	}
	for group := range groups {
		cfgPrefix := "cfg.database.mysql." + group + "."
		username := config.GetString(cfgPrefix + "username")
		password := config.GetString(cfgPrefix + "password")
		host := config.GetString(cfgPrefix + "host")
		port := config.GetString(cfgPrefix + "port")
		db := config.GetString(cfgPrefix + "database")
		charset := config.GetString(cfgPrefix + "charset")
		connectTimeout := time.Duration(config.GetInt(cfgPrefix+"connect_timeout_seconds")) * time.Second
		readTimeout := time.Duration(config.GetInt(cfgPrefix+"read_timeout_seconds")) * time.Second
		writeTimeout := time.Duration(config.GetInt(cfgPrefix+"write_timeout_seconds")) * time.Second
		if ioTimeout > 0 {
			readTimeout = ioTimeout
			writeTimeout = ioTimeout
		}

		driverConfig := mysqlDriver.NewConfig()
		driverConfig.User = username
		driverConfig.Passwd = password
		driverConfig.Net = "tcp"
		driverConfig.Addr = net.JoinHostPort(host, port)
		driverConfig.DBName = db
		driverConfig.Params = map[string]string{"charset": charset}
		driverConfig.ParseTime = true
		driverConfig.Loc = location
		driverConfig.Timeout = connectTimeout
		driverConfig.ReadTimeout = readTimeout
		driverConfig.WriteTimeout = writeTimeout
		driverConfig.RejectReadOnly = config.GetBool(cfgPrefix + "reject_read_only")

		var dbConfig gorm.Dialector
		dbConfig = mysql.New(mysql.Config{
			DSN: driverConfig.FormatDSN(),
		})

		var cfg database.DBClientConfig
		cfg.DBConfig = dbConfig
		cfg.LG = logger.NewGormLogger()
		cfg.MaxOpenConns = config.GetInt(cfgPrefix + "max_open_connections")
		cfg.MaxIdleConns = config.GetInt(cfgPrefix + "max_idle_connections")
		cfg.ConnMaxLifetime = time.Duration(config.GetInt(cfgPrefix+"max_life_seconds")) * time.Second
		cfg.ConnMaxIdleTime = time.Duration(config.GetInt(cfgPrefix+"max_idle_seconds")) * time.Second
		cfg.ConnectTimeout = connectTimeout
		cfg.ReadTimeout = readTimeout
		cfg.WriteTimeout = writeTimeout

		dbConfigs[group] = &cfg
	}

	if err := database.ConnectMySQL(dbConfigs); err != nil {
		return err
	}
	return nil
}

// autoMigrateTables 自动迁移数据存储相关表
func autoMigrateTables() error {
	console.Info("auto migrating data storage tables...")

	// 获取默认数据库连接
	db := database.DB

	// 检查数据库连接是否成功
	if db == nil {
		return fmt.Errorf("database connection is unavailable")
	}

	if err := prepareMethodPipelineIndexes(db); err != nil {
		return err
	}
	if err := prepareMallWeatherVersionIndexes(db); err != nil {
		logger.Error("修复商场天气版本索引失败", zap.Error(err))
		return fmt.Errorf("repair mall weather version indexes: %w", err)
	}
	if err := prepareReportVersionDatasourceSnapshot(db); err != nil {
		logger.Error("回填报表版本数据源快照失败", zap.Error(err))
		return fmt.Errorf("prepare report version datasource snapshot: %w", err)
	}
	if err := prepareReportGrantVersionSnapshot(db); err != nil {
		logger.Error("回填报表授权版本快照失败", zap.Error(err))
		return fmt.Errorf("prepare report grant version snapshot: %w", err)
	}

	// 迁移数据存储相关表
	models := []interface{}{
		&model.User{}, // 用户表
		&model.Permission{},
		&model.Role{},
		&model.RolePermission{},
		&model.UserRole{},
		&model.UserMallScope{},
		&model.AuthAudit{},
		&model.DataSource{},            // 数据源配置表
		&model.RawData{},               // 原始数据表
		&model.ProcessedData{},         // 处理结果表
		&model.DataStatistics{},        // 数据统计表
		&model.QIMAI_ORDER_DATA{},      //企迈订单表
		&model.TokenData{},             //验证信息表
		&model.YOUZAN_ORDER_DATA{},     //有赞订单表
		&model.YOUZAN_RETURN_DATA{},    //有赞退款订单表
		&model.BojunRetailOrder{},      //伯俊零售单表
		&model.BojunOracleSyncState{},  //伯俊 Oracle 增量水位表
		&model.SourceDefinition{},      //通用数据源配置表
		&model.RawRecord{},             //通用原始记录表
		&model.CleanTableDefinition{},  //清洗表配置表
		&model.CleanRecord{},           //通用清洗记录表
		&model.TransformRule{},         //清洗规则表
		&model.DestinationDefinition{}, //通用推送目标表
		&model.DeliveryTask{},          //通用推送任务表
		&model.PipelineRun{},           //运行记录表
		&model.DeliveryLog{},           //推送日志表
		&model.RuntimeConfig{},         //运行配置表
		&model.PipelineDefinition{},    //方法拼接流水线表
		&model.PipelineStage{},         //流水线大块阶段表
		&model.MethodStep{},            //方法步骤表
		&model.MethodParam{},           //方法入参表
		&model.MethodOutput{},          //方法出参表
		&model.StageGeneratedConfig{},  //阶段生成配置表
		&model.StepRun{},               //方法步骤运行明细表
		&model.ExcelMatchJob{},         //Excel匹配导出任务表
		&model.ExcelMatchJobLog{},      //Excel任务处理日志表
		&model.ExcelMatchScheme{},      //Excel匹配参数方案表
		// 有赞分销订单表
		&model.YouzanDistributionOrder{},
	}
	models = append(models, mallWeatherMigrationModels()...)
	models = append(models, reportCenterMigrationModels()...)
	models = append(models, officeMessageMigrationModels()...)
	err := db.AutoMigrate(models...)

	if err != nil {
		logger.Error("数据表自动迁移失败", zap.Error(err))
		return fmt.Errorf("auto migrate tables: %w", err)
	}
	if err := prepareReportResultTableBindings(db); err != nil {
		return err
	}
	if err := verifyMallWeatherVersionIndexes(db); err != nil {
		logger.Error("校验商场天气版本索引失败", zap.Error(err))
		return fmt.Errorf("verify mall weather version indexes: %w", err)
	}

	console.Success("数据表自动迁移完成")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	synchronized, err := auth_svc.SyncExistingConsoleAdminPermissions(ctx, db)
	if err != nil {
		logger.Error("同步 admin 天气权限失败", zap.Error(err))
		return fmt.Errorf("sync admin permissions: %w", err)
	}
	if synchronized {
		console.Success("admin 天气权限同步完成")
	} else {
		console.Info("admin 尚未完成控制台身份初始化，首次控制台登录时将同步天气权限")
	}
	return nil
}

func reportCenterMigrationModels() []interface{} {
	return []interface{}{
		&model.ReportDatasource{},
		&model.ReportInputQueryDefinition{},
		&model.ReportResultTableBinding{},
		&model.ReportDefinition{},
		&model.ReportVersion{},
		&model.ReportParameter{},
		&model.ReportColumn{},
		&model.ReportGrant{},
		&model.ReportCategoryAccess{},
		&model.ReportCategoryGrant{},
		&model.ReportRun{},
		&model.ReportExport{},
		&model.ReportResultReadLease{},
		&model.ReportAudit{},
	}
}

func mallWeatherMigrationModels() []interface{} {
	return []interface{}{
		&model.Mall{},
		&model.MallGeocodeRun{},
		&model.MallGeocodeCandidate{},
		&model.MallCoordinateAudit{},
		&model.ProviderRawSnapshot{},
		&model.MallWeatherFetchRun{},
		&model.MallWeatherFetchAttempt{},
		&model.MallWeatherRealtime{},
		&model.MallWeatherMinutely{},
		&model.MallWeatherHourly{},
		&model.MallWeatherDaily{},
		&model.MallWeatherAlert{},
		&model.MallWeatherAlertRelation{},
		&model.MallWeatherLifeIndex{},
		&model.MallWeatherLatest{},
		&model.MallWeatherExportProfile{},
		&model.MallWeatherExportJob{},
		&model.MallWeatherFeishuRun{},
		&model.MallWeatherSheetRow{},
		&model.AsyncJobOutbox{},
		&model.MallWeatherUserPermission{},
		&model.APIIdempotencyRecord{},
		&model.OpenAPICredential{},
		&model.DataAuthorizationAudit{},
	}
}

func prepareMethodPipelineIndexes(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.MethodStep{}) {
		return nil
	}

	const indexName = "idx_pipeline_step_code"
	if !db.Migrator().HasIndex(&model.MethodStep{}, indexName) {
		return nil
	}

	if err := db.Migrator().DropIndex(&model.MethodStep{}, indexName); err != nil {
		logger.Error("删除旧方法步骤唯一索引失败", zap.String("index", indexName), zap.Error(err))
		return fmt.Errorf("drop legacy method step index %s: %w", indexName, err)
	}
	return nil
}
