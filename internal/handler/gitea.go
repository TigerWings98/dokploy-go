// Input: db (GitProvider 表), provider/gitea
// Output: Gitea 仓库/分支列表查询的 tRPC procedure 实现
// Role: Gitea 专属 handler，通过 Gitea API 查询用户仓库和分支
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerGiteaRoutes(g *echo.Group) {
	g.POST("", h.CreateGiteaProvider)
	g.GET("/:giteaId", h.GetGiteaOne)
	g.PUT("/:giteaId", h.UpdateGiteaProvider)
	g.GET("", h.ListGiteaProviders)
	g.GET("/:giteaId/repositories", h.GetGiteaRepositories)
	g.GET("/:giteaId/branches", h.GetGiteaBranches)
	g.POST("/:giteaId/test-connection", h.TestGiteaConnection)
	g.GET("/:giteaId/url", h.GetGiteaURL)
}

func (h *Handler) CreateGiteaProvider(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var req struct {
		Name             string  `json:"name"`
		GiteaURL         string  `json:"giteaUrl"`
		GiteaInternalURL *string `json:"giteaInternalUrl"`
		ClientID         *string `json:"clientId"`
		ClientSecret     *string `json:"clientSecret"`
		RedirectURI      *string `json:"redirectUri"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	tx := h.DB.Begin()

	gp := &schema.GitProvider{
		Name:           req.Name,
		ProviderType:   schema.GitProviderTypeGitea,
		OrganizationID: member.OrganizationID,
		UserID:         user.ID,
	}
	if err := tx.Create(gp).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	gt := &schema.Gitea{
		GiteaURL:         req.GiteaURL,
		GiteaInternalURL: req.GiteaInternalURL,
		ClientID:         req.ClientID,
		ClientSecret:     req.ClientSecret,
		RedirectURI:      req.RedirectURI,
		GitProviderID:    gp.GitProviderID,
	}
	if err := tx.Create(gt).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := tx.Commit().Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"giteaId":  gt.GiteaID,
		"clientId": gt.ClientID,
		"giteaUrl": gt.GiteaURL,
	})
}

func (h *Handler) GetGiteaOne(c echo.Context) error {
	id := c.Param("giteaId")

	var gt schema.Gitea
	err := h.DB.
		Preload("GitProvider").
		First(&gt, "\"giteaId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitea provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gt)
}

func (h *Handler) ListGiteaProviders(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var providers []schema.Gitea
	err := h.DB.
		Preload("GitProvider").
		Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = gitea.\"gitProviderId\"").
		Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
		Where("gitea.\"clientId\" IS NOT NULL").
		Where("gitea.\"clientSecret\" IS NOT NULL").
		Find(&providers).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, providers)
}

func (h *Handler) UpdateGiteaProvider(c echo.Context) error {
	id := c.Param("giteaId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var gt schema.Gitea
	if err := h.DB.Preload("GitProvider").First(&gt, "\"giteaId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitea provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if name, ok := updates["name"]; ok {
		if gt.GitProvider != nil {
			h.DB.Model(gt.GitProvider).Update("name", name)
		}
		delete(updates, "name")
		delete(updates, "gitProviderId")
	}

	if len(updates) > 0 {
		if err := h.DB.Model(&gt).Updates(updates).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) GetGiteaRepositories(c echo.Context) error {
	id := c.Param("giteaId")

	var gt schema.Gitea
	if err := h.DB.First(&gt, "\"giteaId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitea provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if gt.AccessToken == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Gitea not authenticated")
	}

	repos, err := h.fetchGiteaRepos(&gt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, repos)
}

func (h *Handler) GetGiteaBranches(c echo.Context) error {
	id := c.Param("giteaId")
	owner := c.QueryParam("owner")
	repo := c.QueryParam("repositoryName")
	if repo == "" {
		repo = c.QueryParam("repo")
	}

	if owner == "" || repo == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "owner and repositoryName are required")
	}

	var gt schema.Gitea
	if err := h.DB.First(&gt, "\"giteaId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitea provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if gt.AccessToken == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Gitea not authenticated")
	}

	branches, err := h.fetchGiteaBranches(&gt, owner, repo)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, branches)
}

func (h *Handler) TestGiteaConnection(c echo.Context) error {
	id := c.Param("giteaId")

	var gt schema.Gitea
	if err := h.DB.First(&gt, "\"giteaId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitea provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	repos, err := h.fetchGiteaRepos(&gt)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, fmt.Sprintf("Found %d repositories", len(repos)))
}

func (h *Handler) GetGiteaURL(c echo.Context) error {
	id := c.Param("giteaId")

	var gt schema.Gitea
	if err := h.DB.First(&gt, "\"giteaId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitea provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gt.GiteaURL)
}

func (h *Handler) fetchGiteaRepos(gt *schema.Gitea) ([]map[string]interface{}, error) {
	baseURL := gt.GiteaURL
	if gt.GiteaInternalURL != nil && *gt.GiteaInternalURL != "" {
		baseURL = *gt.GiteaInternalURL
	}

	var allRepos []map[string]interface{}
	page := 1
	for {
		apiURL := fmt.Sprintf("%s/api/v1/user/repos?page=%d&limit=50", baseURL, page)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("Authorization", "token "+*gt.AccessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var repos []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&repos)
		resp.Body.Close()

		if len(repos) == 0 {
			break
		}

		for _, r := range repos {
			repo := map[string]interface{}{
				"id":   r["id"],
				"name": r["name"],
				"url":  r["full_name"],
			}
			if owner, ok := r["owner"].(map[string]interface{}); ok {
				repo["owner"] = map[string]interface{}{
					"username": owner["login"],
				}
			}
			allRepos = append(allRepos, repo)
		}

		if len(repos) < 50 {
			break
		}
		page++
	}

	return allRepos, nil
}

func (h *Handler) fetchGiteaBranches(gt *schema.Gitea, owner, repo string) ([]map[string]interface{}, error) {
	baseURL := gt.GiteaURL
	if gt.GiteaInternalURL != nil && *gt.GiteaInternalURL != "" {
		baseURL = *gt.GiteaInternalURL
	}

	var allBranches []map[string]interface{}
	page := 1
	for {
		apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches?page=%d&limit=50", baseURL, owner, repo, page)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("Authorization", "token "+*gt.AccessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var branches []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&branches)
		resp.Body.Close()

		if len(branches) == 0 {
			break
		}

		for _, b := range branches {
			branch := map[string]interface{}{
				"id":   b["name"],
				"name": b["name"],
			}
			if commit, ok := b["commit"].(map[string]interface{}); ok {
				branch["commit"] = map[string]interface{}{
					"id": commit["id"],
				}
			}
			allBranches = append(allBranches, branch)
		}

		if len(branches) < 50 {
			break
		}
		page++
	}

	return allBranches, nil
}
