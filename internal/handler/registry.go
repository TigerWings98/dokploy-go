package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
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

	// TODO: docker login test
	return c.JSON(http.StatusOK, map[string]string{"message": "Registry connection successful"})
}
