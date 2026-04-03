// Input: HTTP webhook 请求 (GitHub/GitLab/Gitea/Bitbucket), db, service
// Output: Webhook 端点 (POST /api/deploy/:refreshToken)，触发自动部署/PR 预览
// Role: Git webhook 接收器，验证签名后根据事件类型触发 Application/Compose 部署或 PR 预览管理
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/service"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerWebhookRoutes(e *echo.Echo) {
	webhooks := e.Group("/api/webhook")
	webhooks.POST("/github", h.GithubWebhook)
	webhooks.POST("/gitlab", h.GitlabWebhook)
	webhooks.POST("/bitbucket", h.BitbucketWebhook)
	webhooks.POST("/gitea", h.GiteaWebhook)
}

// enqueueWebhookDeploys enqueues deployment tasks for matched apps and composes.
func (h *Handler) enqueueWebhookDeploys(apps []schema.Application, composes []schema.Compose) {
	if h.Queue == nil {
		return
	}
	for _, app := range apps {
		title := "Webhook Deploy"
		_, err := h.Queue.EnqueueDeployApplication(app.ApplicationID, &title, nil)
		if err != nil {
			log.Printf("Failed to enqueue deploy for app %s: %v", app.ApplicationID, err)
		}
	}
	for _, compose := range composes {
		title := "Webhook Deploy"
		_, err := h.Queue.EnqueueDeployCompose(compose.ComposeID, &title)
		if err != nil {
			log.Printf("Failed to enqueue deploy for compose %s: %v", compose.ComposeID, err)
		}
	}
}

// GithubWebhook handles GitHub webhook events (push and pull_request).
func (h *Handler) GithubWebhook(c echo.Context) error {
	event := c.Request().Header.Get("X-GitHub-Event")
	signature := c.Request().Header.Get("X-Hub-Signature-256")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read body")
	}

	switch event {
	case "push":
		var payload struct {
			Ref        string `json:"ref"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}

		branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
		return h.handleGithubPush(c, body, signature, payload.Repository.FullName, branch)

	case "pull_request":
		var payload struct {
			Action      string `json:"action"`
			Number      int    `json:"number"`
			PullRequest struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
				Head  struct {
					Ref string `json:"ref"`
					SHA string `json:"sha"`
				} `json:"head"`
				HTMLURL string `json:"html_url"`
			} `json:"pull_request"`
			Repository struct {
				FullName string `json:"full_name"`
				Owner    struct {
					Login string `json:"login"`
				} `json:"owner"`
				Name string `json:"name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}

		switch payload.Action {
		case "opened", "synchronize", "reopened", "labeled":
			return h.handleGithubPR(c, payload.Repository.FullName, payload.PullRequest.Head.Ref, payload.Number,
				fmt.Sprintf("%d", payload.PullRequest.ID), payload.PullRequest.Title, payload.PullRequest.HTMLURL)
		case "closed":
			return h.handleGithubPRClosed(c, fmt.Sprintf("%d", payload.PullRequest.ID))
		default:
			return c.JSON(http.StatusOK, map[string]string{"message": "ignored action"})
		}

	default:
		return c.JSON(http.StatusOK, map[string]string{"message": "event not handled"})
	}
}

func (h *Handler) handleGithubPush(c echo.Context, body []byte, signature, repoFullName, branch string) error {
	var apps []schema.Application
	err := h.DB.
		Where("\"repository\" = ? AND \"branch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
			repoFullName, branch, schema.SourceTypeGithub, true).
		Find(&apps).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var composes []schema.Compose
	h.DB.
		Where("\"repository\" = ? AND \"branch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
			repoFullName, branch, schema.SourceTypeComposeGithub, true).
		Find(&composes)

	// Filter apps by webhook signature verification
	var verified []schema.Application
	for _, app := range apps {
		if app.GithubID != nil {
			var github schema.Github
			if err := h.DB.First(&github, "\"githubId\" = ?", *app.GithubID).Error; err == nil {
				if github.GithubWebhookSecret != nil && *github.GithubWebhookSecret != "" {
					if !verifyGithubSignature(body, signature, *github.GithubWebhookSecret) {
						continue
					}
				}
			}
		}
		verified = append(verified, app)
	}

	h.enqueueWebhookDeploys(verified, composes)
	total := len(verified) + len(composes)
	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("processed %d services", total)})
}

func (h *Handler) handleGithubPR(c echo.Context, repoFullName, headBranch string, prNumber int, prID, prTitle, prURL string) error {
	var apps []schema.Application
	err := h.DB.
		Where("\"repository\" = ? AND \"sourceType\" = ? AND \"isPreviewDeploymentsActive\" = ?",
			repoFullName, schema.SourceTypeGithub, true).
		Find(&apps).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	created := 0
	for _, app := range apps {
		if app.PreviewWildcard == nil || *app.PreviewWildcard == "" {
			continue
		}

		if h.PreviewSvc != nil {
			preview, err := h.PreviewSvc.CreatePreviewDeployment(service.CreatePreviewInput{
				ApplicationID:     app.ApplicationID,
				Branch:            headBranch,
				PullRequestID:     prID,
				PullRequestNumber: fmt.Sprintf("%d", prNumber),
				PullRequestURL:    prURL,
				PullRequestTitle:  prTitle,
			})
			if err != nil {
				log.Printf("Failed to create preview deployment for app %s: %v", app.ApplicationID, err)
				continue
			}

			// Queue deployment for the preview
			if h.Queue != nil {
				title := fmt.Sprintf("Preview PR #%d", prNumber)
				if _, err := h.Queue.EnqueueDeployApplication(preview.ApplicationID, &title, nil); err != nil {
					log.Printf("Failed to enqueue preview deploy: %v", err)
				}
			}
			created++
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("created %d preview deployments", created)})
}

func (h *Handler) handleGithubPRClosed(c echo.Context, prID string) error {
	if h.PreviewSvc == nil {
		return c.JSON(http.StatusOK, map[string]string{"message": "preview service not available"})
	}

	if err := h.PreviewSvc.RemovePreviewsByPullRequestID(prID); err != nil {
		log.Printf("Failed to cleanup preview deployments for PR %s: %v", prID, err)
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "preview deployments cleaned up"})
}

func verifyGithubSignature(payload []byte, signature, secret string) bool {
	if signature == "" {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// GitlabWebhook handles GitLab webhook events.
func (h *Handler) GitlabWebhook(c echo.Context) error {
	event := c.Request().Header.Get("X-Gitlab-Event")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read body")
	}

	switch event {
	case "Push Hook", "Tag Push Hook":
		var payload struct {
			Ref     string `json:"ref"`
			Project struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"project"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}

		branch := strings.TrimPrefix(payload.Ref, "refs/heads/")

		var apps []schema.Application
		h.DB.
			Where("\"gitlabRepository\" = ? AND \"gitlabBranch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
				payload.Project.PathWithNamespace, branch, schema.SourceTypeGitlab, true).
			Find(&apps)

		var composes []schema.Compose
		h.DB.
			Where("\"repository\" = ? AND \"branch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
				payload.Project.PathWithNamespace, branch, schema.SourceTypeComposeGitlab, true).
			Find(&composes)

		h.enqueueWebhookDeploys(apps, composes)
		total := len(apps) + len(composes)
		return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("processed %d services", total)})

	case "Merge Request Hook":
		var payload struct {
			ObjectAttributes struct {
				IID        int    `json:"iid"`
				ID         int    `json:"id"`
				Title      string `json:"title"`
				URL        string `json:"url"`
				SourceBranch string `json:"source_branch"`
				State      string `json:"state"`
				Action     string `json:"action"`
			} `json:"object_attributes"`
			Project struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"project"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}

		attrs := payload.ObjectAttributes
		prID := fmt.Sprintf("gitlab-%d", attrs.ID)

		if attrs.Action == "open" || attrs.Action == "update" || attrs.Action == "reopen" {
			return h.handleGenericPR(c, payload.Project.PathWithNamespace, schema.SourceTypeGitlab,
				attrs.SourceBranch, attrs.IID, prID, attrs.Title, attrs.URL)
		}
		if attrs.State == "closed" || attrs.State == "merged" {
			if h.PreviewSvc != nil {
				h.PreviewSvc.RemovePreviewsByPullRequestID(prID)
			}
			return c.JSON(http.StatusOK, map[string]string{"message": "preview deployments cleaned up"})
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "ignored action"})

	default:
		return c.JSON(http.StatusOK, map[string]string{"message": "event not handled"})
	}
}

// BitbucketWebhook handles Bitbucket webhook events.
func (h *Handler) BitbucketWebhook(c echo.Context) error {
	event := c.Request().Header.Get("X-Event-Key")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read body")
	}

	switch event {
	case "repo:push":
		var payload struct {
			Push struct {
				Changes []struct {
					New struct {
						Name string `json:"name"`
					} `json:"new"`
				} `json:"changes"`
			} `json:"push"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}

		branch := ""
		if len(payload.Push.Changes) > 0 {
			branch = payload.Push.Changes[0].New.Name
		}

		var apps []schema.Application
		h.DB.
			Where("\"bitbucketRepository\" = ? AND \"bitbucketBranch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
				payload.Repository.FullName, branch, schema.SourceTypeBitbucket, true).
			Find(&apps)

		var composes []schema.Compose
		h.DB.
			Where("\"repository\" = ? AND \"branch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
				payload.Repository.FullName, branch, schema.SourceTypeComposeBitbucket, true).
			Find(&composes)

		h.enqueueWebhookDeploys(apps, composes)
		total := len(apps) + len(composes)
		return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("processed %d services", total)})

	case "pullrequest:created", "pullrequest:updated":
		var payload struct {
			PullRequest struct {
				ID          int    `json:"id"`
				Title       string `json:"title"`
				Source      struct {
					Branch struct{ Name string } `json:"branch"`
				} `json:"source"`
				Links struct {
					HTML struct{ Href string } `json:"html"`
				} `json:"links"`
			} `json:"pullrequest"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}
		pr := payload.PullRequest
		prID := fmt.Sprintf("bitbucket-%d", pr.ID)
		return h.handleGenericPR(c, payload.Repository.FullName, schema.SourceTypeBitbucket,
			pr.Source.Branch.Name, pr.ID, prID, pr.Title, pr.Links.HTML.Href)

	case "pullrequest:fulfilled", "pullrequest:rejected":
		var payload struct {
			PullRequest struct{ ID int } `json:"pullrequest"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}
		prID := fmt.Sprintf("bitbucket-%d", payload.PullRequest.ID)
		if h.PreviewSvc != nil {
			h.PreviewSvc.RemovePreviewsByPullRequestID(prID)
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "preview deployments cleaned up"})

	default:
		return c.JSON(http.StatusOK, map[string]string{"message": "event not handled"})
	}
}

// GiteaWebhook handles Gitea webhook events.
func (h *Handler) GiteaWebhook(c echo.Context) error {
	event := c.Request().Header.Get("X-Gitea-Event")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read body")
	}

	switch event {
	case "pull_request":
		var payload struct {
			Action      string `json:"action"`
			Number      int    `json:"number"`
			PullRequest struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
				Head  struct{ Ref string } `json:"head_branch"`
				URL   string `json:"html_url"`
			} `json:"pull_request"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}
		prID := fmt.Sprintf("gitea-%d", payload.PullRequest.ID)
		if payload.Action == "opened" || payload.Action == "synchronized" || payload.Action == "reopened" {
			return h.handleGenericPR(c, payload.Repository.FullName, schema.SourceTypeGitea,
				payload.PullRequest.Head.Ref, payload.Number, prID, payload.PullRequest.Title, payload.PullRequest.URL)
		}
		if payload.Action == "closed" {
			if h.PreviewSvc != nil {
				h.PreviewSvc.RemovePreviewsByPullRequestID(prID)
			}
			return c.JSON(http.StatusOK, map[string]string{"message": "preview deployments cleaned up"})
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "ignored action"})
	case "push":
		// continue below
	default:
		return c.JSON(http.StatusOK, map[string]string{"message": "event not handled"})
	}

	var pushPayload struct {
		Ref        string `json:"ref"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &pushPayload); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
	}

	branch := strings.TrimPrefix(pushPayload.Ref, "refs/heads/")

	var apps []schema.Application
	h.DB.
		Where("\"giteaRepository\" = ? AND \"giteaBranch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
			pushPayload.Repository.FullName, branch, schema.SourceTypeGitea, true).
		Find(&apps)

	var composes []schema.Compose
	h.DB.
		Where("\"repository\" = ? AND \"branch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
			pushPayload.Repository.FullName, branch, schema.SourceTypeComposeGitea, true).
		Find(&composes)

	h.enqueueWebhookDeploys(apps, composes)
	total := len(apps) + len(composes)
	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("processed %d services", total)})
}

// handleGenericPR creates preview deployments for any git provider.
func (h *Handler) handleGenericPR(c echo.Context, repoIdentifier string, sourceType schema.SourceType, branch string, prNumber int, prID, prTitle, prURL string) error {
	if h.PreviewSvc == nil {
		return c.JSON(http.StatusOK, map[string]string{"message": "preview service not available"})
	}

	var apps []schema.Application
	var query string

	switch sourceType {
	case schema.SourceTypeGitlab:
		query = "\"gitlabRepository\" = ? AND \"sourceType\" = ? AND \"isPreviewDeploymentsActive\" = ?"
	case schema.SourceTypeBitbucket:
		query = "\"bitbucketRepository\" = ? AND \"sourceType\" = ? AND \"isPreviewDeploymentsActive\" = ?"
	case schema.SourceTypeGitea:
		query = "\"giteaRepository\" = ? AND \"sourceType\" = ? AND \"isPreviewDeploymentsActive\" = ?"
	default:
		return c.JSON(http.StatusOK, map[string]string{"message": "unsupported source type for preview"})
	}

	h.DB.Where(query, repoIdentifier, sourceType, true).Find(&apps)

	created := 0
	for _, app := range apps {
		if app.PreviewWildcard == nil || *app.PreviewWildcard == "" {
			continue
		}
		preview, err := h.PreviewSvc.CreatePreviewDeployment(service.CreatePreviewInput{
			ApplicationID:     app.ApplicationID,
			Branch:            branch,
			PullRequestID:     prID,
			PullRequestNumber: fmt.Sprintf("%d", prNumber),
			PullRequestURL:    prURL,
			PullRequestTitle:  prTitle,
		})
		if err != nil {
			log.Printf("Failed to create preview deployment for app %s: %v", app.ApplicationID, err)
			continue
		}
		if h.Queue != nil {
			title := fmt.Sprintf("Preview PR #%d", prNumber)
			h.Queue.EnqueueDeployApplication(preview.ApplicationID, &title, nil)
		}
		created++
	}

	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("created %d preview deployments", created)})
}
