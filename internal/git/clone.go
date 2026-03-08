// Input: Git 提供商配置 (GitHub/GitLab/Gitea/Bitbucket token + SSH key), 仓库信息
// Output: CloneRepository (带认证的 git clone), CloneCustomGit (自定义 Git URL + SSH key 克隆)
// Role: 带认证的 Git 克隆引擎，根据 SourceType 选择 HTTPS token 或 SSH key 认证方式
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package git

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
)

// CloneOptions configures a git clone operation.
type CloneOptions struct {
	RepoURL          string
	Branch           string
	OutputPath       string
	EnableSubmodules bool
	WriteLog         func(string)
}

// CloneResult holds the result of a clone operation.
type CloneResult struct {
	CommitHash    string
	CommitMessage string
}

// CloneWithAuth clones a repository using provider-specific authentication.
func CloneWithAuth(app *schema.Application, outputPath string, writeLog func(string)) (*CloneResult, error) {
	// Remove existing code directory
	os.RemoveAll(outputPath)

	switch app.SourceType {
	case schema.SourceTypeGithub:
		return cloneGithub(app, outputPath, writeLog)
	case schema.SourceTypeGitlab:
		return cloneGitlab(app, outputPath, writeLog)
	case schema.SourceTypeBitbucket:
		return cloneBitbucket(app, outputPath, writeLog)
	case schema.SourceTypeGitea:
		return cloneGitea(app, outputPath, writeLog)
	case schema.SourceTypeGit:
		return cloneCustomGit(app, outputPath, writeLog)
	case schema.SourceTypeDocker:
		writeLog("Using Docker image directly, no source to clone")
		return nil, nil
	case schema.SourceTypeDrop:
		writeLog("Using uploaded source (drop)")
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", app.SourceType)
	}
}

func cloneGithub(app *schema.Application, outputPath string, writeLog func(string)) (*CloneResult, error) {
	if app.Repository == nil || app.Owner == nil || app.Branch == nil {
		return nil, fmt.Errorf("github source requires repository, owner, and branch")
	}

	// Get installation token from the associated Github record
	token, err := getGithubInstallationToken(app)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub token: %w", err)
	}

	var repoURL string
	if token != "" {
		repoURL = fmt.Sprintf("https://oauth2:%s@github.com/%s/%s.git", token, *app.Owner, *app.Repository)
	} else {
		repoURL = fmt.Sprintf("https://github.com/%s/%s.git", *app.Owner, *app.Repository)
	}

	return cloneRepo(CloneOptions{
		RepoURL:          repoURL,
		Branch:           *app.Branch,
		OutputPath:       outputPath,
		EnableSubmodules: app.EnableSubmodules,
		WriteLog:         writeLog,
	})
}

func cloneGitlab(app *schema.Application, outputPath string, writeLog func(string)) (*CloneResult, error) {
	if app.GitlabRepository == nil || app.GitlabBranch == nil {
		return nil, fmt.Errorf("gitlab source requires repository and branch")
	}

	repoURL := *app.GitlabRepository

	// Inject OAuth token if available from associated Gitlab record
	if app.Gitlab != nil && app.Gitlab.AccessToken != nil && *app.Gitlab.AccessToken != "" {
		repoURL = injectOAuth2Token(repoURL, *app.Gitlab.AccessToken)
	}

	return cloneRepo(CloneOptions{
		RepoURL:          repoURL,
		Branch:           *app.GitlabBranch,
		OutputPath:       outputPath,
		EnableSubmodules: app.EnableSubmodules,
		WriteLog:         writeLog,
	})
}

func cloneBitbucket(app *schema.Application, outputPath string, writeLog func(string)) (*CloneResult, error) {
	if app.BitbucketOwner == nil || app.BitbucketRepository == nil || app.BitbucketBranch == nil {
		return nil, fmt.Errorf("bitbucket source requires owner, repository, and branch")
	}

	repoURL := fmt.Sprintf("https://bitbucket.org/%s/%s.git", *app.BitbucketOwner, *app.BitbucketRepository)

	// Inject credentials from associated Bitbucket record
	if app.Bitbucket != nil {
		if app.Bitbucket.BitbucketUsername != nil && app.Bitbucket.AppPassword != nil {
			repoURL = fmt.Sprintf("https://%s:%s@bitbucket.org/%s/%s.git",
				url.PathEscape(*app.Bitbucket.BitbucketUsername),
				url.PathEscape(*app.Bitbucket.AppPassword),
				*app.BitbucketOwner, *app.BitbucketRepository)
		}
	}

	return cloneRepo(CloneOptions{
		RepoURL:          repoURL,
		Branch:           *app.BitbucketBranch,
		OutputPath:       outputPath,
		EnableSubmodules: app.EnableSubmodules,
		WriteLog:         writeLog,
	})
}

func cloneGitea(app *schema.Application, outputPath string, writeLog func(string)) (*CloneResult, error) {
	if app.GiteaRepository == nil || app.GiteaBranch == nil {
		return nil, fmt.Errorf("gitea source requires repository and branch")
	}

	repoURL := *app.GiteaRepository

	// Inject OAuth token if available from associated Gitea record
	if app.Gitea != nil && app.Gitea.AccessToken != nil && *app.Gitea.AccessToken != "" {
		repoURL = injectOAuth2Token(repoURL, *app.Gitea.AccessToken)
	}

	return cloneRepo(CloneOptions{
		RepoURL:          repoURL,
		Branch:           *app.GiteaBranch,
		OutputPath:       outputPath,
		EnableSubmodules: app.EnableSubmodules,
		WriteLog:         writeLog,
	})
}

func cloneCustomGit(app *schema.Application, outputPath string, writeLog func(string)) (*CloneResult, error) {
	if app.CustomGitURL == nil || app.CustomGitBranch == nil {
		return nil, fmt.Errorf("custom git source requires URL and branch")
	}

	repoURL := *app.CustomGitURL

	// Check if SSH key is associated
	if app.CustomGitSSHKey != nil && app.CustomGitSSHKey.PrivateKey != "" {
		return cloneWithSSHKey(repoURL, *app.CustomGitBranch, outputPath, app.CustomGitSSHKey.PrivateKey, app.EnableSubmodules, writeLog)
	}

	return cloneRepo(CloneOptions{
		RepoURL:          repoURL,
		Branch:           *app.CustomGitBranch,
		OutputPath:       outputPath,
		EnableSubmodules: app.EnableSubmodules,
		WriteLog:         writeLog,
	})
}

// cloneRepo performs the actual git clone and extracts commit info.
func cloneRepo(opts CloneOptions) (*CloneResult, error) {
	opts.WriteLog(fmt.Sprintf("Cloning %s (branch: %s)", sanitizeURLForLog(opts.RepoURL), opts.Branch))

	cmd := fmt.Sprintf("git clone --branch %s --depth 1", opts.Branch)
	if opts.EnableSubmodules {
		cmd += " --recurse-submodules"
	}
	cmd += fmt.Sprintf(" --progress %s %s", opts.RepoURL, opts.OutputPath)

	if _, err := process.ExecAsyncStream(cmd, opts.WriteLog); err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}

	return extractCommitInfo(opts.OutputPath)
}

// cloneWithSSHKey clones using a custom SSH private key.
func cloneWithSSHKey(repoURL, branch, outputPath, privateKey string, enableSubmodules bool, writeLog func(string)) (*CloneResult, error) {
	// Write SSH key to temp file
	keyFile := "/tmp/dokploy_id_rsa"
	if err := os.WriteFile(keyFile, []byte(privateKey), 0600); err != nil {
		return nil, fmt.Errorf("failed to write SSH key: %w", err)
	}
	defer os.Remove(keyFile)

	// Parse SSH URL for port
	port := parseSSHPort(repoURL)

	// Add host to known_hosts
	host := parseSSHHost(repoURL)
	if host != "" {
		scanCmd := fmt.Sprintf("ssh-keyscan -p %d %s >> ~/.ssh/known_hosts 2>/dev/null", port, host)
		process.ExecAsync(scanCmd)
	}

	writeLog(fmt.Sprintf("Cloning %s via SSH (branch: %s)", sanitizeURLForLog(repoURL), branch))

	sshCmd := fmt.Sprintf("ssh -i %s -p %d -o StrictHostKeyChecking=accept-new", keyFile, port)

	cmd := fmt.Sprintf("GIT_SSH_COMMAND='%s' git clone --branch %s --depth 1", sshCmd, branch)
	if enableSubmodules {
		cmd += " --recurse-submodules"
	}
	cmd += fmt.Sprintf(" --progress %s %s", repoURL, outputPath)

	if _, err := process.ExecAsyncStream(cmd, writeLog); err != nil {
		return nil, fmt.Errorf("git clone (SSH) failed: %w", err)
	}

	return extractCommitInfo(outputPath)
}

// extractCommitInfo gets the latest commit hash and message from a cloned repo.
func extractCommitInfo(repoPath string) (*CloneResult, error) {
	result, err := process.ExecAsync(
		fmt.Sprintf("git -C %s log -1 --pretty=format:\"%%H---DELIMITER---%%B\"", repoPath),
	)
	if err != nil {
		return &CloneResult{}, nil // non-fatal
	}

	parts := strings.SplitN(result.Stdout, "---DELIMITER---", 2)
	cr := &CloneResult{}
	if len(parts) >= 1 {
		cr.CommitHash = strings.TrimSpace(strings.Trim(parts[0], "\""))
	}
	if len(parts) >= 2 {
		cr.CommitMessage = strings.TrimSpace(strings.Trim(parts[1], "\""))
	}
	return cr, nil
}

// injectOAuth2Token injects oauth2:{token}@ into an HTTP(S) URL.
func injectOAuth2Token(repoURL, token string) string {
	if strings.HasPrefix(repoURL, "https://") {
		return "https://oauth2:" + token + "@" + strings.TrimPrefix(repoURL, "https://")
	}
	if strings.HasPrefix(repoURL, "http://") {
		return "http://oauth2:" + token + "@" + strings.TrimPrefix(repoURL, "http://")
	}
	return repoURL
}

// getGithubInstallationToken retrieves a GitHub App installation access token.
// This requires the app to have githubAppId, githubPrivateKey, githubInstallationId.
func getGithubInstallationToken(app *schema.Application) (string, error) {
	if app.Github == nil {
		return "", nil
	}

	gh := app.Github
	if gh.GithubInstallationID == nil || gh.GithubPrivateKey == nil || gh.GithubAppID == nil {
		return "", nil
	}

	// Use the GitHub Apps API to get an installation access token
	// POST /app/installations/{installation_id}/access_tokens
	// Requires JWT signed with the app's private key
	cmd := fmt.Sprintf(
		`curl -s -X POST -H "Accept: application/vnd.github+json" `+
			`-H "Authorization: Bearer $(echo '{"iat":'$(date +%%s)',"exp":'$(($(date +%%s)+600))',"iss":%d}' | `+
			`openssl dgst -sha256 -sign <(echo '%s') -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')" `+
			`"https://api.github.com/app/installations/%s/access_tokens" | `+
			`grep -o '"token":"[^"]*"' | cut -d'"' -f4`,
		*gh.GithubAppID,
		strings.ReplaceAll(*gh.GithubPrivateKey, "'", "'\\''"),
		*gh.GithubInstallationID,
	)

	result, err := process.ExecAsync(cmd)
	if err != nil || result == nil || strings.TrimSpace(result.Stdout) == "" {
		// Fallback: try without token (public repos)
		return "", nil
	}

	return strings.TrimSpace(result.Stdout), nil
}

// sanitizeURLForLog removes credentials from a URL for safe logging.
func sanitizeURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		// For SSH URLs, just return as-is
		return rawURL
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	return u.String()
}

var sshHostRegex = regexp.MustCompile(`(?:ssh://)?(?:[^@]+@)?([^:/]+)`)

// parseSSHHost extracts the hostname from an SSH URL.
func parseSSHHost(sshURL string) string {
	m := sshHostRegex.FindStringSubmatch(sshURL)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

var sshPortRegex = regexp.MustCompile(`(?:ssh://[^@]+@[^:]+:(\d+))|(?::(\d+)/)`)

// parseSSHPort extracts the port from an SSH URL (defaults to 22).
func parseSSHPort(sshURL string) int {
	m := sshPortRegex.FindStringSubmatch(sshURL)
	if len(m) >= 2 && m[1] != "" {
		port := 0
		fmt.Sscanf(m[1], "%d", &port)
		if port > 0 {
			return port
		}
	}
	if len(m) >= 3 && m[2] != "" {
		port := 0
		fmt.Sscanf(m[2], "%d", &port)
		if port > 0 {
			return port
		}
	}
	return 22
}
