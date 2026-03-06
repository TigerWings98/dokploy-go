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
				Head struct {
					Ref string `json:"ref"`
				} `json:"head"`
			} `json:"pull_request"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
		}

		if payload.Action == "opened" || payload.Action == "synchronize" {
			return h.handleGithubPR(c, payload.Repository.FullName, payload.PullRequest.Head.Ref, payload.Number)
		}

		return c.JSON(http.StatusOK, map[string]string{"message": "ignored action"})

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

func (h *Handler) handleGithubPR(c echo.Context, repoFullName, headBranch string, prNumber int) error {
	var apps []schema.Application
	err := h.DB.
		Where("\"repository\" = ? AND \"sourceType\" = ? AND \"previewWildcard\" IS NOT NULL",
			repoFullName, schema.SourceTypeGithub).
		Find(&apps).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Preview deployments: enqueue deploy for each matching app with PR branch
	if h.Queue != nil {
		for _, app := range apps {
			title := fmt.Sprintf("Preview PR #%d", prNumber)
			_, err := h.Queue.EnqueueDeployApplication(app.ApplicationID, &title, nil)
			if err != nil {
				log.Printf("Failed to enqueue preview deploy for app %s: %v", app.ApplicationID, err)
			}
		}
	}
	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("processed %d preview apps", len(apps))})
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

	if event != "Push Hook" && event != "Tag Push Hook" {
		return c.JSON(http.StatusOK, map[string]string{"message": "event not handled"})
	}

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
}

// BitbucketWebhook handles Bitbucket webhook events.
func (h *Handler) BitbucketWebhook(c echo.Context) error {
	event := c.Request().Header.Get("X-Event-Key")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read body")
	}

	if event != "repo:push" {
		return c.JSON(http.StatusOK, map[string]string{"message": "event not handled"})
	}

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
}

// GiteaWebhook handles Gitea webhook events.
func (h *Handler) GiteaWebhook(c echo.Context) error {
	event := c.Request().Header.Get("X-Gitea-Event")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read body")
	}

	if event != "push" {
		return c.JSON(http.StatusOK, map[string]string{"message": "event not handled"})
	}

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

	var apps []schema.Application
	h.DB.
		Where("\"giteaRepository\" = ? AND \"giteaBranch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
			payload.Repository.FullName, branch, schema.SourceTypeGitea, true).
		Find(&apps)

	var composes []schema.Compose
	h.DB.
		Where("\"repository\" = ? AND \"branch\" = ? AND \"sourceType\" = ? AND \"autoDeploy\" = ?",
			payload.Repository.FullName, branch, schema.SourceTypeComposeGitea, true).
		Find(&composes)

	h.enqueueWebhookDeploys(apps, composes)
	total := len(apps) + len(composes)
	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("processed %d services", total)})
}
