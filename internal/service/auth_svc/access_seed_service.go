package auth_svc

import (
	"context"
	"fmt"
	"sort"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type permissionSeed struct {
	Code, Name, Module, Description, RiskLevel string
	APIGrantable                               bool
	Sort                                       int
}

var accessPermissionSeeds = []permissionSeed{
	{model.PermissionSystemAccountRead, "查看账号", "系统管理", "查看控制台与开放 API 账号", "MEDIUM", false, 10},
	{model.PermissionSystemAccountManage, "管理账号", "系统管理", "创建、编辑、启停和重置账号", "HIGH", false, 20},
	{model.PermissionSystemRoleRead, "查看角色", "系统管理", "查看角色和权限矩阵", "MEDIUM", false, 30},
	{model.PermissionSystemRoleManage, "管理角色", "系统管理", "创建和维护自定义角色", "HIGH", false, 40},
	{model.PermissionSystemAuditRead, "查看权限审计", "系统管理", "查看账号及权限变更记录", "HIGH", false, 50},
	{model.PermissionBusinessOverviewRead, "查看营业概况", "营业概况", "查看销售日结、收款构成与门店对账记录", "MEDIUM", true, 90},
	{model.PermissionMallRead, "查看商场", "商场", "查看授权范围内的商场", "LOW", false, 100},
	{model.PermissionMallWrite, "管理商场", "商场", "创建和编辑授权范围内的商场", "HIGH", false, 110},
	{model.PermissionMallGeocodeConfirm, "确认商场定位", "商场", "确认商场地理编码结果", "HIGH", false, 120},
	{model.PermissionWeatherRead, "查看天气", "天气", "查看授权商场的天气数据", "LOW", true, 200},
	{model.PermissionWeatherRefresh, "刷新天气", "天气", "触发授权商场天气刷新", "MEDIUM", false, 210},
	{model.PermissionWeatherExport, "导出天气", "天气", "导出授权商场天气数据", "MEDIUM", false, 220},
	{model.PermissionWeatherFeishuPush, "推送天气", "天气", "向飞书推送天气数据", "HIGH", false, 230},
	{model.PermissionWeatherConfigManage, "管理天气配置", "天气", "维护天气导出和推送配置", "HIGH", false, 240},
	{model.PermissionWeatherRawRead, "查看天气原始数据", "天气", "查看供应商原始天气数据", "HIGH", false, 250},
	{model.PermissionBojunOrderRead, "查看伯俊订单", "订单", "通过开放接口查询伯俊订单", "MEDIUM", true, 300},
	{model.PermissionSourceRead, "查看数据源", "数据处理", "查看数据源和拉取记录", "LOW", false, 400},
	{model.PermissionSourceManage, "管理数据源", "数据处理", "维护并执行数据源", "HIGH", false, 410},
	{model.PermissionPipelineRead, "查看流水线", "数据处理", "查看流水线、方法和运行记录", "LOW", false, 420},
	{model.PermissionPipelineManage, "管理流水线", "数据处理", "维护流水线及处理规则", "HIGH", false, 430},
	{model.PermissionPipelineExecute, "执行流水线", "数据处理", "手动执行流水线和历史任务", "MEDIUM", false, 440},
	{model.PermissionDeliveryRead, "查看推送", "数据处理", "查看推送目标、任务和日志", "LOW", false, 450},
	{model.PermissionDeliveryManage, "管理推送", "数据处理", "维护推送目标、任务和策略", "HIGH", false, 460},
	{model.PermissionDataRead, "查看业务数据", "数据仓库", "查看原始、清洗和处理数据", "LOW", false, 500},
	{model.PermissionDataManage, "管理业务数据", "数据仓库", "接收、回补和重新处理数据", "HIGH", false, 510},
	{model.PermissionExcelRead, "查看 Excel 任务", "Excel", "查看 Excel 任务和方案", "LOW", false, 600},
	{model.PermissionExcelManage, "管理 Excel 方案", "Excel", "创建和删除 Excel 方案", "MEDIUM", false, 610},
	{model.PermissionExcelExecute, "执行 Excel 任务", "Excel", "上传并执行 Excel 匹配或回写", "HIGH", false, 620},
	{model.PermissionReportRead, "查看报表", "报表中心", "查看已授权报表、查询结果和导出任务", "LOW", false, 700},
	{model.PermissionReportManage, "管理报表", "报表中心", "维护数据源、过程形参、字段表头、权限和发布版本", "HIGH", false, 710},
	{model.PermissionReportExecute, "运行报表", "报表中心", "使用已发布契约执行报表存储过程", "MEDIUM", false, 720},
	{model.PermissionReportExport, "导出报表", "报表中心", "生成并下载已授权报表 Excel", "HIGH", false, 730},
	{model.PermissionOfficeMessageRead, "查看办公消息", "办公消息推送", "查看消息、推送目标和推送记录", "LOW", false, 800},
	{model.PermissionOfficeMessageManage, "管理办公消息", "办公消息推送", "维护编辑消息、Oracle 过程和飞书推送目标", "HIGH", false, 810},
	{model.PermissionOfficeMessagePush, "执行办公消息推送", "办公消息推送", "触发 Oracle 导出并通过飞书自建机器人发送", "HIGH", false, 820},
}

var roleSeeds = []model.Role{
	{Code: model.RoleCodeSuperAdmin, Name: "超级管理员", Description: "拥有当前及未来全部权限", Status: model.RoleStatusActive, IsSystem: true, IsSuper: true},
	{Code: model.RoleCodeAdmin, Name: "管理员", Description: "管理账号、角色、开放接口与全部业务", Status: model.RoleStatusActive, IsSystem: true},
	{Code: model.RoleCodeOperator, Name: "操作员", Description: "执行日常业务操作，不管理账号与角色", Status: model.RoleStatusActive, IsSystem: true},
	{Code: model.RoleCodeViewer, Name: "只读用户", Description: "只读查看业务数据", Status: model.RoleStatusActive, IsSystem: true},
}

func PermissionCatalog() []model.Permission {
	permissions := make([]model.Permission, 0, len(accessPermissionSeeds))
	for _, seed := range accessPermissionSeeds {
		permissions = append(permissions, model.Permission{Code: seed.Code, Name: seed.Name, Module: seed.Module, Description: seed.Description, RiskLevel: seed.RiskLevel, APIGrantable: seed.APIGrantable, Sort: seed.Sort})
	}
	return permissions
}

func SyncAccessControlSeeds(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return fmt.Errorf("sync access control seeds: invalid database")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		permissions := PermissionCatalog()
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "code"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "module", "description", "risk_level", "api_grantable", "sort", "updated_at"})}).Create(&permissions).Error; err != nil {
			return fmt.Errorf("sync access control seeds: permissions: %w", err)
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "code"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "description", "status", "is_system", "is_super", "updated_at"})}).Create(&roleSeeds).Error; err != nil {
			return fmt.Errorf("sync access control seeds: roles: %w", err)
		}
		var roles []model.Role
		if err := tx.Where("code IN ?", []string{model.RoleCodeSuperAdmin, model.RoleCodeAdmin, model.RoleCodeOperator, model.RoleCodeViewer}).Find(&roles).Error; err != nil {
			return fmt.Errorf("sync access control seeds: read roles: %w", err)
		}
		roleByCode := make(map[string]model.Role, len(roles))
		for _, role := range roles {
			roleByCode[role.Code] = role
		}
		if len(roleByCode) != len(roleSeeds) {
			return fmt.Errorf("sync access control seeds: incomplete system roles")
		}
		if err := syncSystemRolePermissions(tx, roleByCode, permissions); err != nil {
			return err
		}
		if err := migrateAccountKinds(tx); err != nil {
			return err
		}
		return assignSuperAdminRole(tx, roleByCode[model.RoleCodeSuperAdmin].ID)
	})
}

func syncSystemRolePermissions(tx *gorm.DB, roles map[string]model.Role, permissions []model.Permission) error {
	readOnly := map[string]struct{}{model.PermissionBusinessOverviewRead: {}, model.PermissionMallRead: {}, model.PermissionWeatherRead: {}, model.PermissionSourceRead: {}, model.PermissionPipelineRead: {}, model.PermissionDeliveryRead: {}, model.PermissionDataRead: {}, model.PermissionExcelRead: {}, model.PermissionReportRead: {}, model.PermissionOfficeMessageRead: {}}
	for _, roleCode := range []string{model.RoleCodeSuperAdmin, model.RoleCodeAdmin, model.RoleCodeOperator, model.RoleCodeViewer} {
		role := roles[roleCode]
		if err := tx.Where("role_id = ?", role.ID).Delete(&model.RolePermission{}).Error; err != nil {
			return fmt.Errorf("sync access control seeds: clear %s permissions: %w", roleCode, err)
		}
		grants := make([]model.RolePermission, 0, len(permissions))
		for _, permission := range permissions {
			include := roleCode == model.RoleCodeSuperAdmin || roleCode == model.RoleCodeAdmin
			if roleCode == model.RoleCodeOperator {
				include = permission.Module != "系统管理" && permission.Code != model.PermissionWeatherRawRead
			}
			if roleCode == model.RoleCodeViewer {
				_, include = readOnly[permission.Code]
			}
			if include {
				grants = append(grants, model.RolePermission{RoleID: role.ID, PermissionCode: permission.Code})
			}
		}
		if len(grants) > 0 {
			if err := tx.Create(&grants).Error; err != nil {
				return fmt.Errorf("sync access control seeds: grant %s: %w", roleCode, err)
			}
		}
	}
	return nil
}

func migrateAccountKinds(tx *gorm.DB) error {
	if err := tx.Model(&model.User{}).Where("id IN (?)", tx.Model(&model.OpenAPICredential{}).Select("user_id")).Updates(map[string]interface{}{"account_type": model.AccountTypeOpenAPI, "status": model.AccountStatusActive, "mall_scope_mode": model.MallScopeAll}).Error; err != nil {
		return fmt.Errorf("sync access control seeds: migrate open api accounts: %w", err)
	}
	if err := tx.Model(&model.User{}).Where("account = ? AND console_managed = ?", constant.ConsoleAdmin, true).Updates(map[string]interface{}{"account_type": model.AccountTypeConsole, "status": model.AccountStatusActive, "mall_scope_mode": model.MallScopeAll}).Error; err != nil {
		return fmt.Errorf("sync access control seeds: migrate console admin: %w", err)
	}
	return nil
}

func assignSuperAdminRole(tx *gorm.DB, roleID uint) error {
	var admin model.User
	err := tx.Where("account = ? AND console_managed = ?", constant.ConsoleAdmin, true).First(&admin).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sync access control seeds: read console admin: %w", err)
	}
	assignment := model.UserRole{UserID: admin.ID, RoleID: roleID, CreatedBy: admin.ID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&assignment).Error; err != nil {
		return fmt.Errorf("sync access control seeds: assign super admin: %w", err)
	}
	return nil
}

func PermissionCodes() []string {
	codes := make([]string, 0, len(accessPermissionSeeds))
	for _, permission := range accessPermissionSeeds {
		codes = append(codes, permission.Code)
	}
	sort.Strings(codes)
	return codes
}
