package handler

import (
	"encoding/json"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerGitProviderTRPC(r procedureRegistry) {
	// gitProvider.getAll (matches TS frontend tRPC call)
	r["gitProvider.getAll"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
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
	r["github.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GithubID string `json:"githubId"`
		}
		json.Unmarshal(input, &in)
		var gh schema.Github
		if err := h.DB.Preload("GitProvider").First(&gh, "\"githubId\" = ?", in.GithubID).Error; err != nil {
			return nil, &trpcErr{"GitHub provider not found", "NOT_FOUND", 404}
		}
		return gh, nil
	}

	r["github.githubProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var githubs []schema.Github
		h.DB.Preload("GitProvider").
			Joins("JOIN \"git_provider\" ON \"git_provider\".\"gitProviderId\" = \"github\".\"gitProviderId\"").
			Where("\"git_provider\".\"organizationId\" = ?", member.OrganizationID).
			Find(&githubs)
		if githubs == nil {
			githubs = []schema.Github{}
		}
		return githubs, nil
	}

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

	r["gitlab.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GitlabID string `json:"gitlabId"`
		}
		json.Unmarshal(input, &in)
		var gl schema.Gitlab
		if err := h.DB.Preload("GitProvider").First(&gl, "\"gitlabId\" = ?", in.GitlabID).Error; err != nil {
			return nil, &trpcErr{"GitLab provider not found", "NOT_FOUND", 404}
		}
		return gl, nil
	}

	r["gitlab.gitlabProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var gitlabs []schema.Gitlab
		h.DB.Preload("GitProvider").
			Joins("JOIN \"git_provider\" ON \"git_provider\".\"gitProviderId\" = \"gitlab\".\"gitProviderId\"").
			Where("\"git_provider\".\"organizationId\" = ?", member.OrganizationID).
			Find(&gitlabs)
		if gitlabs == nil {
			gitlabs = []schema.Gitlab{}
		}
		return gitlabs, nil
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

	r["bitbucket.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BitbucketID string `json:"bitbucketId"`
		}
		json.Unmarshal(input, &in)
		var bb schema.Bitbucket
		if err := h.DB.Preload("GitProvider").First(&bb, "\"bitbucketId\" = ?", in.BitbucketID).Error; err != nil {
			return nil, &trpcErr{"Bitbucket provider not found", "NOT_FOUND", 404}
		}
		return bb, nil
	}

	r["bitbucket.bitbucketProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var bitbuckets []schema.Bitbucket
		h.DB.Preload("GitProvider").
			Joins("JOIN \"git_provider\" ON \"git_provider\".\"gitProviderId\" = \"bitbucket\".\"gitProviderId\"").
			Where("\"git_provider\".\"organizationId\" = ?", member.OrganizationID).
			Find(&bitbuckets)
		if bitbuckets == nil {
			bitbuckets = []schema.Bitbucket{}
		}
		return bitbuckets, nil
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

	r["gitea.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			GiteaID string `json:"giteaId"`
		}
		json.Unmarshal(input, &in)
		var gt schema.Gitea
		if err := h.DB.Preload("GitProvider").First(&gt, "\"giteaId\" = ?", in.GiteaID).Error; err != nil {
			return nil, &trpcErr{"Gitea provider not found", "NOT_FOUND", 404}
		}
		return gt, nil
	}

	r["gitea.giteaProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var giteas []schema.Gitea
		h.DB.Preload("GitProvider").
			Joins("JOIN \"git_provider\" ON \"git_provider\".\"gitProviderId\" = \"gitea\".\"gitProviderId\"").
			Where("\"git_provider\".\"organizationId\" = ?", member.OrganizationID).
			Find(&giteas)
		if giteas == nil {
			giteas = []schema.Gitea{}
		}
		return giteas, nil
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
	r["previewDeployment.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string `json:"applicationId"`
		}
		json.Unmarshal(input, &in)
		var previews []schema.PreviewDeployment
		h.DB.Preload("Deployments").Preload("Domains").
			Where("\"applicationId\" = ?", in.ApplicationID).
			Order("\"createdAt\" DESC").
			Find(&previews)
		if previews == nil {
			previews = []schema.PreviewDeployment{}
		}
		return previews, nil
	}

	r["previewDeployment.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PreviewDeploymentID string `json:"previewDeploymentId"`
		}
		json.Unmarshal(input, &in)
		var preview schema.PreviewDeployment
		if err := h.DB.Preload("Application").Preload("Deployments").Preload("Domains").
			First(&preview, "\"previewDeploymentId\" = ?", in.PreviewDeploymentID).Error; err != nil {
			return nil, &trpcErr{"Preview deployment not found", "NOT_FOUND", 404}
		}
		return preview, nil
	}

	r["previewDeployment.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PreviewDeploymentID string `json:"previewDeploymentId"`
		}
		json.Unmarshal(input, &in)
		if h.PreviewSvc != nil {
			if err := h.PreviewSvc.RemovePreviewDeployment(in.PreviewDeploymentID); err != nil {
				return nil, err
			}
		} else {
			h.DB.Delete(&schema.PreviewDeployment{}, "\"previewDeploymentId\" = ?", in.PreviewDeploymentID)
		}
		return true, nil
	}

	r["previewDeployment.redeploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PreviewDeploymentID string  `json:"previewDeploymentId"`
			Title               *string `json:"title"`
			Description         *string `json:"description"`
		}
		json.Unmarshal(input, &in)

		var preview schema.PreviewDeployment
		if err := h.DB.First(&preview, "\"previewDeploymentId\" = ?", in.PreviewDeploymentID).Error; err != nil {
			return nil, &trpcErr{"Preview deployment not found", "NOT_FOUND", 404}
		}

		// Queue a deploy for the preview's application
		if h.Queue != nil {
			title := "Rebuild Preview Deployment"
			if in.Title != nil {
				title = *in.Title
			}
			_, err := h.Queue.EnqueueDeployApplication(preview.ApplicationID, &title, in.Description)
			if err != nil {
				return nil, err
			}
		}
		return true, nil
	}
}
