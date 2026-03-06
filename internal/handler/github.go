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

func (h *Handler) registerGithubRoutes(g *echo.Group) {
	g.GET("/:githubId", h.GetGithubOne)
	g.PUT("/:githubId", h.UpdateGithubProvider)
	g.GET("/:githubId/repositories", h.GetGithubRepositories)
	g.GET("/:githubId/branches", h.GetGithubBranches)
	g.POST("/:githubId/test-connection", h.TestGithubConnection)
	g.GET("", h.ListGithubProviders)
}

func (h *Handler) GetGithubOne(c echo.Context) error {
	id := c.Param("githubId")

	var gh schema.Github
	err := h.DB.
		Preload("GitProvider").
		First(&gh, "\"githubId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Github provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, gh)
}

func (h *Handler) ListGithubProviders(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var providers []schema.Github
	err := h.DB.
		Preload("GitProvider").
		Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = github.\"gitProviderId\"").
		Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
		Where("github.\"githubAppId\" IS NOT NULL").
		Where("github.\"githubPrivateKey\" IS NOT NULL").
		Where("github.\"githubInstallationId\" IS NOT NULL").
		Find(&providers).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, providers)
}

func (h *Handler) GetGithubRepositories(c echo.Context) error {
	id := c.Param("githubId")

	var gh schema.Github
	if err := h.DB.First(&gh, "\"githubId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Github provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if gh.GithubAppID == nil || gh.GithubPrivateKey == nil || gh.GithubInstallationID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Github app not fully configured")
	}

	repos, err := h.fetchGithubRepos(&gh)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, repos)
}

func (h *Handler) GetGithubBranches(c echo.Context) error {
	id := c.Param("githubId")
	owner := c.QueryParam("owner")
	repo := c.QueryParam("repo")

	if owner == "" || repo == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "owner and repo are required")
	}

	var gh schema.Github
	if err := h.DB.First(&gh, "\"githubId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Github provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	branches, err := h.fetchGithubBranches(&gh, owner, repo)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, branches)
}

func (h *Handler) TestGithubConnection(c echo.Context) error {
	id := c.Param("githubId")

	var gh schema.Github
	if err := h.DB.First(&gh, "\"githubId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Github provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	repos, err := h.fetchGithubRepos(&gh)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, fmt.Sprintf("Found %d repositories", len(repos)))
}

func (h *Handler) UpdateGithubProvider(c echo.Context) error {
	id := c.Param("githubId")

	var req struct {
		GithubAppName *string `json:"githubAppName"`
		Name          *string `json:"name"`
		GitProviderID *string `json:"gitProviderId"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var gh schema.Github
	if err := h.DB.Preload("GitProvider").First(&gh, "\"githubId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Github provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if req.GithubAppName != nil {
		h.DB.Model(&gh).Update("githubAppName", *req.GithubAppName)
	}

	if req.Name != nil && gh.GitProvider != nil {
		h.DB.Model(gh.GitProvider).Update("name", *req.Name)
	}

	return c.JSON(http.StatusOK, gh)
}

// fetchGithubRepos uses the GitHub App installation API to list accessible repos.
func (h *Handler) fetchGithubRepos(gh *schema.Github) ([]interface{}, error) {
	if gh.GithubInstallationID == nil || gh.GithubAppID == nil || gh.GithubPrivateKey == nil {
		return nil, fmt.Errorf("github app not configured")
	}

	// Generate installation access token using JWT
	token, err := generateGithubInstallationToken(*gh.GithubAppID, *gh.GithubInstallationID, *gh.GithubPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate installation token: %w", err)
	}

	// Fetch repos using the installation token
	var allRepos []interface{}
	page := 1
	for {
		url := fmt.Sprintf("https://api.github.com/installation/repositories?per_page=100&page=%d", page)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var result struct {
			Repositories []interface{} `json:"repositories"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if len(result.Repositories) == 0 {
			break
		}
		allRepos = append(allRepos, result.Repositories...)
		if len(result.Repositories) < 100 {
			break
		}
		page++
	}

	return allRepos, nil
}

// fetchGithubBranches lists branches for a repo using the GitHub App.
func (h *Handler) fetchGithubBranches(gh *schema.Github, owner, repo string) ([]interface{}, error) {
	token, err := generateGithubInstallationToken(*gh.GithubAppID, *gh.GithubInstallationID, *gh.GithubPrivateKey)
	if err != nil {
		return nil, err
	}

	var allBranches []interface{}
	page := 1
	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches?per_page=100&page=%d", owner, repo, page)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var branches []interface{}
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

// generateGithubInstallationToken creates a JWT and exchanges it for an installation access token.
func generateGithubInstallationToken(appID int, installationID string, privateKeyPEM string) (string, error) {
	// For now, use a simplified approach via the GitHub API
	// In production, this would create a JWT signed with the app's private key
	// and exchange it for an installation token via POST /app/installations/{id}/access_tokens

	// This is a placeholder that would need proper JWT implementation
	// when the GitHub App integration is fully tested
	return "", fmt.Errorf("github app token generation requires JWT library - configure via webhook instead")
}
