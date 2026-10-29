package model

import (
	"gorm.io/gorm"
	"time"
)

// TableEmailRule keeps the committed scan position independently of queue state.
type TableEmailRule struct {
	BaseModel
	Name                string         `gorm:"size:100;not null" json:"name"`
	DatasourceID        uint           `gorm:"not null" json:"datasourceId"`
	Owner               string         `gorm:"size:128;not null" json:"owner"`
	TableNameValue      string         `gorm:"column:table_name;size:128;not null" json:"tableName"`
	CursorColumn        string         `gorm:"size:128;not null" json:"cursorColumn"`
	RecipientsTemplate  string         `gorm:"type:text;not null" json:"recipientsTemplate"`
	SubjectTemplate     string         `gorm:"type:text;not null" json:"subjectTemplate"`
	BodyTemplate        string         `gorm:"type:text;not null" json:"bodyTemplate"`
	Enabled             bool           `gorm:"not null;index:idx_table_email_due,priority:1" json:"enabled"`
	PollIntervalSeconds int            `gorm:"not null" json:"pollIntervalSeconds"`
	LockVersion         uint64         `gorm:"not null" json:"lockVersion"`
	Generation          uint64         `gorm:"not null" json:"-"`
	CursorValue         string         `gorm:"size:40;not null" json:"cursorValue"`
	NextCheckAt         time.Time      `gorm:"type:datetime(3);not null;index:idx_table_email_due,priority:2" json:"-"`
	LastCheckedAt       *time.Time     `gorm:"type:datetime(3)" json:"lastCheckedAt"`
	LastError           string         `gorm:"type:text" json:"lastError"`
	CreatedBy           uint           `json:"-"`
	UpdatedBy           uint           `json:"-"`
	CreatedAt           time.Time      `gorm:"type:datetime(3)" json:"createdAt"`
	UpdatedAt           time.Time      `gorm:"type:datetime(3)" json:"updatedAt"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TableEmailRule) TableName() string { return "table_email_rules" }

type TableEmailDelivery struct {
	BaseModel
	RuleID         uint       `gorm:"not null;uniqueIndex:uk_table_email_record,priority:1;index" json:"ruleId"`
	Generation     uint64     `gorm:"not null;uniqueIndex:uk_table_email_record,priority:2" json:"-"`
	SourceValue    string     `gorm:"size:40;not null;uniqueIndex:uk_table_email_record,priority:3" json:"sourceValue"`
	RecipientsJSON JSONText   `gorm:"type:json;not null" json:"recipients"`
	Subject        string     `gorm:"type:text;not null" json:"subject"`
	Body           string     `gorm:"type:mediumtext;not null" json:"body"`
	SnapshotJSON   JSONText   `gorm:"type:json;not null" json:"-"`
	Status         string     `gorm:"size:16;not null;index" json:"status"`
	Attempts       int        `gorm:"not null" json:"attempts"`
	RetryVersion   uint64     `gorm:"not null" json:"-"`
	LastError      string     `gorm:"type:text" json:"lastError"`
	SentAt         *time.Time `gorm:"type:datetime(3)" json:"sentAt"`
	CreatedAt      time.Time  `gorm:"type:datetime(3)" json:"createdAt"`
	UpdatedAt      time.Time  `gorm:"type:datetime(3)" json:"updatedAt"`
}

func (TableEmailDelivery) TableName() string { return "table_email_deliveries" }
