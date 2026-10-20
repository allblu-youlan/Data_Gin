package model

import "time"

type ReportBojunCatalog struct {
	BaseModel
	DatasourceID      uint `gorm:"uniqueIndex;not null"`
	CurrentGeneration uint64
	LastSuccessAt     *time.Time
	LockVersion       uint64
	WeatherTimestamps
}

func (ReportBojunCatalog) TableName() string { return "report_bojun_catalogs" }

type ReportBojunTable struct {
	BaseModel
	DatasourceID     uint       `gorm:"uniqueIndex:uk_bojun_table,priority:1;index;not null" json:"datasourceId"`
	BojunTableID     string     `gorm:"size:32;uniqueIndex:uk_bojun_table,priority:2" json:"bojunTableId"`
	LogicalName      string     `gorm:"size:128;index" json:"logicalName"`
	Description      string     `gorm:"size:500" json:"description"`
	RealTableID      string     `gorm:"size:32" json:"realTableId"`
	ResolvedOwner    string     `gorm:"size:128" json:"resolvedOwner"`
	ResolvedTable    string     `gorm:"size:128" json:"resolvedTable"`
	DBLink           string     `gorm:"size:128" json:"dbLink"`
	SourceFilter     string     `gorm:"type:text" json:"sourceFilter"`
	SourcePK         string     `gorm:"size:32" json:"-"`
	SourceAK         string     `gorm:"size:32" json:"-"`
	SourceDK         string     `gorm:"size:32" json:"-"`
	SourceActive     bool       `gorm:"index" json:"-"`
	ValidationStatus string     `gorm:"size:32" json:"validationStatus"`
	ValidationReason string     `gorm:"size:500" json:"validationReason"`
	MetadataJSON     JSONText   `gorm:"type:json" json:"-"`
	Generation       uint64     `gorm:"index" json:"generation"`
	LastSyncedAt     *time.Time `json:"lastSyncedAt"`
	WeatherTimestamps
}

func (ReportBojunTable) TableName() string { return "report_bojun_tables" }

type ReportBojunColumn struct {
	BaseModel
	TableID       uint     `gorm:"uniqueIndex:uk_bojun_column,priority:1;index"`
	BojunColumnID string   `gorm:"size:32;uniqueIndex:uk_bojun_column,priority:2"`
	MetadataJSON  JSONText `gorm:"type:json"`
	WeatherTimestamps
}

func (ReportBojunColumn) TableName() string { return "report_bojun_columns" }

type ReportBojunTableConfig struct {
	BaseModel
	TableID     uint     `gorm:"uniqueIndex;not null"`
	ConfigJSON  JSONText `gorm:"type:json"`
	LockVersion uint64   `gorm:"not null;default:1"`
	UpdatedBy   uint
	WeatherTimestamps
}

func (ReportBojunTableConfig) TableName() string { return "report_bojun_table_configs" }

type ReportBojunSyncRun struct {
	BaseModel
	DatasourceID uint       `gorm:"index" json:"datasourceId"`
	Generation   uint64     `json:"generation"`
	Scope        string     `gorm:"size:16" json:"scope"`
	TableID      uint       `json:"tableId"`
	Status       string     `gorm:"size:16;index" json:"status"`
	StartedAt    *time.Time `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
	TableCount   int        `json:"tableCount"`
	ErrorSafe    string     `gorm:"size:500" json:"errorSafe"`
	WeatherTimestamps
}

func (ReportBojunSyncRun) TableName() string { return "report_bojun_sync_runs" }

type ReportBojunSyncStaging struct {
	BaseModel
	SyncRunID   uint     `gorm:"uniqueIndex:uk_bojun_staging,priority:1"`
	SourceKey   string   `gorm:"size:32;uniqueIndex:uk_bojun_staging,priority:2"`
	PayloadJSON JSONText `gorm:"type:json"`
}

func (ReportBojunSyncStaging) TableName() string { return "report_bojun_sync_staging" }
