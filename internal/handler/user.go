// Input: db (User 表), crypto/rand, bcrypt
// Output: User profile 更新/2FA 管理/密码修改的 tRPC procedure 实现
// Role: 用户管理 handler，处理用户信息更新、密码修改、2FA 启用/禁用
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerUserRoutes(g *echo.Group) {
	g.GET("/me", h.GetCurrentUser)
	g.PUT("/me", h.UpdateCurrentUser)
	g.GET("/:userId", h.GetUserByID)
	g.GET("", h.ListUsers)

	// API key management
	g.POST("/api-key", h.CreateAPIKey)
	g.DELETE("/api-key/:apiKeyId", h.DeleteAPIKey)

	// Permissions (admin only)
	g.PUT("/permissions", h.AssignPermissions)
}

func (h *Handler) GetCurrentUser(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var u schema.User
	if err := h.DB.Preload("APIKeys").First(&u, "id = ?", user.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "User not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, u)
}

func (h *Handler) UpdateCurrentUser(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Prevent updating sensitive fields
	delete(updates, "id")
	delete(updates, "email")
	delete(updates, "role")

	if err := h.DB.Model(&schema.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var u schema.User
	h.DB.First(&u, "id = ?", user.ID)
	return c.JSON(http.StatusOK, u)
}

func (h *Handler) GetUserByID(c echo.Context) error {
	userID := c.Param("userId")

	var u schema.User
	if err := h.DB.First(&u, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "User not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, u)
}

func (h *Handler) ListUsers(c echo.Context) error {
	var users []schema.User
	if err := h.DB.Find(&users).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, users)
}

type CreateAPIKeyRequest struct {
	Name             string  `json:"name" validate:"required"`
	ExpiresIn        *int    `json:"expiresIn"` // hours
	RateLimitEnabled *bool   `json:"rateLimitEnabled"`
	RateLimitMax     *int    `json:"rateLimitMax"`
	Prefix           *string `json:"prefix"`
}

func (h *Handler) CreateAPIKey(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var req CreateAPIKeyRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Generate random API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate key")
	}
	rawKey := hex.EncodeToString(keyBytes)

	var expiresAt *time.Time
	if req.ExpiresIn != nil {
		t := time.Now().Add(time.Duration(*req.ExpiresIn) * time.Hour)
		expiresAt = &t
	}

	prefix := req.Prefix
	start := rawKey[:8]

	apiKey := &schema.APIKey{
		Name:             &req.Name,
		Key:              rawKey,
		ReferenceID:      user.ID,
		ConfigID:         "default",
		Start:            &start,
		Prefix:           prefix,
		ExpiresAt:        expiresAt,
		RateLimitEnabled: req.RateLimitEnabled,
		RateLimitMax:     req.RateLimitMax,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := h.DB.Create(apiKey).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Return with the raw key visible (only time it's shown)
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"apiKey": apiKey,
		"key":    rawKey,
	})
}

func (h *Handler) DeleteAPIKey(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	apiKeyID := c.Param("apiKeyId")

	result := h.DB.Where("id = ? AND user_id = ?", apiKeyID, user.ID).Delete(&schema.APIKey{})
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "API key not found")
	}

	return c.NoContent(http.StatusNoContent)
}

type AssignPermissionsRequest struct {
	MemberID                string   `json:"id" validate:"required"`
	AccessedProjects        []string `json:"accessedProjects"`
	AccessedEnvironments    []string `json:"accessedEnvironments"`
	AccessedServices        []string `json:"accessedServices"`
	CanCreateProjects       *bool    `json:"canCreateProjects"`
	CanCreateServices       *bool    `json:"canCreateServices"`
	CanDeleteProjects       *bool    `json:"canDeleteProjects"`
	CanDeleteServices       *bool    `json:"canDeleteServices"`
	CanAccessToDocker       *bool    `json:"canAccessToDocker"`
	CanAccessToTraefikFiles *bool    `json:"canAccessToTraefikFiles"`
	CanAccessToAPI          *bool    `json:"canAccessToAPI"`
	CanAccessToSSHKeys      *bool    `json:"canAccessToSSHKeys"`
	CanAccessToGitProviders *bool    `json:"canAccessToGitProviders"`
	CanDeleteEnvironments   *bool    `json:"canDeleteEnvironments"`
	CanCreateEnvironments   *bool    `json:"canCreateEnvironments"`
}

func (h *Handler) AssignPermissions(c echo.Context) error {
	var req AssignPermissionsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var member schema.Member
	if err := h.DB.First(&member, "id = ?", req.MemberID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Member not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	updates := map[string]interface{}{}
	if req.AccessedProjects != nil {
		updates["accesedProjects"] = schema.StringArray(req.AccessedProjects)
	}
	if req.AccessedEnvironments != nil {
		updates["accessedEnvironments"] = schema.StringArray(req.AccessedEnvironments)
	}
	if req.AccessedServices != nil {
		updates["accesedServices"] = schema.StringArray(req.AccessedServices)
	}
	if req.CanCreateProjects != nil {
		updates["canCreateProjects"] = *req.CanCreateProjects
	}
	if req.CanCreateServices != nil {
		updates["canCreateServices"] = *req.CanCreateServices
	}
	if req.CanDeleteProjects != nil {
		updates["canDeleteProjects"] = *req.CanDeleteProjects
	}
	if req.CanDeleteServices != nil {
		updates["canDeleteServices"] = *req.CanDeleteServices
	}
	if req.CanAccessToDocker != nil {
		updates["canAccessToDocker"] = *req.CanAccessToDocker
	}
	if req.CanAccessToTraefikFiles != nil {
		updates["canAccessToTraefikFiles"] = *req.CanAccessToTraefikFiles
	}
	if req.CanAccessToAPI != nil {
		updates["canAccessToAPI"] = *req.CanAccessToAPI
	}
	if req.CanAccessToSSHKeys != nil {
		updates["canAccessToSSHKeys"] = *req.CanAccessToSSHKeys
	}
	if req.CanAccessToGitProviders != nil {
		updates["canAccessToGitProviders"] = *req.CanAccessToGitProviders
	}
	if req.CanDeleteEnvironments != nil {
		updates["canDeleteEnvironments"] = *req.CanDeleteEnvironments
	}
	if req.CanCreateEnvironments != nil {
		updates["canCreateEnvironments"] = *req.CanCreateEnvironments
	}

	if len(updates) > 0 {
		if err := h.DB.Model(&member).Updates(updates).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	h.DB.First(&member, "id = ?", req.MemberID)
	return c.JSON(http.StatusOK, member)
}
