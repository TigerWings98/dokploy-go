// Input: gorm.DB, HTTP Request (Cookie/Header)
// Output: ValidateSession (User+Session), ValidateAPIKey (User), GetSessionTokenFromRequest, GetAPIKeyFromRequest
// Role: 认证核心，验证 Better Auth 兼容的 session token 和 API Key，从请求中提取认证凭证
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"gorm.io/gorm"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrUserNotFound = errors.New("user not found")
)

// Auth provides authentication and authorization functionality.
type Auth struct {
	db *db.DB
}

// New creates a new Auth instance.
func New(db *db.DB) *Auth {
	return &Auth{db: db}
}

// ValidateSession validates a session token and returns the associated user.
// 与 TS 版 validateRequest 对齐：查询 member 表补充 activeOrganizationId 和用户角色。
func (a *Auth) ValidateSession(token string) (*schema.User, *schema.Session, error) {
	var session schema.Session
	err := a.db.Where("token = ?", token).First(&session).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrUnauthorized
		}
		return nil, nil, err
	}

	var user schema.User
	err = a.db.Where("id = ?", session.UserID).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrUserNotFound
		}
		return nil, nil, err
	}

	// 与 TS 版 validateRequest 对齐：从 member 表查询用户的组织信息，
	// 补充 activeOrganizationId 和用户角色（TS 版在每次请求时都做这个查询）
	var member schema.Member
	q := a.db.Preload("Organization").Where("user_id = ?", user.ID)
	if session.ActiveOrganizationID != nil && *session.ActiveOrganizationID != "" {
		q = q.Where("organization_id = ?", *session.ActiveOrganizationID)
	}
	if err := q.Order("is_default DESC, created_at DESC").First(&member).Error; err == nil {
		// 用 member 的组织 ID 覆盖 session（与 TS 版第 494 行一致）
		if member.Organization != nil {
			orgID := member.Organization.ID
			session.ActiveOrganizationID = &orgID
		}
		// 同步用户角色（与 TS 版第 489 行一致）
		if member.Role != "" {
			user.Role = string(member.Role)
		}
	}

	return &user, &session, nil
}

// HashAPIKey 对 API Key 进行 SHA-256 哈希 + base64url 编码（无 padding），
// 与 Better Auth 的 defaultKeyHasher 完全一致。
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// ValidateAPIKey validates an API key and returns the associated user.
// Better Auth 存储的是哈希后的 key，需要先哈希再查询。
func (a *Auth) ValidateAPIKey(key string) (*schema.User, error) {
	hashed := HashAPIKey(key)
	var apiKey schema.APIKey
	err := a.db.Where("key = ?", hashed).First(&apiKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}

	if apiKey.Enabled != nil && !*apiKey.Enabled {
		return nil, ErrUnauthorized
	}

	var user schema.User
	err = a.db.Where("id = ?", apiKey.UserID).First(&user).Error
	if err != nil {
		return nil, ErrUserNotFound
	}

	return &user, nil
}

// GetSessionTokenFromRequest extracts the session token from cookies or Authorization header.
func GetSessionTokenFromRequest(r *http.Request) string {
	// Check cookie first
	cookie, err := r.Cookie("better-auth.session_token")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Check Authorization header for Bearer token
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}

	return ""
}

// GetAPIKeyFromRequest extracts the API key from the x-api-key header.
func GetAPIKeyFromRequest(r *http.Request) string {
	return r.Header.Get("x-api-key")
}
