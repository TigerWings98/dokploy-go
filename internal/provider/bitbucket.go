package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BitbucketClient provides Bitbucket API operations.
type BitbucketClient struct {
	username    string
	appPassword string
	httpClient  *http.Client
}

// NewBitbucketClient creates a new Bitbucket client using app password auth.
func NewBitbucketClient(username, appPassword string) *BitbucketClient {
	return &BitbucketClient{
		username:    username,
		appPassword: appPassword,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// BitbucketRepository represents a Bitbucket repository.
type BitbucketRepository struct {
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Slug     string `json:"slug"`
	IsPrivate bool  `json:"is_private"`
	Links    struct {
		Clone []struct {
			Href string `json:"href"`
			Name string `json:"name"`
		} `json:"clone"`
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
	MainBranch struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
}

// BitbucketBranch represents a Bitbucket branch.
type BitbucketBranch struct {
	Name   string `json:"name"`
	Target struct {
		Hash string `json:"hash"`
	} `json:"target"`
}

// ListRepositories lists repositories for a workspace.
func (c *BitbucketClient) ListRepositories(workspace string) ([]BitbucketRepository, error) {
	url := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s", workspace)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.appPassword)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bitbucket API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Values []BitbucketRepository `json:"values"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Values, nil
}

// ListBranches lists branches for a repository.
func (c *BitbucketClient) ListBranches(workspace, repoSlug string) ([]BitbucketBranch, error) {
	url := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s/refs/branches", workspace, repoSlug)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.appPassword)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Values []BitbucketBranch `json:"values"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Values, nil
}
