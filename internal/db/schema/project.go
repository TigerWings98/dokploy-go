// Input: gorm, go-nanoid
// Output: Project struct (含 name/description/organizationId)，关联 Application/Compose/Database 列表
// Role: 项目容器数据表模型，作为 Application/Compose/数据库服务的顶层分组，归属于 Organization
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Project represents the project table.
type Project struct {
	ProjectID      string `gorm:"column:projectId;primaryKey;type:text" json:"projectId"`
	Name           string `gorm:"column:name;type:text;not null" json:"name"`
	Description    *string `gorm:"column:description;type:text" json:"description"`
	CreatedAt      string `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	OrganizationID string `gorm:"column:organizationId;type:text;not null" json:"organizationId"`
	Env            string `gorm:"column:env;type:text;not null;default:''" json:"env"`

	// Relations
	Organization *Organization  `gorm:"foreignKey:OrganizationID;references:ID" json:"organization,omitempty"`
	Environments []Environment  `gorm:"foreignKey:ProjectID;references:ProjectID" json:"environments"`
}

func (Project) TableName() string { return "project" }

func (p *Project) BeforeCreate(tx *gorm.DB) error {
	if p.ProjectID == "" {
		p.ProjectID, _ = gonanoid.New()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Environment represents the environment table.
type Environment struct {
	EnvironmentID string  `gorm:"column:environmentId;primaryKey;type:text" json:"environmentId"`
	Name          string  `gorm:"column:name;type:text;not null" json:"name"`
	Description   *string `gorm:"column:description;type:text" json:"description"`
	CreatedAt     string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	Env           string  `gorm:"column:env;type:text;not null;default:''" json:"env"`
	ProjectID     string  `gorm:"column:projectId;type:text;not null" json:"projectId"`
	IsDefault     bool    `gorm:"column:isDefault;not null;default:false" json:"isDefault"`

	// Relations
	Project      *Project      `gorm:"foreignKey:ProjectID;references:ProjectID" json:"project"`
	Applications []Application `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"applications"`
	Postgres     []Postgres    `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"postgres"`
	MySQL        []MySQL       `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"mysql"`
	MariaDB      []MariaDB     `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"mariadb"`
	Mongo        []Mongo       `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"mongo"`
	Redis        []Redis       `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"redis"`
	Compose      []Compose     `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"compose"`
}

func (Environment) TableName() string { return "environment" }

func (e *Environment) BeforeCreate(tx *gorm.DB) error {
	if e.EnvironmentID == "" {
		e.EnvironmentID, _ = gonanoid.New()
	}
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
