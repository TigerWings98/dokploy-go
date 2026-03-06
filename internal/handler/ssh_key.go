package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerSSHKeyRoutes(g *echo.Group) {
	g.POST("", h.CreateSSHKey)
	g.GET("/:sshKeyId", h.GetSSHKey)
	g.GET("", h.ListSSHKeys)
	g.PUT("/:sshKeyId", h.UpdateSSHKey)
	g.DELETE("/:sshKeyId", h.DeleteSSHKey)
	g.POST("/generate", h.GenerateSSHKey)
}

type CreateSSHKeyRequest struct {
	Name        string  `json:"name" validate:"required"`
	Description *string `json:"description"`
	PublicKey   string  `json:"publicKey" validate:"required"`
	PrivateKey  string  `json:"privateKey" validate:"required"`
}

func (h *Handler) CreateSSHKey(c echo.Context) error {
	var req CreateSSHKeyRequest
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

	key := &schema.SSHKey{
		Name:           req.Name,
		Description:    req.Description,
		PublicKey:      req.PublicKey,
		PrivateKey:     req.PrivateKey,
		OrganizationID: member.OrganizationID,
	}

	if err := h.DB.Create(key).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, key)
}

func (h *Handler) GetSSHKey(c echo.Context) error {
	id := c.Param("sshKeyId")

	var key schema.SSHKey
	if err := h.DB.First(&key, "\"sshKeyId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "SSH key not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, key)
}

func (h *Handler) ListSSHKeys(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var keys []schema.SSHKey
	if err := h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&keys).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, keys)
}

func (h *Handler) UpdateSSHKey(c echo.Context) error {
	id := c.Param("sshKeyId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var key schema.SSHKey
	if err := h.DB.First(&key, "\"sshKeyId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "SSH key not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&key).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, key)
}

func (h *Handler) DeleteSSHKey(c echo.Context) error {
	id := c.Param("sshKeyId")

	result := h.DB.Delete(&schema.SSHKey{}, "\"sshKeyId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "SSH key not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) GenerateSSHKey(c echo.Context) error {
	// TODO: Generate ed25519 key pair using crypto/ed25519
	return c.JSON(http.StatusOK, map[string]string{
		"message": "SSH key generation not yet implemented",
	})
}
