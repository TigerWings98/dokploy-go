package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerScheduleRoutes(g *echo.Group) {
	g.POST("", h.CreateSchedule)
	g.GET("/:scheduleId", h.GetSchedule)
	g.PUT("/:scheduleId", h.UpdateSchedule)
	g.DELETE("/:scheduleId", h.DeleteSchedule)
}

type CreateScheduleRequest struct {
	Name          string  `json:"name" validate:"required"`
	CronExpr      string  `json:"cronExpr" validate:"required"`
	Command       string  `json:"command" validate:"required"`
	Enabled       bool    `json:"enabled"`
	Type          string  `json:"type" validate:"required"`
	ApplicationID *string `json:"applicationId"`
	ComposeID     *string `json:"composeId"`
	ServerID      *string `json:"serverId"`
}

func (h *Handler) CreateSchedule(c echo.Context) error {
	var req CreateScheduleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	s := &schema.Schedule{
		Name:          req.Name,
		CronExpr:      req.CronExpr,
		Command:       req.Command,
		Enabled:       req.Enabled,
		Type:          schema.ScheduleType(req.Type),
		ApplicationID: req.ApplicationID,
		ComposeID:     req.ComposeID,
		ServerID:      req.ServerID,
	}

	if err := h.DB.Create(s).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) GetSchedule(c echo.Context) error {
	id := c.Param("scheduleId")

	var s schema.Schedule
	err := h.DB.Preload("Deployments").First(&s, "\"scheduleId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, s)
}

func (h *Handler) UpdateSchedule(c echo.Context) error {
	id := c.Param("scheduleId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var s schema.Schedule
	if err := h.DB.First(&s, "\"scheduleId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&s).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, s)
}

func (h *Handler) DeleteSchedule(c echo.Context) error {
	id := c.Param("scheduleId")

	result := h.DB.Delete(&schema.Schedule{}, "\"scheduleId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
	}

	return c.NoContent(http.StatusNoContent)
}
