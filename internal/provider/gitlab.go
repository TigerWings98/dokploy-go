// Input: GitLab REST API (自定义 apiUrl), access token
// Output: GitLabProvider (ListRepos/ListBranches/GetRepository)
// Role: GitLab API 客户端，支持自托管实例，查询用户仓库和分支信息
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GitlabClient provides GitLab API operations.
type GitlabClient struct {
	accessToken string
	baseURL     string
	httpClient  *http.Client
}

// NewGitlabClient creates a new GitLab client.
func NewGitlabClient(accessToken, baseURL string) *GitlabClient {
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	return &GitlabClient{
		accessToken: accessToken,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// GitlabProject represents a GitLab project/repository.
type GitlabProject struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
	HTTPURLToRepo     string `json:"http_url_to_repo"`
	SSHURLToRepo      string `json:"ssh_url_to_repo"`
	Visibility        string `json:"visibility"`
}

// GitlabBranch represents a GitLab branch.
type GitlabBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Default   bool   `json:"default"`
}

// ListProjects lists projects accessible by the GitLab token.
func (c *GitlabClient) ListProjects(page, perPage int) ([]GitlabProject, error) {
	url := fmt.Sprintf("%s/api/v4/projects?membership=true&page=%d&per_page=%d", c.baseURL, page, perPage)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab API error %d: %s", resp.StatusCode, string(body))
	}

	var projects []GitlabProject
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, err
	}

	return projects, nil
}

// ListBranches lists branches for a project.
func (c *GitlabClient) ListBranches(projectID int) ([]GitlabBranch, error) {
	url := fmt.Sprintf("%s/api/v4/projects/%d/branches", c.baseURL, projectID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var branches []GitlabBranch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, err
	}

	return branches, nil
}

// GitlabWebhookPayload represents a GitLab webhook push event.
type GitlabWebhookPayload struct {
	ObjectKind string `json:"object_kind"`
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Project    struct {
		ID                int    `json:"id"`
		Name              string `json:"name"`
		PathWithNamespace string `json:"path_with_namespace"`
		WebURL            string `json:"web_url"`
	} `json:"project"`
	UserUsername string `json:"user_username"`
}
