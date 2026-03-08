// Input: db (SSOProvider/User/Account/Session/Organization/Member 表)
// Output: Service (FindProviderByDomain, CreateOrLinkUser, CreateSession)
// Role: SSO 核心服务，负责 email domain → provider 查找、用户创建/链接、session 生成
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package sso

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
)

// Service 提供 SSO 认证相关的业务逻辑
type Service struct {
	db *db.DB
}

// NewService 创建 SSO 服务实例
func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

// FindProviderByEmail 通过用户 email 的 domain 查找对应的 SSO 提供商
func (s *Service) FindProviderByEmail(email string) (*schema.SSOProvider, error) {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid email format")
	}
	domain := strings.ToLower(parts[1])

	var providers []schema.SSOProvider
	s.db.Find(&providers)

	for _, p := range providers {
		domains := strings.Split(p.Domain, ",")
		for _, d := range domains {
			if strings.TrimSpace(strings.ToLower(d)) == domain {
				return &p, nil
			}
		}
	}

	return nil, fmt.Errorf("no SSO provider configured for domain: %s", domain)
}

// FindProviderByID 根据 provider_id 查找 SSO 提供商
func (s *Service) FindProviderByID(providerID string) (*schema.SSOProvider, error) {
	var provider schema.SSOProvider
	if err := s.db.Where("provider_id = ?", providerID).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("SSO provider not found: %s", providerID)
	}
	return &provider, nil
}

// ParseOIDCConfig 解析提供商的 OIDC 配置
func ParseOIDCConfig(provider *schema.SSOProvider) (*OIDCConfig, error) {
	if provider.OIDCConfig == nil || *provider.OIDCConfig == "" {
		return nil, fmt.Errorf("OIDC config not set for provider %s", provider.ProviderID)
	}
	var cfg OIDCConfig
	if err := json.Unmarshal([]byte(*provider.OIDCConfig), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse OIDC config: %w", err)
	}
	return &cfg, nil
}

// ParseSAMLConfig 解析提供商的 SAML 配置
func ParseSAMLConfig(provider *schema.SSOProvider) (*SAMLConfig, error) {
	if provider.SAMLConfig == nil || *provider.SAMLConfig == "" {
		return nil, fmt.Errorf("SAML config not set for provider %s", provider.ProviderID)
	}
	var cfg SAMLConfig
	if err := json.Unmarshal([]byte(*provider.SAMLConfig), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse SAML config: %w", err)
	}
	return &cfg, nil
}

// CreateOrLinkUser 创建新用户或链接到已有用户（account linking）
// 返回 user 和是否是新创建的
func (s *Service) CreateOrLinkUser(info *UserInfo, providerID string, orgID *string) (*schema.User, bool, error) {
	if info.Email == "" {
		return nil, false, fmt.Errorf("SSO provider did not return an email")
	}

	// 查找是否已有同 email 用户
	var existingUser schema.User
	err := s.db.Where("email = ?", info.Email).First(&existingUser).Error
	if err == nil {
		// 已有用户 → account linking
		// 检查是否已有该 provider 的 account
		var account schema.Account
		accErr := s.db.Where("user_id = ? AND provider_id = ?", existingUser.ID, providerID).First(&account).Error
		if accErr != nil {
			// 创建新 account 关联
			now := time.Now().UTC()
			newAccount := &schema.Account{
				AccountID:  info.ID,
				ProviderID: providerID,
				UserID:     existingUser.ID,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			s.db.Create(newAccount)
		}

		// 确保用户已加入提供商所属的组织
		if orgID != nil {
			s.ensureMembership(existingUser.ID, *orgID)
		}

		return &existingUser, false, nil
	}

	// 新用户：创建 user + account
	tx := s.db.Begin()
	now := time.Now().UTC()

	user := &schema.User{
		FirstName:     info.FirstName,
		LastName:      info.LastName,
		Email:         info.Email,
		EmailVerified: info.EmailVerified,
		IsRegistered:  true,
		Role:          "user", // SSO 用户默认普通角色
		CreatedAt:     &now,
		UpdatedAt:     now,
	}
	if info.Image != "" {
		user.Image = &info.Image
	}

	if err := tx.Create(user).Error; err != nil {
		tx.Rollback()
		return nil, false, fmt.Errorf("failed to create user: %w", err)
	}

	account := &schema.Account{
		AccountID:  info.ID,
		ProviderID: providerID,
		UserID:     user.ID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := tx.Create(account).Error; err != nil {
		tx.Rollback()
		return nil, false, fmt.Errorf("failed to create account: %w", err)
	}

	// 将用户加入提供商关联的组织
	if orgID != nil && *orgID != "" {
		member := &schema.Member{
			UserID:         user.ID,
			OrganizationID: *orgID,
			Role:           schema.MemberRoleMember,
			CreatedAt:      now,
			IsDefault:      true,
		}
		if err := tx.Create(member).Error; err != nil {
			tx.Rollback()
			return nil, false, fmt.Errorf("failed to create member: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, false, fmt.Errorf("transaction failed: %w", err)
	}

	return user, true, nil
}

// CreateSession 为用户创建新 session
func (s *Service) CreateSession(userID, ipAddress, userAgent string, orgID *string) (*schema.Session, string, error) {
	now := time.Now().UTC()
	token := generateToken()

	session := &schema.Session{
		Token:     token,
		UserID:    userID,
		ExpiresAt: now.Add(3 * 24 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
		IPAddress: &ipAddress,
		UserAgent: &userAgent,
	}

	// 设置 activeOrganizationId
	if orgID != nil {
		session.ActiveOrganizationID = orgID
	} else {
		// 查找用户的默认组织
		var member schema.Member
		if err := s.db.Where("user_id = ?", userID).
			Order("is_default DESC, created_at DESC").
			First(&member).Error; err == nil {
			session.ActiveOrganizationID = &member.OrganizationID
		}
	}

	if err := s.db.Create(session).Error; err != nil {
		return nil, "", fmt.Errorf("failed to create session: %w", err)
	}

	return session, token, nil
}

// ensureMembership 确保用户是组织成员
func (s *Service) ensureMembership(userID, orgID string) {
	var count int64
	s.db.Model(&schema.Member{}).Where("user_id = ? AND organization_id = ?", userID, orgID).Count(&count)
	if count == 0 {
		now := time.Now().UTC()
		member := &schema.Member{
			UserID:         userID,
			OrganizationID: orgID,
			Role:           schema.MemberRoleMember,
			CreatedAt:      now,
		}
		s.db.Create(member)
	}
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
