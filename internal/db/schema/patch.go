package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// PatchType represents the type of a patch.
type PatchType string

const (
	PatchTypeCreate PatchType = "create"
	PatchTypeUpdate PatchType = "update"
	PatchTypeDelete PatchType = "delete"
)

// Patch represents the patch table.
type Patch struct {
	PatchID       string    `gorm:"column:patchId;primaryKey;type:text" json:"patchId"`
	Type          PatchType `gorm:"column:type;type:text;not null;default:'update'" json:"type"`
	FilePath      string    `gorm:"column:filePath;type:text;not null" json:"filePath"`
	Enabled       bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	Content       string    `gorm:"column:content;type:text;not null" json:"content"`
	CreatedAt     string    `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	UpdatedAt     *string   `gorm:"column:updatedAt;type:text" json:"updatedAt"`
	ApplicationID *string   `gorm:"column:applicationId;type:text" json:"applicationId"`
	ComposeID     *string   `gorm:"column:composeId;type:text" json:"composeId"`

	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Compose     *Compose     `gorm:"foreignKey:ComposeID" json:"compose,omitempty"`
}

func (Patch) TableName() string { return "patch" }

func (p *Patch) BeforeCreate(tx *gorm.DB) error {
	if p.PatchID == "" {
		p.PatchID, _ = gonanoid.New()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
