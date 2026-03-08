// Input: db (GitProvider 表), provider/bitbucket
// Output: Bitbucket 仓库/分支列表查询的 tRPC procedure 实现
// Role: Bitbucket 专属 handler，通过 Bitbucket API 查询用户仓库和分支
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

func (h *Handler) registerBitbucketRoutes(g *echo.Group) {
	g.POST("", h.CreateBitbucketProvider)
	g.GET("/:bitbucketId", h.GetBitbucketOne)
	g.PUT("/:bitbucketId", h.UpdateBitbucketProvider)
	g.GET("", h.ListBitbucketProviders)
	g.GET("/:bitbucketId/repositories", h.GetBitbucketRepositories)
	g.GET("/:bitbucketId/branches", h.GetBitbucketBranches)
	g.POST("/:bitbucketId/test-connection", h.TestBitbucketConnection)
}

func (h *Handler) CreateBitbucketProvider(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var req struct {
		Name                   string  `json:"name"`
		BitbucketUsername      *string `json:"bitbucketUsername"`
		BitbucketEmail         *string `json:"bitbucketEmail"`
		AppPassword            *string `json:"appPassword"`
		APIToken               *string `json:"apiToken"`
		BitbucketWorkspaceName *string `json:"bitbucketWorkspaceName"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	tx := h.DB.Begin()

	gp := &schema.GitProvider{
		Name:           req.Name,
		ProviderType:   schema.GitProviderTypeBitbucket,
		OrganizationID: member.OrganizationID,
		UserID:         user.ID,
	}
	if err := tx.Create(gp).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	bb := &schema.Bitbucket{
		BitbucketUsername:      req.BitbucketUsername,
		BitbucketEmail:         req.BitbucketEmail,
		AppPassword:            req.AppPassword,
		APIToken:               req.APIToken,
		BitbucketWorkspaceName: req.BitbucketWorkspaceName,
		GitProviderID:          gp.GitProviderID,
	}
	if err := tx.Create(bb).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := tx.Commit().Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, bb)
}

func (h *Handler) GetBitbucketOne(c echo.Context) error {
	id := c.Param("bitbucketId")

	var bb schema.Bitbucket
	err := h.DB.
		Preload("GitProvider").
		First(&bb, "\"bitbucketId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Bitbucket provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, bb)
}

func (h *Handler) ListBitbucketProviders(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var providers []schema.Bitbucket
	err := h.DB.
		Preload("GitProvider").
		Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = bitbucket.\"gitProviderId\"").
		Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
		Find(&providers).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, providers)
}

func (h *Handler) UpdateBitbucketProvider(c echo.Context) error {
	id := c.Param("bitbucketId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var bb schema.Bitbucket
	if err := h.DB.Preload("GitProvider").First(&bb, "\"bitbucketId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Bitbucket provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Remove non-bitbucket fields
	delete(updates, "name")
	delete(updates, "gitProviderId")

	if len(updates) > 0 {
		if err := h.DB.Model(&bb).Updates(updates).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, bb)
}

func (h *Handler) GetBitbucketRepositories(c echo.Context) error {
	id := c.Param("bitbucketId")

	var bb schema.Bitbucket
	if err := h.DB.First(&bb, "\"bitbucketId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Bitbucket provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	repos, err := h.fetchBitbucketRepos(&bb)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, repos)
}

func (h *Handler) GetBitbucketBranches(c echo.Context) error {
	id := c.Param("bitbucketId")
	owner := c.QueryParam("owner")
	repo := c.QueryParam("repo")

	if owner == "" || repo == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "owner and repo are required")
	}

	var bb schema.Bitbucket
	if err := h.DB.First(&bb, "\"bitbucketId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Bitbucket provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	branches, err := h.fetchBitbucketBranches(&bb, owner, repo)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, branches)
}

func (h *Handler) TestBitbucketConnection(c echo.Context) error {
	id := c.Param("bitbucketId")

	var bb schema.Bitbucket
	if err := h.DB.First(&bb, "\"bitbucketId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Bitbucket provider not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	repos, err := h.fetchBitbucketRepos(&bb)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, fmt.Sprintf("Found %d repositories", len(repos)))
}

func (h *Handler) bitbucketAuth(bb *schema.Bitbucket) (string, string) {
	username := ""
	password := ""

	if bb.BitbucketEmail != nil && bb.APIToken != nil {
		username = *bb.BitbucketEmail
		password = *bb.APIToken
	} else if bb.BitbucketUsername != nil && bb.AppPassword != nil {
		username = *bb.BitbucketUsername
		password = *bb.AppPassword
	}

	return username, password
}

func (h *Handler) fetchBitbucketRepos(bb *schema.Bitbucket) ([]map[string]interface{}, error) {
	workspace := ""
	if bb.BitbucketWorkspaceName != nil {
		workspace = *bb.BitbucketWorkspaceName
	} else if bb.BitbucketUsername != nil {
		workspace = *bb.BitbucketUsername
	}

	if workspace == "" {
		return nil, fmt.Errorf("workspace name is required")
	}

	username, password := h.bitbucketAuth(bb)
	if username == "" {
		return nil, fmt.Errorf("bitbucket authentication not configured")
	}

	var allRepos []map[string]interface{}
	nextURL := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s?pagelen=100", workspace)

	for nextURL != "" {
		req, _ := http.NewRequest("GET", nextURL, nil)
		req.SetBasicAuth(username, password)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var result struct {
			Values []map[string]interface{} `json:"values"`
			Next   string                   `json:"next"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		for _, r := range result.Values {
			repo := map[string]interface{}{
				"name": r["name"],
				"slug": r["slug"],
			}
			if links, ok := r["links"].(map[string]interface{}); ok {
				if html, ok := links["html"].(map[string]interface{}); ok {
					repo["url"] = html["href"]
				}
			}
			if ws, ok := r["workspace"].(map[string]interface{}); ok {
				repo["owner"] = map[string]interface{}{
					"username": ws["slug"],
				}
			}
			allRepos = append(allRepos, repo)
		}

		nextURL = result.Next
	}

	return allRepos, nil
}

func (h *Handler) fetchBitbucketBranches(bb *schema.Bitbucket, owner, repo string) ([]map[string]interface{}, error) {
	username, password := h.bitbucketAuth(bb)
	if username == "" {
		return nil, fmt.Errorf("bitbucket authentication not configured")
	}

	var allBranches []map[string]interface{}
	nextURL := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s/refs/branches?pagelen=100", owner, repo)

	for nextURL != "" {
		req, _ := http.NewRequest("GET", nextURL, nil)
		req.SetBasicAuth(username, password)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var result struct {
			Values []map[string]interface{} `json:"values"`
			Next   string                   `json:"next"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		for _, b := range result.Values {
			branch := map[string]interface{}{
				"name": b["name"],
			}
			if target, ok := b["target"].(map[string]interface{}); ok {
				branch["commit"] = map[string]interface{}{
					"sha": target["hash"],
				}
			}
			allBranches = append(allBranches, branch)
		}

		nextURL = result.Next
	}

	return allBranches, nil
}
