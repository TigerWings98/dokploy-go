// Input: gorm, go-nanoid
// Output: SSOProvider struct (含 issuer/oidcConfig/samlConfig/organizationId 等字段)
// Role: SSO 提供商配置数据表模型，支持 OIDC 和 SAML 协议，关联 Organization
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// SSOProvider represents the sso_provider table.
type SSOProvider struct {
	ID             string    `gorm:"column:id;primaryKey;type:text" json:"id"`
	Issuer         string    `gorm:"column:issuer;type:text;not null" json:"issuer"`
	OIDCConfig     *string   `gorm:"column:oidc_config;type:text" json:"oidcConfig"`
	SAMLConfig     *string   `gorm:"column:saml_config;type:text" json:"samlConfig"`
	ProviderID     string    `gorm:"column:provider_id;type:text;not null;uniqueIndex:sso_provider_provider_id_unique" json:"providerId"`
	UserID         *string   `gorm:"column:user_id;type:text" json:"userId"`
	OrganizationID *string   `gorm:"column:organization_id;type:text" json:"organizationId"`
	Domain         string    `gorm:"column:domain;type:text;not null" json:"domain"`
	CreatedAt      time.Time `gorm:"column:created_at;not null;autoCreateTime" json:"createdAt"`

	// Relations
	Organization *Organization `gorm:"foreignKey:OrganizationID;references:ID" json:"organization,omitempty"`
	User         *User         `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
}

func (SSOProvider) TableName() string { return "sso_provider" }

func (s *SSOProvider) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID, _ = gonanoid.New()
	}
	return nil
}
