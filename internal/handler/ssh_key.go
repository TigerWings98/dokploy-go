// Input: db (SSHKey 表), crypto/ed25519
// Output: SSH Key 生成/CRUD 的 tRPC procedure 实现
// Role: SSH 密钥管理 handler，支持 Ed25519 密钥对生成和持久化
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/ssh"
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
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Marshal private key to PEM
	privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})

	// Marshal public key to OpenSSH format
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	pubKeyStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))

	return c.JSON(http.StatusOK, map[string]string{
		"publicKey":  pubKeyStr,
		"privateKey": string(privPEM),
	})
}
