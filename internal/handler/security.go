// Input: db (Security 表), traefik
// Output: Security (Basic Auth) CRUD 的 tRPC procedure 实现
// Role: 域名安全策略管理 handler，配置 Basic Auth 认证并同步 Traefik 中间件
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (h *Handler) registerSecurityRoutes(g *echo.Group) {
	g.POST("", h.CreateSecurity)
	g.GET("/:securityId", h.GetSecurity)
	g.PUT("/:securityId", h.UpdateSecurity)
	g.DELETE("/:securityId", h.DeleteSecurity)
}

type CreateSecurityRequest struct {
	Username      string  `json:"username" validate:"required"`
	Password      string  `json:"password" validate:"required"`
	ApplicationID *string `json:"applicationId"`
	ComposeID     *string `json:"composeId"`
}

func (h *Handler) CreateSecurity(c echo.Context) error {
	var req CreateSecurityRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// 与 TS 版一致：使用 bcrypt 哈希密码后存储（Traefik basicAuth 需要 htpasswd 格式）
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to hash password")
	}

	s := &schema.Security{
		Username:      req.Username,
		Password:      string(hashedPassword),
		ApplicationID: req.ApplicationID,
		ComposeID:     req.ComposeID,
	}

	if err := h.DB.Create(s).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.syncBasicAuthTraefik(s.ApplicationID, s.ComposeID)

	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) GetSecurity(c echo.Context) error {
	id := c.Param("securityId")

	var s schema.Security
	if err := h.DB.First(&s, "\"securityId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Security not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, s)
}

func (h *Handler) UpdateSecurity(c echo.Context) error {
	id := c.Param("securityId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var s schema.Security
	if err := h.DB.First(&s, "\"securityId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Security not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// 如果更新包含密码，先 bcrypt 哈希
	if pw, ok := updates["password"].(string); ok && pw != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(pw), 10)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to hash password")
		}
		updates["password"] = string(hashed)
	}

	if err := h.DB.Model(&s).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.syncBasicAuthTraefik(s.ApplicationID, s.ComposeID)

	return c.JSON(http.StatusOK, s)
}

func (h *Handler) DeleteSecurity(c echo.Context) error {
	id := c.Param("securityId")

	var s schema.Security
	if err := h.DB.First(&s, "\"securityId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Security not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	appID := s.ApplicationID
	composeID := s.ComposeID

	if err := h.DB.Delete(&s).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.syncBasicAuthTraefik(appID, composeID)

	return c.NoContent(http.StatusNoContent)
}

// syncBasicAuthTraefik loads all security entries for an app/compose and updates Traefik.
func (h *Handler) syncBasicAuthTraefik(applicationID, composeID *string) {
	if h.Traefik == nil {
		return
	}

	var appName string
	var securities []schema.Security

	if applicationID != nil {
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", *applicationID).Error; err == nil {
			appName = app.AppName
		}
		h.DB.Where("\"applicationId\" = ?", *applicationID).Find(&securities)
	} else if composeID != nil {
		var compose schema.Compose
		if err := h.DB.First(&compose, "\"composeId\" = ?", *composeID).Error; err == nil {
			appName = compose.AppName
		}
		h.DB.Where("\"composeId\" = ?", *composeID).Find(&securities)
	}

	if appName == "" {
		return
	}

	credentials := make([]string, len(securities))
	for i, s := range securities {
		credentials[i] = fmt.Sprintf("%s:%s", s.Username, s.Password)
	}

	if err := h.Traefik.UpdateBasicAuth(appName, credentials); err != nil {
		log.Printf("Failed to update traefik basic auth for %s: %v", appName, err)
	}
}
