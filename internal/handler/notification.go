// Input: db (Notification 表), notify (多渠道通知发送)
// Output: Notification CRUD + 测试通知的 tRPC procedure 实现
// Role: 通知配置管理 handler，支持 Slack/Telegram/Discord/Email 等渠道的配置和测试
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerNotificationRoutes(g *echo.Group) {
	g.POST("", h.CreateNotification)
	g.GET("/:notificationId", h.GetNotification)
	g.GET("", h.ListNotifications)
	g.PUT("/:notificationId", h.UpdateNotification)
	g.DELETE("/:notificationId", h.DeleteNotification)
	g.POST("/:notificationId/test", h.TestNotification)
}

func (h *Handler) CreateNotification(c echo.Context) error {
	var n schema.Notification
	if err := c.Bind(&n); err != nil {
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
	n.OrganizationID = member.OrganizationID

	if err := h.DB.Create(&n).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, n)
}

func (h *Handler) GetNotification(c echo.Context) error {
	id := c.Param("notificationId")

	var n schema.Notification
	if err := h.DB.First(&n, "\"notificationId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Notification not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, n)
}

func (h *Handler) ListNotifications(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var notifications []schema.Notification
	if err := h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&notifications).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, notifications)
}

func (h *Handler) UpdateNotification(c echo.Context) error {
	id := c.Param("notificationId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var n schema.Notification
	if err := h.DB.First(&n, "\"notificationId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Notification not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&n).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, n)
}

func (h *Handler) DeleteNotification(c echo.Context) error {
	id := c.Param("notificationId")

	result := h.DB.Delete(&schema.Notification{}, "\"notificationId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Notification not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) TestNotification(c echo.Context) error {
	id := c.Param("notificationId")

	var n schema.Notification
	if err := h.DB.First(&n, "\"notificationId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Notification not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Notifier != nil {
		if err := h.Notifier.SendTest(&n); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Test notification sent"})
}
