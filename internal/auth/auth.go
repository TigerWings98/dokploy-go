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

	return &user, &session, nil
}

// hashAPIKey 对 API Key 进行 SHA-256 哈希 + base64url 编码（无 padding），
// 与 Better Auth 的 defaultKeyHasher 完全一致。
func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// ValidateAPIKey validates an API key and returns the associated user.
// Better Auth 存储的是哈希后的 key，需要先哈希再查询。
func (a *Auth) ValidateAPIKey(key string) (*schema.User, error) {
	hashed := hashAPIKey(key)
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
