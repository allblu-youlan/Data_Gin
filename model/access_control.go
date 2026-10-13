package model

import "time"

const (
	AccountTypeConsole = "CONSOLE"
	AccountTypeOpenAPI = "OPEN_API"

	AccountStatusActive   = "ACTIVE"
	AccountStatusDisabled = "DISABLED"

	RoleStatusActive   = "ACTIVE"
	RoleStatusDisabled = "DISABLED"

	MallScopeAll      = "ALL"
	MallScopeSelected = "SELECTED"

	RoleCodeSuperAdmin = "super_admin"
	RoleCodeAdmin      = "admin"
	RoleCodeOperator   = "operator"
	RoleCodeViewer     = "viewer"
)

const (
	PermissionSystemAccountRead    = "system.account.read"
	PermissionSystemAccountManage  = "system.account.manage"
	PermissionSystemRoleRead       = "system.role.read"
	PermissionSystemRoleManage     = "system.role.manage"
	PermissionSystemAuditRead      = "system.audit.read"
	PermissionBusinessOverviewRead = "business_overview.read"
	PermissionSourceRead           = "source.read"
	PermissionSourceManage         = "source.manage"
	PermissionPipelineRead         = "pipeline.read"
	PermissionPipelineManage       = "pipeline.manage"
	PermissionPipelineExecute      = "pipeline.execute"
	PermissionDeliveryRead         = "delivery.read"
	PermissionDeliveryManage       = "delivery.manage"
	PermissionDataRead             = "data.read"
	PermissionDataManage           = "data.manage"
	PermissionExcelRead            = "excel.read"
	PermissionExcelManage          = "excel.manage"
	PermissionExcelExecute         = "excel.execute"
	PermissionReportRead           = "report.read"
	PermissionReportManage         = "report.manage"
	PermissionReportExecute        = "report.execute"
	PermissionReportExport         = "report.export"
	PermissionOfficeMessageRead    = "office_message.read"
	PermissionOfficeMessageManage  = "office_message.manage"
	PermissionOfficeMessagePush    = "office_message.push"
	PermissionOfficeMentionRead    = "office_mention.read"
	PermissionOfficeMentionManage  = "office_mention.manage"
	PermissionOfficeMentionRetry   = "office_mention.retry"
)

type Permission struct {
	Code         string    `gorm:"column:code;size:64;primaryKey" json:"code"`
	Name         string    `gorm:"column:name;size:128;not null" json:"name"`
	Module       string    `gorm:"column:module;size:64;not null;index" json:"module"`
	Description  string    `gorm:"column:description;size:500;not null" json:"description"`
	RiskLevel    string    `gorm:"column:risk_level;size:16;not null;default:'LOW'" json:"riskLevel"`
	APIGrantable bool      `gorm:"column:api_grantable;not null;default:false" json:"apiGrantable"`
	Sort         int       `gorm:"column:sort;not null;default:0" json:"sort"`
	CreatedAt    time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updatedAt"`
}

func (Permission) TableName() string { return "permissions" }

type Role struct {
	BaseModel
	Code        string    `gorm:"column:code;size:64;not null;uniqueIndex" json:"code"`
	Name        string    `gorm:"column:name;size:128;not null" json:"name"`
	Description string    `gorm:"column:description;size:500;not null" json:"description"`
	Status      string    `gorm:"column:status;size:16;not null;default:'ACTIVE';index" json:"status"`
	IsSystem    bool      `gorm:"column:is_system;not null;default:false" json:"isSystem"`
	IsSuper     bool      `gorm:"column:is_super;not null;default:false" json:"isSuper"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updatedAt"`
}

func (Role) TableName() string { return "roles" }

type RolePermission struct {
	RoleID         uint      `gorm:"column:role_id;primaryKey" json:"roleId"`
	PermissionCode string    `gorm:"column:permission_code;size:64;primaryKey" json:"permissionCode"`
	CreatedAt      time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
}

func (RolePermission) TableName() string { return "role_permissions" }

type UserRole struct {
	UserID    uint      `gorm:"column:user_id;primaryKey" json:"userId"`
	RoleID    uint      `gorm:"column:role_id;primaryKey" json:"roleId"`
	CreatedBy uint      `gorm:"column:created_by;not null;default:0" json:"createdBy"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
}

func (UserRole) TableName() string { return "user_roles" }

type UserMallScope struct {
	UserID    uint      `gorm:"column:user_id;primaryKey" json:"userId"`
	MallID    uint      `gorm:"column:mall_id;primaryKey" json:"mallId"`
	CreatedBy uint      `gorm:"column:created_by;not null;default:0" json:"createdBy"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
}

func (UserMallScope) TableName() string { return "user_mall_scopes" }

type AuthAudit struct {
	BaseModel
	ActorUserID uint      `gorm:"column:actor_user_id;not null;index:idx_auth_audit_actor_created,priority:1" json:"actorUserId"`
	Action      string    `gorm:"column:action;size:64;not null;index" json:"action"`
	TargetType  string    `gorm:"column:target_type;size:32;not null;index:idx_auth_audit_target_created,priority:1" json:"targetType"`
	TargetID    uint      `gorm:"column:target_id;not null;index:idx_auth_audit_target_created,priority:2" json:"targetId"`
	BeforeJSON  JSONText  `gorm:"column:before_json;type:json" json:"before,omitempty"`
	AfterJSON   JSONText  `gorm:"column:after_json;type:json" json:"after,omitempty"`
	Reason      string    `gorm:"column:reason;size:500;not null" json:"reason"`
	RequestID   string    `gorm:"column:request_id;size:128;not null;index" json:"requestId"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime;index:idx_auth_audit_actor_created,priority:2;index:idx_auth_audit_target_created,priority:3" json:"createdAt"`
}

func (AuthAudit) TableName() string { return "auth_audits" }
