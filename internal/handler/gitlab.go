package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerGitlabRoutes(g *echo.Group) {
	g.POST("", h.CreateGitlabProvider)
	g.GET("/:gitlabId", h.GetGitlabOne)
	g.PUT("/:gitlabId", h.UpdateGitlabProvider)
	g.GET("", h.ListGitlabProviders)
	g.GET("/:gitlabId/repositories", h.GetGitlabRepositories)
	g.GET("/:gitlabId/branches", h.GetGitlabBranches)
	g.POST("/:gitlabId/test-connection", h.TestGitlabConnection)
}

func (h *Handler) CreateGitlabProvider(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var req struct {
		Name              string  `json:"name"`
		GitlabURL         string  `json:"gitlabUrl"`
		GitlabInternalURL *string `json:"gitlabInternalUrl"`
		ApplicationID     *string `json:"applicationId"`
		RedirectURI       *string `json:"redirectUri"`
		Secret            *string `json:"secret"`
		GroupName         *string `json:"groupName"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	tx := h.DB.Begin()

	gp := &schema.GitProvider{
		Name:           req.Name,
		ProviderType:   schema.GitProviderTypeGitlab,
		OrganizationID: member.OrganizationID,
		UserID:         user.ID,
	}
	if err := tx.Create(gp).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	gl := &schema.Gitlab{
		GitlabURL:         req.GitlabURL,
		GitlabInternalURL: req.GitlabInternalURL,
		ApplicationID:     req.ApplicationID,
		RedirectURI:       req.RedirectURI,
		Secret:            req.Secret,
		GroupName:         req.GroupName,
		GitProviderID:     gp.GitProviderID,
	}
	if err := tx.Create(gl).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := tx.Commit().Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, gl)
}

func (h *Handler) GetGitlabOne(c echo.Context) error {
	id := c.Param("gitlabId")

	var gl schema.Gitlab
	err := h.DB.
		Preload("GitProvider").
		First(&gl, "\"gitlabId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitlab provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gl)
}

func (h *Handler) ListGitlabProviders(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var providers []schema.Gitlab
	err := h.DB.
		Preload("GitProvider").
		Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = gitlab.\"gitProviderId\"").
		Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
		Where("gitlab.\"accessToken\" IS NOT NULL").
		Where("gitlab.\"refreshToken\" IS NOT NULL").
		Find(&providers).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, providers)
}

func (h *Handler) UpdateGitlabProvider(c echo.Context) error {
	id := c.Param("gitlabId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var gl schema.Gitlab
	if err := h.DB.Preload("GitProvider").First(&gl, "\"gitlabId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitlab provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Update git provider name if provided
	if name, ok := updates["name"]; ok {
		if gl.GitProvider != nil {
			h.DB.Model(gl.GitProvider).Update("name", name)
		}
		delete(updates, "name")
		delete(updates, "gitProviderId")
	}

	if len(updates) > 0 {
		if err := h.DB.Model(&gl).Updates(updates).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, gl)
}

func (h *Handler) GetGitlabRepositories(c echo.Context) error {
	id := c.Param("gitlabId")

	var gl schema.Gitlab
	if err := h.DB.First(&gl, "\"gitlabId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitlab provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if gl.AccessToken == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Gitlab not authenticated")
	}

	repos, err := h.fetchGitlabRepos(&gl)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, repos)
}

func (h *Handler) GetGitlabBranches(c echo.Context) error {
	id := c.Param("gitlabId")
	owner := c.QueryParam("owner")
	repo := c.QueryParam("repo")
	projectID := c.QueryParam("id")

	if (owner == "" || repo == "") && projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "owner/repo or project id is required")
	}

	var gl schema.Gitlab
	if err := h.DB.First(&gl, "\"gitlabId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitlab provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if gl.AccessToken == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Gitlab not authenticated")
	}

	project := projectID
	if project == "" {
		project = url.PathEscape(owner + "/" + repo)
	}

	branches, err := h.fetchGitlabBranches(&gl, project)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, branches)
}

func (h *Handler) TestGitlabConnection(c echo.Context) error {
	id := c.Param("gitlabId")

	var gl schema.Gitlab
	if err := h.DB.First(&gl, "\"gitlabId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Gitlab provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	repos, err := h.fetchGitlabRepos(&gl)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, fmt.Sprintf("Found %d repositories", len(repos)))
}

func (h *Handler) fetchGitlabRepos(gl *schema.Gitlab) ([]map[string]interface{}, error) {
	baseURL := gl.GitlabURL
	if gl.GitlabInternalURL != nil && *gl.GitlabInternalURL != "" {
		baseURL = *gl.GitlabInternalURL
	}

	var allRepos []map[string]interface{}
	page := 1
	for {
		apiURL := fmt.Sprintf("%s/api/v4/projects?membership=true&per_page=100&page=%d", baseURL, page)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("Authorization", "Bearer "+*gl.AccessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var projects []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&projects)
		resp.Body.Close()

		if len(projects) == 0 {
			break
		}

		for _, p := range projects {
			repo := map[string]interface{}{
				"id":   p["id"],
				"name": p["name"],
				"url":  p["path_with_namespace"],
			}
			if ns, ok := p["namespace"].(map[string]interface{}); ok {
				repo["owner"] = map[string]interface{}{
					"username": ns["path"],
				}
			}
			allRepos = append(allRepos, repo)
		}

		if len(projects) < 100 {
			break
		}
		page++
	}

	return allRepos, nil
}

func (h *Handler) fetchGitlabBranches(gl *schema.Gitlab, projectID string) ([]map[string]interface{}, error) {
	baseURL := gl.GitlabURL
	if gl.GitlabInternalURL != nil && *gl.GitlabInternalURL != "" {
		baseURL = *gl.GitlabInternalURL
	}

	var allBranches []map[string]interface{}
	page := 1
	for {
		apiURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/branches?per_page=100&page=%d", baseURL, projectID, page)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("Authorization", "Bearer "+*gl.AccessToken)

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
		allBranches = append(allBranches, branches...)
		if len(branches) < 100 {
			break
		}
		page++
	}

	return allBranches, nil
}
