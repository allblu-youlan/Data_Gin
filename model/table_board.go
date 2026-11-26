package model

// TableBoard holds the layout owned by one console account. Database query
// results are resolved on demand and never written into the layout.
type TableBoard struct {
	BaseModel
	OwnerID  uint     `gorm:"not null;index:idx_table_boards_owner" json:"-"`
	Name     string   `gorm:"size:100;not null" json:"name"`
	Revision uint64   `gorm:"not null" json:"revision"`
	Layout   JSONText `gorm:"type:json;not null" json:"layout"`
}
