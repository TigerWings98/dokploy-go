// Input: db (Registry 表), docker (Registry 登录测试)
// Output: Registry CRUD + 登录测试的 tRPC procedure 实现
// Role: 容器镜像仓库管理 handler，支持自托管和云端 Registry 配置
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerRegistryRoutes(g *echo.Group) {
	g.POST("", h.CreateRegistry)
	g.GET("/:registryId", h.GetRegistry)
	g.GET("", h.ListRegistries)
	g.PUT("/:registryId", h.UpdateRegistry)
	g.DELETE("/:registryId", h.DeleteRegistry)
	g.POST("/:registryId/test", h.TestRegistry)
}

type CreateRegistryRequest struct {
	RegistryName    string  `json:"registryName" validate:"required"`
	Username        string  `json:"username" validate:"required"`
	Password        string  `json:"password" validate:"required"`
	RegistryURL     string  `json:"registryUrl" validate:"required"`
	RegistryType    string  `json:"registryType"`
	ImagePrefix     *string `json:"imagePrefix"`
	SelfHostedImage *string `json:"selfHostedImage"`
}

func (h *Handler) CreateRegistry(c echo.Context) error {
	var req CreateRegistryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	regType := schema.RegistryType("cloud")
	if req.RegistryType != "" {
		regType = schema.RegistryType(req.RegistryType)
	}

	reg := &schema.Registry{
		RegistryName:    req.RegistryName,
		Username:        req.Username,
		Password:        req.Password,
		RegistryURL:     req.RegistryURL,
		RegistryType:    regType,
		ImagePrefix:     req.ImagePrefix,
		SelfHostedImage: req.SelfHostedImage,
		OrganizationID:  member.OrganizationID,
	}

	if err := h.DB.Create(reg).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, reg)
}

func (h *Handler) GetRegistry(c echo.Context) error {
	id := c.Param("registryId")

	var reg schema.Registry
	if err := h.DB.First(&reg, "\"registryId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Registry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, reg)
}

func (h *Handler) ListRegistries(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var registries []schema.Registry
	if err := h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&registries).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, registries)
}

func (h *Handler) UpdateRegistry(c echo.Context) error {
	id := c.Param("registryId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var reg schema.Registry
	if err := h.DB.First(&reg, "\"registryId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Registry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&reg).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, reg)
}

func (h *Handler) DeleteRegistry(c echo.Context) error {
	id := c.Param("registryId")

	result := h.DB.Delete(&schema.Registry{}, "\"registryId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Registry not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) TestRegistry(c echo.Context) error {
	id := c.Param("registryId")

	var reg schema.Registry
	if err := h.DB.First(&reg, "\"registryId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Registry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Test docker login
	cmd := fmt.Sprintf("echo %q | docker login %s -u %s --password-stdin",
		reg.Password, reg.RegistryURL, reg.Username)
	_, execErr := process.ExecAsync(cmd, process.WithTimeout(30*time.Second))
	if execErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Registry login failed: %v", execErr))
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Registry connection successful"})
}
