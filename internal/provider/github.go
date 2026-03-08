// Input: GitHub REST API (api.github.com), access token
// Output: GitHubProvider (ListRepos/ListBranches/GetRepository)
// Role: GitHub API 客户端，查询用户仓库列表和分支信息
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GithubClient provides GitHub API operations.
type GithubClient struct {
	installationID string
	privateKey     string
	appID          int
	httpClient     *http.Client
}

// NewGithubClient creates a new GitHub client.
func NewGithubClient(appID int, installationID, privateKey string) *GithubClient {
	return &GithubClient{
		installationID: installationID,
		privateKey:     privateKey,
		appID:          appID,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// GithubRepository represents a GitHub repository.
type GithubRepository struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	HTMLURL       string `json:"html_url"`
}

// GithubBranch represents a GitHub branch.
type GithubBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// ListRepositories lists repositories accessible by the GitHub App installation.
func (c *GithubClient) ListRepositories(token string) ([]GithubRepository, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/installation/repositories", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Repositories []GithubRepository `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Repositories, nil
}

// ListBranches lists branches for a repository.
func (c *GithubClient) ListBranches(token, owner, repo string) ([]GithubBranch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches", owner, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var branches []GithubBranch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, err
	}

	return branches, nil
}

// GithubWebhookPayload represents a GitHub webhook push event.
type GithubWebhookPayload struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Installation struct {
		ID int `json:"id"`
	} `json:"installation"`
}

// GithubPRPayload represents a GitHub webhook pull request event.
type GithubPRPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Title  string `json:"title"`
		Number int    `json:"number"`
		Head   struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		HTMLURL string `json:"html_url"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int `json:"id"`
	} `json:"installation"`
}
