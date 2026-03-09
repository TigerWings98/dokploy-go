// Input: db (User/Session/Account/Organization/Member 表), bcrypt, crypto
// Output: Better Auth 兼容的注册/登录/注销端点 (/api/auth/*)
// Role: 认证端点 handler，实现 Better Auth 协议兼容的用户注册、密码登录和会话管理
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// registerAuthRoutes registers better-auth compatible auth endpoints.
// These are mounted at /api/auth/* and are PUBLIC (no auth middleware).
func (h *Handler) registerAuthRoutes(g *echo.Group) {
	g.POST("/sign-up/email", h.SignUpEmail)
	g.POST("/sign-in/email", h.SignInEmail)
	g.GET("/get-session", h.GetSession)
	g.POST("/sign-out", h.SignOut)
	g.POST("/change-password", h.ChangePassword)
	// better-auth also calls GET /ok for health
	g.GET("/ok", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{"ok": true})
	})

	// SSO endpoints (OIDC + SAML)
	h.registerSSOAuthRoutes(g)
}

// SignUpEmail handles POST /api/auth/sign-up/email
// better-auth format: { name, email, password, lastName? }
// Response: { user: {...}, session: {...}, token: "..." }
func (h *Handler) SignUpEmail(c echo.Context) error {
	var req struct {
		Name     string `json:"name"`
		LastName string `json:"lastName"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid request body")
	}

	if req.Email == "" || req.Password == "" {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Email and password are required")
	}

	if len(req.Password) < 8 {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Password must be at least 8 characters")
	}

	// Check if admin already exists (non-cloud: only first user can register without invite)
	var ownerCount int64
	h.DB.Model(&schema.Member{}).Where("role = ?", "owner").Count(&ownerCount)
	if ownerCount > 0 {
		// Check for x-dokploy-token header (invitation flow)
		token := c.Request().Header.Get("x-dokploy-token")
		if token == "" {
			return h.betterAuthError(c, http.StatusBadRequest, "BAD_REQUEST", "Admin is already created")
		}
		// Validate the invite token
		var inv schema.Invitation
		if err := h.DB.Where("id = ? AND status = ?", token, "pending").First(&inv).Error; err != nil {
			return h.betterAuthError(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid invitation token")
		}
	}

	// Check if email is already taken
	var existingUser schema.User
	if err := h.DB.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
		return h.betterAuthError(c, http.StatusBadRequest, "BAD_REQUEST", "Email already in use")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to hash password")
	}

	tx := h.DB.Begin()

	// Create user
	now := time.Now().UTC()
	user := &schema.User{
		FirstName:     req.Name,
		LastName:      req.LastName,
		Email:         req.Email,
		EmailVerified: true, // auto-verify for self-hosted
		IsRegistered:  true,
		Role:          "admin", // first user is admin
		CreatedAt:     &now,
		UpdatedAt:     now,
	}

	// If admin already exists, new user is regular user
	if ownerCount > 0 {
		user.Role = "user"
	}

	if err := tx.Create(user).Error; err != nil {
		tx.Rollback()
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create user")
	}

	// Create account (better-auth stores password in account table)
	hashedStr := string(hashedPassword)
	account := &schema.Account{
		AccountID:  user.ID,
		ProviderID: "credential",
		UserID:     user.ID,
		Password:   &hashedStr,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := tx.Create(account).Error; err != nil {
		tx.Rollback()
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create account")
	}

	// Create default organization and member
	org := &schema.Organization{
		Name:      "My Organization",
		OwnerID:   user.ID,
		CreatedAt: now,
	}
	if err := tx.Create(org).Error; err != nil {
		tx.Rollback()
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create organization")
	}

	member := &schema.Member{
		UserID:         user.ID,
		OrganizationID: org.ID,
		Role:           schema.MemberRoleOwner,
		CreatedAt:      now,
		IsDefault:      true,
	}
	if err := tx.Create(member).Error; err != nil {
		tx.Rollback()
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create member")
	}

	// Create session
	sessionToken := generateSessionToken()
	session := &schema.Session{
		Token:     sessionToken,
		UserID:    user.ID,
		ExpiresAt: now.Add(3 * 24 * time.Hour), // 3 days
		CreatedAt: now,
		UpdatedAt: now,
		IPAddress: strPtr(c.RealIP()),
		UserAgent: strPtr(c.Request().UserAgent()),
	}
	if err := tx.Create(session).Error; err != nil {
		tx.Rollback()
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create session")
	}

	if err := tx.Commit().Error; err != nil {
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Transaction failed")
	}

	// Set session cookie (same as better-auth)
	h.setSessionCookie(c, sessionToken, session.ExpiresAt)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"user":    h.buildUserResponse(user),
		"session": h.buildSessionResponse(session),
		"token":   sessionToken,
	})
}

// SignInEmail handles POST /api/auth/sign-in/email
// better-auth format: { email, password }
func (h *Handler) SignInEmail(c echo.Context) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid request body")
	}

	if req.Email == "" || req.Password == "" {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Email and password are required")
	}

	// Find user
	var user schema.User
	if err := h.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return h.betterAuthError(c, http.StatusUnauthorized, "INVALID_EMAIL_OR_PASSWORD", "Invalid email or password")
		}
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Database error")
	}

	// Find account and verify password
	var account schema.Account
	if err := h.DB.Where("user_id = ? AND provider_id = ?", user.ID, "credential").First(&account).Error; err != nil {
		return h.betterAuthError(c, http.StatusUnauthorized, "INVALID_EMAIL_OR_PASSWORD", "Invalid email or password")
	}

	if account.Password == nil {
		return h.betterAuthError(c, http.StatusUnauthorized, "INVALID_EMAIL_OR_PASSWORD", "Invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*account.Password), []byte(req.Password)); err != nil {
		return h.betterAuthError(c, http.StatusUnauthorized, "INVALID_EMAIL_OR_PASSWORD", "Invalid email or password")
	}

	// Check if 2FA is enabled
	if account.Is2FAEnabled {
		// Return twoFactorRedirect flag - frontend handles 2FA flow
		return c.JSON(http.StatusOK, map[string]interface{}{
			"twoFactorRedirect": true,
		})
	}

	// Create new session
	now := time.Now().UTC()
	sessionToken := generateSessionToken()
	session := &schema.Session{
		Token:     sessionToken,
		UserID:    user.ID,
		ExpiresAt: now.Add(3 * 24 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
		IPAddress: strPtr(c.RealIP()),
		UserAgent: strPtr(c.Request().UserAgent()),
	}
	if err := h.DB.Create(session).Error; err != nil {
		return h.betterAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create session")
	}

	h.setSessionCookie(c, sessionToken, session.ExpiresAt)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"user":    h.buildUserResponse(&user),
		"session": h.buildSessionResponse(session),
		"token":   sessionToken,
	})
}

// GetSession handles GET /api/auth/get-session
// Returns the current session and user, or null if not authenticated.
func (h *Handler) GetSession(c echo.Context) error {
	token := getSessionToken(c)
	if token == "" {
		return c.JSON(http.StatusOK, nil)
	}

	var session schema.Session
	if err := h.DB.Where("token = ? AND expires_at > ?", token, time.Now().UTC()).First(&session).Error; err != nil {
		return c.JSON(http.StatusOK, nil)
	}

	var user schema.User
	if err := h.DB.Where("id = ?", session.UserID).First(&user).Error; err != nil {
		return c.JSON(http.StatusOK, nil)
	}

	// Find active organization
	var member schema.Member
	h.DB.Where("user_id = ?", user.ID).
		Order("is_default DESC, created_at DESC").
		First(&member)

	// Refresh session if older than 1 day
	if time.Since(session.UpdatedAt) > 24*time.Hour {
		session.ExpiresAt = time.Now().UTC().Add(3 * 24 * time.Hour)
		session.UpdatedAt = time.Now().UTC()
		h.DB.Save(&session)
		h.setSessionCookie(c, token, session.ExpiresAt)
	}

	resp := map[string]interface{}{
		"user":    h.buildUserResponse(&user),
		"session": h.buildSessionResponseWithOrg(&session, member.OrganizationID),
	}

	return c.JSON(http.StatusOK, resp)
}

// SignOut handles POST /api/auth/sign-out
func (h *Handler) SignOut(c echo.Context) error {
	token := getSessionToken(c)
	if token != "" {
		h.DB.Where("token = ?", token).Delete(&schema.Session{})
	}

	// Clear cookie
	cookie := &http.Cookie{
		Name:     "better-auth.session_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	c.SetCookie(cookie)

	return c.JSON(http.StatusOK, map[string]interface{}{"success": true})
}

// ChangePassword handles POST /api/auth/change-password
func (h *Handler) ChangePassword(c echo.Context) error {
	token := getSessionToken(c)
	if token == "" {
		return h.betterAuthError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated")
	}

	var session schema.Session
	if err := h.DB.Where("token = ? AND expires_at > ?", token, time.Now().UTC()).First(&session).Error; err != nil {
		return h.betterAuthError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid session")
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid request")
	}

	var account schema.Account
	if err := h.DB.Where("user_id = ? AND provider_id = ?", session.UserID, "credential").First(&account).Error; err != nil {
		return h.betterAuthError(c, http.StatusBadRequest, "BAD_REQUEST", "Account not found")
	}

	if account.Password != nil {
		if err := bcrypt.CompareHashAndPassword([]byte(*account.Password), []byte(req.CurrentPassword)); err != nil {
			return h.betterAuthError(c, http.StatusBadRequest, "BAD_REQUEST", "Current password is incorrect")
		}
	}

	hashed, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 10)
	hashedStr := string(hashed)
	h.DB.Model(&account).Update("password", hashedStr)

	return c.JSON(http.StatusOK, map[string]interface{}{"success": true})
}

// --- helpers ---

func (h *Handler) betterAuthError(c echo.Context, status int, code, message string) error {
	return c.JSON(status, map[string]interface{}{
		"code":       code,
		"message":    message,
		"statusCode": status,
	})
}

func (h *Handler) setSessionCookie(c echo.Context, token string, expires time.Time) {
	cookie := &http.Cookie{
		Name:     "better-auth.session_token",
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	c.SetCookie(cookie)
}

func (h *Handler) buildUserResponse(user *schema.User) map[string]interface{} {
	resp := map[string]interface{}{
		"id":                       user.ID,
		"name":                     user.FirstName,
		"email":                    user.Email,
		"emailVerified":            user.EmailVerified,
		"image":                    user.Image,
		"createdAt":                user.CreatedAt,
		"updatedAt":                user.UpdatedAt,
		"twoFactorEnabled":        user.TwoFactorEnabled,
		"role":                     user.Role,
		"banned":                   user.Banned,
		"banReason":                user.BanReason,
		"banExpires":               user.BanExpires,
		"lastName":                 user.LastName,
		"enableEnterpriseFeatures": user.EnableEnterpriseFeatures,
		"isValidEnterpriseLicense": user.IsValidEnterpriseLicense,
	}
	return resp
}

func (h *Handler) buildSessionResponse(session *schema.Session) map[string]interface{} {
	return map[string]interface{}{
		"id":        session.ID,
		"userId":    session.UserID,
		"token":     session.Token,
		"expiresAt": session.ExpiresAt,
		"createdAt": session.CreatedAt,
		"updatedAt": session.UpdatedAt,
		"ipAddress": session.IPAddress,
		"userAgent": session.UserAgent,
	}
}

func (h *Handler) buildSessionResponseWithOrg(session *schema.Session, orgID string) map[string]interface{} {
	resp := h.buildSessionResponse(session)
	resp["activeOrganizationId"] = orgID
	return resp
}

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getSessionToken(c echo.Context) string {
	// Cookie first
	cookie, err := c.Cookie("better-auth.session_token")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}
	// Authorization header
	auth := c.Request().Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}

func strPtr(s string) *string {
	return &s
}
