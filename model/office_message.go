package model

import "time"

const (
	OfficeMessageSourceEdited          = "EDITED"
	OfficeMessageSourceOracleProcedure = "ORACLE_PROCEDURE"
	OfficeMessageSourceOracleQuery     = "ORACLE_QUERY"

	OfficePushChannelFeishu = "FEISHU"
	OfficePushChannelWebDAV = "WEBDAV"

	OfficePushRunStatusQueued    = "QUEUED"
	OfficePushRunStatusRunning   = "RUNNING"
	OfficePushRunStatusSucceeded = "SUCCEEDED"
	OfficePushRunStatusFailed    = "FAILED"
	OfficePushRunStatusUnknown   = "UNKNOWN"

	OfficePushTriggerManual   = "MANUAL"
	OfficePushTriggerSchedule = "SCHEDULE"

	OfficeScheduleParameterLiteral       = "LITERAL"
	OfficeScheduleParameterScheduledDate = "SCHEDULED_DATE"
	OfficeScheduleTimeZone               = "Asia/Shanghai"
)

// OfficeMessage describes either operator-authored text or an Oracle result
// table that is exported when a push starts.
type OfficeMessage struct {
	BaseModel
	Name                string    `gorm:"column:name;size:128;not null;index" json:"name"`
	SourceType          string    `gorm:"column:source_type;size:16;not null;index" json:"sourceType"`
	Content             string    `gorm:"column:content;type:text" json:"content"`
	ProcedureOwner      string    `gorm:"column:procedure_owner;size:128" json:"procedureOwner"`
	PackageName         string    `gorm:"column:package_name;size:128" json:"packageName"`
	ProcedureName       string    `gorm:"column:procedure_name;size:128" json:"procedureName"`
	ProcedureOverload   string    `gorm:"column:procedure_overload;size:32" json:"procedureOverload"`
	ResultTableOwner    string    `gorm:"column:result_table_owner;size:128" json:"resultTableOwner"`
	ResultTableName     string    `gorm:"column:result_table_name;size:128" json:"resultTableName"`
	SelectSQL           string    `gorm:"column:select_sql;type:text" json:"selectSql"`
	FileNameTemplate    string    `gorm:"column:file_name_template;size:255" json:"fileNameTemplate"`
	ParameterSchemaJSON JSONText  `gorm:"column:parameter_schema_json;type:json" json:"parameters"`
	ColumnMappingJSON   JSONText  `gorm:"column:column_mapping_json;type:json" json:"columnMapping"`
	Enabled             bool      `gorm:"column:enabled;not null;default:true;index" json:"enabled"`
	LockVersion         uint64    `gorm:"column:lock_version;not null;default:1" json:"lockVersion"`
	CreatedBy           uint      `gorm:"column:created_by;not null;index" json:"createdBy"`
	UpdatedBy           uint      `gorm:"column:updated_by;not null" json:"updatedBy"`
	CreatedAt           time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
	UpdatedAt           time.Time `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updatedAt"`
}

func (OfficeMessage) TableName() string { return "office_messages" }

// OfficePushTarget binds one message to a delivery destination.
type OfficePushTarget struct {
	BaseModel
	Name                     string    `gorm:"column:name;size:128;not null;index" json:"name"`
	MessageID                uint      `gorm:"column:message_id;not null;index" json:"messageId"`
	Channel                  string    `gorm:"column:channel;size:16;not null;default:'FEISHU';index" json:"channel"`
	BotAppID                 string    `gorm:"column:bot_app_id;size:128;not null;default:''" json:"botAppId"`
	ReceiveIDType            string    `gorm:"column:receive_id_type;size:32;not null;default:''" json:"receiveIdType"`
	ReceiveID                string    `gorm:"column:receive_id;size:255;not null;default:''" json:"receiveId"`
	WebDAVURL                string    `gorm:"column:webdav_url;size:2048;not null;default:''" json:"webdavUrl"`
	WebDAVUsername           string    `gorm:"column:webdav_username;size:255;not null;default:''" json:"webdavUsername"`
	WebDAVPath               string    `gorm:"column:webdav_path;size:1024;not null;default:''" json:"webdavPath"`
	WebDAVPasswordCiphertext string    `gorm:"column:webdav_password_ciphertext;size:2048;not null;default:''" json:"-"`
	CredentialKeyVersion     string    `gorm:"column:credential_key_version;size:64;not null;default:''" json:"-"`
	HasWebDAVPassword        bool      `gorm:"-" json:"hasWebdavPassword"`
	Enabled                  bool      `gorm:"column:enabled;not null;default:true;index" json:"enabled"`
	LockVersion              uint64    `gorm:"column:lock_version;not null;default:1" json:"lockVersion"`
	CreatedBy                uint      `gorm:"column:created_by;not null;index" json:"createdBy"`
	UpdatedBy                uint      `gorm:"column:updated_by;not null" json:"updatedBy"`
	CreatedAt                time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
	UpdatedAt                time.Time `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updatedAt"`
}

func (OfficePushTarget) TableName() string { return "office_push_targets" }

// OfficePushSchedule stores one independently managed cron trigger for a push
// target. The due index is used by the database-backed planner across replicas.
type OfficePushSchedule struct {
	BaseModel
	Name            string     `gorm:"column:name;size:128;not null;index" json:"name"`
	TargetID        uint       `gorm:"column:target_id;not null;index" json:"targetId"`
	CronExpr        string     `gorm:"column:cron_expr;size:128;not null" json:"cronExpr"`
	TimeZone        string     `gorm:"column:time_zone;size:64;not null;default:'Asia/Shanghai'" json:"timeZone"`
	ParametersJSON  JSONText   `gorm:"column:parameters_json;type:json" json:"parameters"`
	Enabled         bool       `gorm:"column:enabled;not null;default:true;index:idx_office_push_schedules_due,priority:1" json:"enabled"`
	NextRunAt       time.Time  `gorm:"column:next_run_at;type:datetime(3);not null;index:idx_office_push_schedules_due,priority:2" json:"nextRunAt"`
	LastScheduledAt *time.Time `gorm:"column:last_scheduled_at;type:datetime(3)" json:"lastScheduledAt,omitempty"`
	LastErrorSafe   string     `gorm:"column:last_error_safe;size:500" json:"lastError,omitempty"`
	LockVersion     uint64     `gorm:"column:lock_version;not null;default:1" json:"lockVersion"`
	CreatedBy       uint       `gorm:"column:created_by;not null;index" json:"createdBy"`
	UpdatedBy       uint       `gorm:"column:updated_by;not null" json:"updatedBy"`
	CreatedAt       time.Time  `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updatedAt"`
}

func (OfficePushSchedule) TableName() string { return "office_push_schedules" }

// OfficePushRun is the durable, idempotent execution record consumed by the
// asynchronous worker. RunUUID is also sent to Feishu as its idempotency key.
type OfficePushRun struct {
	BaseModel
	RunUUID          string     `gorm:"column:run_uuid;size:64;not null;uniqueIndex" json:"runUuid"`
	TargetID         uint       `gorm:"column:target_id;not null;index" json:"targetId"`
	MessageID        uint       `gorm:"column:message_id;not null;index" json:"messageId"`
	Status           string     `gorm:"column:status;size:16;not null;index" json:"status"`
	AttemptCount     int        `gorm:"column:attempt_count;not null;default:0" json:"attemptCount"`
	RowCount         int64      `gorm:"column:row_count;not null;default:0" json:"rowCount"`
	FeishuMessageID  string     `gorm:"column:feishu_message_id;size:128" json:"feishuMessageId,omitempty"`
	ErrorCode        string     `gorm:"column:error_code;size:64" json:"errorCode,omitempty"`
	ErrorMessageSafe string     `gorm:"column:error_message_safe;size:500" json:"errorMessage,omitempty"`
	TriggerType      string     `gorm:"column:trigger_type;size:16;not null;default:'MANUAL';index" json:"triggerType"`
	ScheduleID       *uint      `gorm:"column:schedule_id;index" json:"scheduleId,omitempty"`
	ScheduledFor     *time.Time `gorm:"column:scheduled_for;type:datetime(3);index" json:"scheduledFor,omitempty"`
	RequestedBy      uint       `gorm:"column:requested_by;not null;index" json:"requestedBy"`
	ParametersJSON   JSONText   `gorm:"column:parameters_json;type:json" json:"-"`
	SnapshotJSON     JSONText   `gorm:"column:snapshot_json;type:json" json:"-"`
	LeaseToken       string     `gorm:"column:lease_token;size:64;not null;default:'';index" json:"-"`
	LeaseExpiresAt   *time.Time `gorm:"column:lease_expires_at;type:datetime(3);index" json:"-"`
	HeartbeatAt      *time.Time `gorm:"column:heartbeat_at;type:datetime(3)" json:"-"`
	StartedAt        *time.Time `gorm:"column:started_at;type:datetime(3)" json:"startedAt,omitempty"`
	FinishedAt       *time.Time `gorm:"column:finished_at;type:datetime(3)" json:"finishedAt,omitempty"`
	CreatedAt        time.Time  `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime;index" json:"createdAt"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updatedAt"`
}

func (OfficePushRun) TableName() string { return "office_push_runs" }

// OfficeProcedureExportLock serializes procedure/result-table exports across
// worker instances so a shared result table cannot be overwritten mid-read.
type OfficeProcedureExportLock struct {
	LockKey        string    `gorm:"column:lock_key;size:255;primaryKey" json:"-"`
	LeaseToken     string    `gorm:"column:lease_token;size:64;not null;index" json:"-"`
	LeaseExpiresAt time.Time `gorm:"column:lease_expires_at;type:datetime(3);not null;index" json:"-"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"-"`
}

func (OfficeProcedureExportLock) TableName() string { return "office_procedure_export_locks" }
