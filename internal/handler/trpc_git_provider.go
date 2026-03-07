package handler

import (
	"encoding/json"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerGitProviderTRPC(r procedureRegistry) {
	// gitProvider.all
	r["gitProvider.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var providers []schema.GitProvider
		h.DB.Preload("Github").Preload("Gitlab").Preload("Bitbucket").Preload("Gitea").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Find(&providers)
		if providers == nil {
			providers = []schema.GitProvider{}
		}
		return providers, nil
	}

	r["gitProvider.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GitProviderID string `json:"gitProviderId"`
		}
		json.Unmarshal(input, &in)
		var gp schema.GitProvider
		if err := h.DB.Preload("Github").Preload("Gitlab").Preload("Bitbucket").Preload("Gitea").
			First(&gp, "\"gitProviderId\" = ?", in.GitProviderID).Error; err != nil {
			return nil, &trpcErr{"Git provider not found", "NOT_FOUND", 404}
		}
		return gp, nil
	}

	r["gitProvider.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GitProviderID string `json:"gitProviderId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.GitProvider{}, "\"gitProviderId\" = ?", in.GitProviderID)
		return true, nil
	}

	// GitHub
	r["github.testConnection"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GithubID string `json:"githubId"`
		}
		json.Unmarshal(input, &in)
		var gh schema.Github
		if err := h.DB.First(&gh, "\"githubId\" = ?", in.GithubID).Error; err != nil {
			return nil, &trpcErr{"GitHub provider not found", "NOT_FOUND", 404}
		}
		return true, nil
	}

	r["github.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["githubId"].(string)
		delete(in, "githubId")
		h.DB.Model(&schema.Github{}).Where("\"githubId\" = ?", id).Updates(in)
		return true, nil
	}

	r["github.getGithubRepositories"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	r["github.getGithubBranches"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	// GitLab
	r["gitlab.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			GitlabURL     string `json:"gitlabUrl"`
			ApplicationID string `json:"applicationId"`
			Secret        string `json:"secret"`
			Name          string `json:"name"`
			GroupName     string `json:"groupName"`
		}
		json.Unmarshal(input, &in)

		gitlabURL := in.GitlabURL
		if gitlabURL == "" {
			gitlabURL = "https://gitlab.com"
		}

		gp := schema.GitProvider{
			ProviderType:   "gitlab",
			Name:           in.Name,
			OrganizationID: member.OrganizationID,
		}
		h.DB.Create(&gp)

		appID := in.ApplicationID
		secret := in.Secret
		gl := schema.Gitlab{
			ApplicationID: &appID,
			Secret:        &secret,
			GitlabURL:     gitlabURL,
			GroupName:      &in.GroupName,
			GitProviderID:  gp.GitProviderID,
		}
		h.DB.Create(&gl)
		return gl, nil
	}

	r["gitlab.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["gitlabId"].(string)
		delete(in, "gitlabId")
		h.DB.Model(&schema.Gitlab{}).Where("\"gitlabId\" = ?", id).Updates(in)
		return true, nil
	}

	r["gitlab.testConnection"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GitlabID string `json:"gitlabId"`
		}
		json.Unmarshal(input, &in)
		var gl schema.Gitlab
		if err := h.DB.First(&gl, "\"gitlabId\" = ?", in.GitlabID).Error; err != nil {
			return nil, &trpcErr{"GitLab provider not found", "NOT_FOUND", 404}
		}
		return true, nil
	}

	r["gitlab.getGitlabRepositories"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	r["gitlab.getGitlabBranches"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	// Bitbucket
	r["bitbucket.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Username    string  `json:"username"`
			AppPassword string  `json:"appPassword"`
			ApiToken    *string `json:"apiToken"`
			Workspace   string  `json:"workspace"`
			Name        string  `json:"name"`
		}
		json.Unmarshal(input, &in)

		gp := schema.GitProvider{
			ProviderType:   "bitbucket",
			Name:           in.Name,
			OrganizationID: member.OrganizationID,
		}
		h.DB.Create(&gp)

		username := in.Username
		workspace := in.Workspace
		bb := schema.Bitbucket{
			BitbucketUsername:      &username,
			AppPassword:            &in.AppPassword,
			APIToken:               in.ApiToken,
			BitbucketWorkspaceName: &workspace,
			GitProviderID:          gp.GitProviderID,
		}
		h.DB.Create(&bb)
		return bb, nil
	}

	r["bitbucket.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["bitbucketId"].(string)
		delete(in, "bitbucketId")
		h.DB.Model(&schema.Bitbucket{}).Where("\"bitbucketId\" = ?", id).Updates(in)
		return true, nil
	}

	r["bitbucket.testConnection"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["bitbucket.getBitbucketRepositories"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	r["bitbucket.getBitbucketBranches"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	// Gitea
	r["gitea.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Name     string `json:"name"`
			GiteaURL string `json:"giteaUrl"`
			Token    string `json:"token"`
		}
		json.Unmarshal(input, &in)

		gp := schema.GitProvider{
			ProviderType:   "gitea",
			Name:           in.Name,
			OrganizationID: member.OrganizationID,
		}
		h.DB.Create(&gp)

		token := in.Token
		gt := schema.Gitea{
			AccessToken:   &token,
			GiteaURL:      in.GiteaURL,
			GitProviderID: gp.GitProviderID,
		}
		h.DB.Create(&gt)
		return gt, nil
	}

	r["gitea.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["giteaId"].(string)
		delete(in, "giteaId")
		h.DB.Model(&schema.Gitea{}).Where("\"giteaId\" = ?", id).Updates(in)
		return true, nil
	}

	r["gitea.testConnection"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["gitea.getGiteaRepositories"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	r["gitea.getGiteaBranches"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	r["gitea.getGiteaUrl"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GiteaID string `json:"giteaId"`
		}
		json.Unmarshal(input, &in)
		var gt schema.Gitea
		if err := h.DB.First(&gt, "\"giteaId\" = ?", in.GiteaID).Error; err != nil {
			return "", nil
		}
		return gt.GiteaURL, nil
	}

	// Preview Deployment
	r["previewDeployment.redeploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// TODO: Implement redeploy when PreviewService supports it
		return true, nil
	}
}
