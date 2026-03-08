// Input: db (Destination 表), backup (S3 连接测试)
// Output: Destination (S3 备份目标) CRUD + 连接测试的 tRPC procedure 实现
// Role: S3 备份目标管理 handler，配置 rclone 所需的 S3 凭证和存储桶信息
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

func (h *Handler) registerDestinationRoutes(g *echo.Group) {
	g.POST("", h.CreateDestination)
	g.GET("/:destinationId", h.GetDestination)
	g.GET("", h.ListDestinations)
	g.PUT("/:destinationId", h.UpdateDestination)
	g.DELETE("/:destinationId", h.DeleteDestination)
	g.POST("/:destinationId/test", h.TestDestination)
}

type CreateDestinationRequest struct {
	Name           string `json:"name" validate:"required"`
	AccessKey      string `json:"accessKey" validate:"required"`
	SecretAccessKey string `json:"secretAccessKey" validate:"required"`
	Bucket         string `json:"bucket" validate:"required"`
	Region         string `json:"region" validate:"required"`
	Endpoint       string `json:"endpoint" validate:"required"`
}

func (h *Handler) CreateDestination(c echo.Context) error {
	var req CreateDestinationRequest
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

	dest := &schema.Destination{
		Name:           req.Name,
		AccessKey:      req.AccessKey,
		SecretAccessKey: req.SecretAccessKey,
		Bucket:         req.Bucket,
		Region:         req.Region,
		Endpoint:       req.Endpoint,
		OrganizationID: member.OrganizationID,
	}

	if err := h.DB.Create(dest).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, dest)
}

func (h *Handler) GetDestination(c echo.Context) error {
	id := c.Param("destinationId")

	var dest schema.Destination
	if err := h.DB.First(&dest, "\"destinationId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Destination not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, dest)
}

func (h *Handler) ListDestinations(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var dests []schema.Destination
	if err := h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&dests).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, dests)
}

func (h *Handler) UpdateDestination(c echo.Context) error {
	id := c.Param("destinationId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var dest schema.Destination
	if err := h.DB.First(&dest, "\"destinationId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Destination not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&dest).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, dest)
}

func (h *Handler) DeleteDestination(c echo.Context) error {
	id := c.Param("destinationId")

	result := h.DB.Delete(&schema.Destination{}, "\"destinationId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Destination not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) TestDestination(c echo.Context) error {
	id := c.Param("destinationId")

	var dest schema.Destination
	if err := h.DB.First(&dest, "\"destinationId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Destination not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Test S3 connection using rclone lsd command
	cmd := fmt.Sprintf(
		"RCLONE_CONFIG_S3_TYPE=s3 RCLONE_CONFIG_S3_ACCESS_KEY_ID=%s RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=%s RCLONE_CONFIG_S3_REGION=%s RCLONE_CONFIG_S3_ENDPOINT=%s rclone lsd s3:%s --max-depth 1",
		dest.AccessKey, dest.SecretAccessKey, dest.Region, dest.Endpoint, dest.Bucket,
	)
	_, execErr := process.ExecAsync(cmd, process.WithTimeout(30*time.Second))
	if execErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Connection failed: %v", execErr))
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Connection successful"})
}
