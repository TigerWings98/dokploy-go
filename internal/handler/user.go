package handler

import (
	"errors"
	"net/http"

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
}

func (h *Handler) GetCurrentUser(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var u schema.User
	if err := h.DB.First(&u, "id = ?", user.ID).Error; err != nil {
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
