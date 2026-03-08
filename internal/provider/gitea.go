// Input: Gitea REST API (自定义 apiUrl), access token
// Output: GiteaProvider (ListRepos/ListBranches/GetRepository)
// Role: Gitea API 客户端，支持自托管实例，查询用户仓库和分支信息
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GiteaClient provides Gitea API operations.
type GiteaClient struct {
	accessToken string
	baseURL     string
	httpClient  *http.Client
}

// NewGiteaClient creates a new Gitea client.
func NewGiteaClient(accessToken, baseURL string) *GiteaClient {
	return &GiteaClient{
		accessToken: accessToken,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// GiteaRepository represents a Gitea repository.
type GiteaRepository struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	HTMLURL       string `json:"html_url"`
	SSHURL        string `json:"ssh_url"`
}

// GiteaBranch represents a Gitea branch.
type GiteaBranch struct {
	Name string `json:"name"`
}

// ListRepositories lists repositories accessible by the Gitea token.
func (c *GiteaClient) ListRepositories(page, limit int) ([]GiteaRepository, error) {
	url := fmt.Sprintf("%s/api/v1/user/repos?page=%d&limit=%d", c.baseURL, page, limit)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitea API error %d: %s", resp.StatusCode, string(body))
	}

	var repos []GiteaRepository
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, err
	}

	return repos, nil
}

// ListBranches lists branches for a repository.
func (c *GiteaClient) ListBranches(owner, repo string) ([]GiteaBranch, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches", c.baseURL, owner, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var branches []GiteaBranch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, err
	}

	return branches, nil
}

// GiteaWebhookPayload represents a Gitea webhook push event.
type GiteaWebhookPayload struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Repository struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	Pusher struct {
		Login string `json:"login"`
	} `json:"pusher"`
}
