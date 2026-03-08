// Input: db (Certificate 表), 文件系统 (证书文件读写)
// Output: SSL 证书 CRUD 的 tRPC procedure 实现
// Role: SSL 证书管理 handler，处理自定义证书的上传/存储/删除
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerCertificateRoutes(g *echo.Group) {
	g.POST("", h.CreateCertificate)
	g.GET("/:certificateId", h.GetCertificate)
	g.GET("", h.ListCertificates)
	g.DELETE("/:certificateId", h.DeleteCertificate)
}

type CreateCertificateRequest struct {
	Name            string  `json:"name" validate:"required"`
	CertificateData string  `json:"certificateData" validate:"required"`
	PrivateKey      string  `json:"privateKey" validate:"required"`
	AutoRenew       *bool   `json:"autoRenew"`
	ServerID        *string `json:"serverId"`
}

func (h *Handler) CreateCertificate(c echo.Context) error {
	var req CreateCertificateRequest
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

	cert := &schema.Certificate{
		Name:            req.Name,
		CertificateData: req.CertificateData,
		PrivateKey:      req.PrivateKey,
		AutoRenew:       req.AutoRenew,
		ServerID:        req.ServerID,
		OrganizationID:  member.OrganizationID,
	}

	if err := h.DB.Create(cert).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.writeCertFiles(cert)

	return c.JSON(http.StatusCreated, cert)
}

func (h *Handler) GetCertificate(c echo.Context) error {
	id := c.Param("certificateId")

	var cert schema.Certificate
	if err := h.DB.First(&cert, "\"certificateId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Certificate not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, cert)
}

func (h *Handler) ListCertificates(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var certs []schema.Certificate
	if err := h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&certs).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, certs)
}

func (h *Handler) DeleteCertificate(c echo.Context) error {
	id := c.Param("certificateId")

	var cert schema.Certificate
	if err := h.DB.First(&cert, "\"certificateId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Certificate not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Delete(&cert).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.removeCertFiles(cert.CertificateID)

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) writeCertFiles(cert *schema.Certificate) {
	if h.CertsPath == "" {
		return
	}

	certDir := filepath.Join(h.CertsPath, cert.CertificateID)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		log.Printf("Failed to create cert directory %s: %v", certDir, err)
		return
	}

	certFile := filepath.Join(certDir, "cert.pem")
	keyFile := filepath.Join(certDir, "key.pem")

	if err := os.WriteFile(certFile, []byte(cert.CertificateData), 0644); err != nil {
		log.Printf("Failed to write cert file: %v", err)
		return
	}
	if err := os.WriteFile(keyFile, []byte(cert.PrivateKey), 0600); err != nil {
		log.Printf("Failed to write key file: %v", err)
		return
	}

	// Write Traefik TLS config pointing to these files
	if h.Traefik != nil {
		traefikConfig := fmt.Sprintf(`tls:
  certificates:
    - certFile: %s
      keyFile: %s
`, certFile, keyFile)
		configFile := filepath.Join(h.CertsPath, cert.CertificateID+".yaml")
		if err := os.WriteFile(configFile, []byte(traefikConfig), 0644); err != nil {
			log.Printf("Failed to write traefik cert config: %v", err)
		}
	}
}

func (h *Handler) removeCertFiles(certID string) {
	if h.CertsPath == "" {
		return
	}

	certDir := filepath.Join(h.CertsPath, certID)
	if err := os.RemoveAll(certDir); err != nil {
		log.Printf("Failed to remove cert directory %s: %v", certDir, err)
	}

	configFile := filepath.Join(h.CertsPath, certID+".yaml")
	os.Remove(configFile)
}
