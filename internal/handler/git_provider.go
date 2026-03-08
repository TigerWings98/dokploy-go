// Input: db (GitProvider 表), provider (GitHub/GitLab/Gitea/Bitbucket API)
// Output: GitProvider CRUD + 仓库/分支列表查询的 tRPC procedure 实现
// Role: Git 提供商管理 handler，配置 API 凭证并查询远程仓库信息
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

func (h *Handler) registerGitProviderRoutes(g *echo.Group) {
	g.POST("", h.CreateGitProvider)
	g.GET("/:gitProviderId", h.GetGitProvider)
	g.GET("", h.ListGitProviders)
	g.PUT("/:gitProviderId", h.UpdateGitProvider)
	g.DELETE("/:gitProviderId", h.DeleteGitProvider)

	// GitHub sub-routes
	g.POST("/:gitProviderId/github", h.CreateGithub)
	g.PUT("/:gitProviderId/github/:githubId", h.UpdateGithub)

	// GitLab sub-routes
	g.POST("/:gitProviderId/gitlab", h.CreateGitlab)
	g.PUT("/:gitProviderId/gitlab/:gitlabId", h.UpdateGitlab)

	// Bitbucket sub-routes
	g.POST("/:gitProviderId/bitbucket", h.CreateBitbucket)
	g.PUT("/:gitProviderId/bitbucket/:bitbucketId", h.UpdateBitbucket)

	// Gitea sub-routes
	g.POST("/:gitProviderId/gitea", h.CreateGitea)
	g.PUT("/:gitProviderId/gitea/:giteaId", h.UpdateGitea)
}

type CreateGitProviderRequest struct {
	Name         string `json:"name" validate:"required"`
	ProviderType string `json:"providerType" validate:"required"`
}

func (h *Handler) CreateGitProvider(c echo.Context) error {
	var req CreateGitProviderRequest
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

	gp := &schema.GitProvider{
		Name:           req.Name,
		ProviderType:   schema.GitProviderType(req.ProviderType),
		OrganizationID: member.OrganizationID,
	}

	if err := h.DB.Create(gp).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, gp)
}

func (h *Handler) GetGitProvider(c echo.Context) error {
	id := c.Param("gitProviderId")

	var gp schema.GitProvider
	err := h.DB.
		Preload("Github").
		Preload("Gitlab").
		Preload("Bitbucket").
		Preload("Gitea").
		First(&gp, "\"gitProviderId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Git provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gp)
}

func (h *Handler) ListGitProviders(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var providers []schema.GitProvider
	err := h.DB.
		Preload("Github").
		Preload("Gitlab").
		Preload("Bitbucket").
		Preload("Gitea").
		Where("\"organizationId\" = ?", member.OrganizationID).
		Find(&providers).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, providers)
}

func (h *Handler) UpdateGitProvider(c echo.Context) error {
	id := c.Param("gitProviderId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var gp schema.GitProvider
	if err := h.DB.First(&gp, "\"gitProviderId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Git provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&gp).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gp)
}

func (h *Handler) DeleteGitProvider(c echo.Context) error {
	id := c.Param("gitProviderId")

	result := h.DB.Delete(&schema.GitProvider{}, "\"gitProviderId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Git provider not found")
	}

	return c.NoContent(http.StatusNoContent)
}

// GitHub CRUD
func (h *Handler) CreateGithub(c echo.Context) error {
	gpID := c.Param("gitProviderId")

	var gh schema.Github
	if err := c.Bind(&gh); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	gh.GitProviderID = gpID

	if err := h.DB.Create(&gh).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, gh)
}

func (h *Handler) UpdateGithub(c echo.Context) error {
	ghID := c.Param("githubId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var gh schema.Github
	if err := h.DB.First(&gh, "\"githubId\" = ?", ghID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Github not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&gh).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gh)
}

// GitLab CRUD
func (h *Handler) CreateGitlab(c echo.Context) error {
	gpID := c.Param("gitProviderId")

	var gl schema.Gitlab
	if err := c.Bind(&gl); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	gl.GitProviderID = gpID

	if err := h.DB.Create(&gl).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, gl)
}

func (h *Handler) UpdateGitlab(c echo.Context) error {
	glID := c.Param("gitlabId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var gl schema.Gitlab
	if err := h.DB.First(&gl, "\"gitlabId\" = ?", glID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitlab not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&gl).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gl)
}

// Bitbucket CRUD
func (h *Handler) CreateBitbucket(c echo.Context) error {
	gpID := c.Param("gitProviderId")

	var bb schema.Bitbucket
	if err := c.Bind(&bb); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	bb.GitProviderID = gpID

	if err := h.DB.Create(&bb).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, bb)
}

func (h *Handler) UpdateBitbucket(c echo.Context) error {
	bbID := c.Param("bitbucketId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var bb schema.Bitbucket
	if err := h.DB.First(&bb, "\"bitbucketId\" = ?", bbID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Bitbucket not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&bb).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, bb)
}

// Gitea CRUD
func (h *Handler) CreateGitea(c echo.Context) error {
	gpID := c.Param("gitProviderId")

	var gt schema.Gitea
	if err := c.Bind(&gt); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	gt.GitProviderID = gpID

	if err := h.DB.Create(&gt).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, gt)
}

func (h *Handler) UpdateGitea(c echo.Context) error {
	gtID := c.Param("giteaId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var gt schema.Gitea
	if err := h.DB.First(&gt, "\"giteaId\" = ?", gtID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitea not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&gt).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gt)
}
