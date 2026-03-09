// Input: DB (go_config 表), Docker daemon (service update), Registry HTTP API v2
// Output: Updater (CheckUpdate/ApplyUpdate/GetConfig/UpdateConfig)，提供版本检测、滚动更新和更新源配置
// Role: 自更新模块，从 DB 读取 Registry 配置，查询远端最新 semver tag，执行 docker service update
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package updater

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
)

// Version 是构建时通过 ldflags 注入的版本号
// go build -ldflags "-X github.com/dokploy/dokploy/internal/updater.Version=28.0.2"
var Version = "0.0.0-dev"

// UpdateData 是返回给前端的更新信息
type UpdateData struct {
	UpdateAvailable bool    `json:"updateAvailable"`
	LatestVersion   *string `json:"latestVersion"`
}

// Updater 管理版本检测和自更新
type Updater struct {
	db *db.DB
	mu sync.RWMutex
	// 缓存配置，避免每次都查 DB
	cached *schema.GoConfig
}

// New 创建 Updater 实例
func New(database *db.DB) *Updater {
	u := &Updater{db: database}
	u.reloadConfig()
	return u
}

// GetVersion 返回当前构建版本
func (u *Updater) GetVersion() string {
	return Version
}

// GetReleaseTag 返回当前发布通道 tag（即构建版本）
func (u *Updater) GetReleaseTag() string {
	return Version
}

// GetConfig 返回当前更新源配置（前端读取用）
func (u *Updater) GetConfig() schema.GoConfig {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.cached != nil {
		return *u.cached
	}
	return schema.GoConfig{}
}

// UpdateConfig 更新更新源配置（前端设置用）
func (u *Updater) UpdateConfig(cfg schema.GoConfig) error {
	updates := map[string]interface{}{
		"registry_image": cfg.RegistryImage,
		"registry_id":    cfg.RegistryID,
		"service_name":   cfg.ServiceName,
		"updated_at":     time.Now().UTC(),
	}
	if err := u.db.Model(&schema.GoConfig{}).Where("id = ?", "default").Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update go_config: %w", err)
	}
	u.reloadConfig()
	return nil
}

// reloadConfig 从 DB 重新加载配置到缓存（Preload Registry 以获取认证信息）
func (u *Updater) reloadConfig() {
	var cfg schema.GoConfig
	if err := u.db.Preload("Registry").First(&cfg, "id = ?", "default").Error; err != nil {
		log.Printf("[Updater] Failed to load go_config: %v", err)
		return
	}
	u.mu.Lock()
	u.cached = &cfg
	u.mu.Unlock()
}

// getImage 获取当前配置的镜像名（优先 DB，其次环境变量，未配置则返回空）
func (u *Updater) getImage() string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.cached != nil && u.cached.RegistryImage != "" {
		return u.cached.RegistryImage
	}
	if env := os.Getenv("DOKPLOY_IMAGE"); env != "" {
		return env
	}
	// 不再 fallback 到官方 TS 版镜像，未配置时返回空，CheckUpdate 会直接返回"无更新"
	return ""
}

// getServiceName 获取 Docker service 名称
func (u *Updater) getServiceName() string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.cached != nil && u.cached.ServiceName != "" {
		return u.cached.ServiceName
	}
	if env := os.Getenv("DOKPLOY_SERVICE_NAME"); env != "" {
		return env
	}
	return "dokploy"
}

// getRegistryAuth 获取 Registry 认证信息（从关联的 Registry 记录读取）
func (u *Updater) getRegistryAuth() (user, password string) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.cached != nil && u.cached.Registry != nil {
		user = u.cached.Registry.Username
		password = u.cached.Registry.Password
	}
	// 环境变量作为 fallback
	if user == "" {
		user = os.Getenv("REGISTRY_USER")
	}
	if password == "" {
		password = os.Getenv("REGISTRY_PASSWORD")
	}
	return
}

// CheckUpdate 检查是否有新版本可用
func (u *Updater) CheckUpdate() UpdateData {
	noUpdate := UpdateData{UpdateAvailable: false, LatestVersion: nil}

	image := u.getImage()
	if image == "" {
		return noUpdate
	}

	tags, err := u.listRegistryTags(image)
	if err != nil {
		log.Printf("[Updater] Failed to list registry tags: %v", err)
		return noUpdate
	}

	// 筛选出所有 semver tags
	var semverTags []string
	for _, tag := range tags {
		if isValidSemver(tag) {
			semverTags = append(semverTags, tag)
		}
	}
	if len(semverTags) == 0 {
		return noUpdate
	}

	// 排序找最新版本
	sort.Slice(semverTags, func(i, j int) bool {
		return compareSemver(semverTags[i], semverTags[j]) > 0
	})
	latest := semverTags[0]

	// 与当前版本比较
	if compareSemver(latest, Version) <= 0 {
		return noUpdate
	}

	return UpdateData{
		UpdateAvailable: true,
		LatestVersion:   &latest,
	}
}

// ApplyUpdate 执行更新：docker service update --force --image {image}:{version} {service}
func (u *Updater) ApplyUpdate(version string) error {
	if version == "" {
		data := u.CheckUpdate()
		if !data.UpdateAvailable || data.LatestVersion == nil {
			return fmt.Errorf("no update available")
		}
		version = *data.LatestVersion
	}

	image := u.getImage()
	serviceName := u.getServiceName()
	imageRef := fmt.Sprintf("%s:%s", image, version)
	log.Printf("[Updater] Applying update: docker service update --force --image %s %s", imageRef, serviceName)

	cmd := exec.Command("docker", "service", "update", "--force", "--image", imageRef, serviceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 异步执行，与 TS 版的 spawn 行为一致
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start update: %w", err)
	}

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("[Updater] Update command finished with error: %v", err)
		}
	}()

	return nil
}

// ReloadService 强制重启服务（不更换镜像）
func (u *Updater) ReloadService() error {
	serviceName := u.getServiceName()
	log.Printf("[Updater] Reloading service: docker service update --force %s", serviceName)
	cmd := exec.Command("docker", "service", "update", "--force", serviceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to reload service: %w", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("[Updater] Reload command finished with error: %v", err)
		}
	}()
	return nil
}

// --- Registry API ---

// listRegistryTags 通过 Registry HTTP API v2 获取所有 tags
func (u *Updater) listRegistryTags(image string) ([]string, error) {
	registry, repo := parseImageRef(image)

	if registry == "docker.io" || registry == "" {
		return u.listDockerHubTags(repo)
	}

	return u.listV2Tags(registry, repo)
}

// listDockerHubTags 从 Docker Hub API 获取 tags
func (u *Updater) listDockerHubTags(repo string) ([]string, error) {
	apiURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", repo)
	client := &http.Client{Timeout: 15 * time.Second}

	var allTags []string
	for apiURL != "" {
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return nil, err
		}

		if token := os.Getenv("DOCKERHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("docker hub API request failed: %w", err)
		}
		var data struct {
			Next    *string `json:"next"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()

		for _, r := range data.Results {
			allTags = append(allTags, r.Name)
		}
		if data.Next != nil {
			apiURL = *data.Next
		} else {
			apiURL = ""
		}
	}
	return allTags, nil
}

// listV2Tags 通过 Registry v2 API 获取 tags（GHCR/Harbor/ECR/ACR 等）
func (u *Updater) listV2Tags(registry, repo string) ([]string, error) {
	token, err := u.getRegistryToken(registry, repo)
	if err != nil {
		log.Printf("[Updater] Warning: failed to get registry token: %v", err)
	}

	apiURL := fmt.Sprintf("https://%s/v2/%s/tags/list", registry, repo)
	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry API returned %d", resp.StatusCode)
	}

	var data struct {
		Tags []string `json:"tags"`
	}
	json.NewDecoder(resp.Body).Decode(&data)
	return data.Tags, nil
}

// getRegistryToken 通过 WWW-Authenticate 握手获取 Registry token
func (u *Updater) getRegistryToken(registry, repo string) (string, error) {
	// 环境变量直接传入 token
	if token := os.Getenv("REGISTRY_TOKEN"); token != "" {
		return token, nil
	}

	// WWW-Authenticate 握手
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://%s/v2/", registry))
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		authHeader := resp.Header.Get("Www-Authenticate")
		if authHeader != "" {
			return u.exchangeToken(authHeader, repo)
		}
	}

	return "", nil
}

// exchangeToken 解析 Www-Authenticate header 并交换 Bearer token
func (u *Updater) exchangeToken(authHeader, repo string) (string, error) {
	params := parseWWWAuthenticate(authHeader)
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("no realm in WWW-Authenticate header")
	}

	tokenURL := fmt.Sprintf("%s?service=%s&scope=repository:%s:pull",
		realm, params["service"], repo)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", err
	}

	// 用 DB 配置的凭证做 Basic Auth
	user, password := u.getRegistryAuth()
	if user != "" {
		req.SetBasicAuth(user, password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&data)

	if data.Token != "" {
		return data.Token, nil
	}
	return data.AccessToken, nil
}

// --- 工具函数 ---

func parseWWWAuthenticate(header string) map[string]string {
	params := make(map[string]string)
	header = strings.TrimPrefix(header, "Bearer ")
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = strings.Trim(kv[1], "\"")
		}
	}
	return params
}

// parseImageRef 解析镜像名为 registry + repo
// "ghcr.io/org/name" → ("ghcr.io", "org/name")
// "crpi-xxx.cn-shanghai.personal.cr.aliyuncs.com/tigerking/dokploy-go" → ("crpi-xxx...", "tigerking/dokploy-go")
// "dokploy/dokploy" → ("docker.io", "dokploy/dokploy")
func parseImageRef(image string) (registry, repo string) {
	parts := strings.SplitN(image, "/", 2)
	if len(parts) == 1 {
		return "docker.io", "library/" + parts[0]
	}
	if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
		return parts[0], parts[1]
	}
	return "docker.io", image
}

// isValidSemver 检查是否是合法的 semver tag（MAJOR.MINOR.PATCH 或 vMAJOR.MINOR.PATCH）
func isValidSemver(tag string) bool {
	v := strings.TrimPrefix(tag, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		numStr := strings.SplitN(p, "-", 2)[0]
		if _, err := strconv.Atoi(numStr); err != nil {
			return false
		}
	}
	return true
}

// compareSemver 比较两个 semver 版本
func compareSemver(a, b string) int {
	aParts := parseSemverParts(a)
	bParts := parseSemverParts(b)
	for i := 0; i < 3; i++ {
		if aParts[i] != bParts[i] {
			return aParts[i] - bParts[i]
		}
	}
	return 0
}

func parseSemverParts(version string) [3]int {
	v := strings.TrimPrefix(version, "v")
	parts := strings.Split(v, ".")
	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		numStr := strings.SplitN(parts[i], "-", 2)[0]
		result[i], _ = strconv.Atoi(numStr)
	}
	return result
}
