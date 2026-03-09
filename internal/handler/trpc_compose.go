// Input: procedureRegistry, db (Compose 表), docker, traefik, compose/transform
// Output: registerComposeTRPC - Compose 领域的 tRPC procedure 注册
// Role: Compose tRPC 路由注册，将 compose.* procedure 绑定到 Compose 服务管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

func (h *Handler) registerComposeTRPC(r procedureRegistry) {
	r["compose.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		var compose schema.Compose
		err := h.DB.
			Preload("Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC").Limit(10)
			}).
			Preload("Domains").
			Preload("Mounts").
			Preload("Server").
			Preload("Github").
			Preload("Github.GitProvider").
			Preload("Gitlab").
			Preload("Gitlab.GitProvider").
			Preload("Gitea").
			Preload("Gitea.GitProvider").
			Preload("Bitbucket").
			Preload("Bitbucket.GitProvider").
			Preload("Backups").
			Preload("Backups.Destination").
			Preload("Backups.Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC")
			}).
			First(&compose, "\"composeId\" = ?", in.ComposeID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
			}
			return nil, err
		}
		// Ensure slices are never null in JSON
		if compose.Deployments == nil { compose.Deployments = []schema.Deployment{} }
		if compose.Domains == nil { compose.Domains = []schema.Domain{} }
		if compose.Mounts == nil { compose.Mounts = []schema.Mount{} }
		if compose.Security == nil { compose.Security = []schema.Security{} }
		if compose.Redirects == nil { compose.Redirects = []schema.Redirect{} }
		if compose.Backups == nil { compose.Backups = []schema.Backup{} }

		if compose.EnvironmentID != "" {
			var env schema.Environment
			if err := h.DB.First(&env, "\"environmentId\" = ?", compose.EnvironmentID).Error; err == nil {
				if env.ProjectID != "" {
					var proj schema.Project
					if err := h.DB.First(&proj, "\"projectId\" = ?", env.ProjectID).Error; err == nil {
						env.Project = &proj
					}
				}
				compose.Environment = &env
			}
		}
		return compose, nil
	}

	r["compose.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Name          string  `json:"name"`
			Description   *string `json:"description"`
			EnvironmentID string  `json:"environmentId"`
			ServerID      *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		compose := &schema.Compose{
			Name:          in.Name,
			Description:   in.Description,
			EnvironmentID: in.EnvironmentID,
			ServerID:      in.ServerID,
		}
		if err := h.DB.Create(compose).Error; err != nil {
			return nil, err
		}
		return compose, nil
	}

	r["compose.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		composeID, _ := in["composeId"].(string)
		delete(in, "composeId")
		var compose schema.Compose
		if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		if err := h.DB.Model(&compose).Updates(in).Error; err != nil {
			return nil, err
		}
		return compose, nil
	}

	r["compose.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)

		var comp schema.Compose
		if err := h.DB.Preload("Server").Preload("Server.SSHKey").
			First(&comp, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}

		// 1. Stop and remove compose containers
		composeDir := ""
		if h.Config != nil {
			composeDir = filepath.Join(h.Config.Paths.ComposePath, comp.AppName)
		}
		stopCmd := fmt.Sprintf("docker compose -p %s down", comp.AppName)
		if comp.ServerID != nil && comp.Server != nil && comp.Server.SSHKey != nil {
			conn := process.SSHConnection{
				Host:       comp.Server.IPAddress,
				Port:       comp.Server.Port,
				Username:   comp.Server.Username,
				PrivateKey: comp.Server.SSHKey.PrivateKey,
			}
			process.ExecAsyncRemote(conn, stopCmd, nil)
		} else if composeDir != "" {
			process.ExecAsyncStream(stopCmd, nil, process.WithDir(composeDir))
		}

		// 2. Remove Traefik config
		if h.Traefik != nil {
			h.Traefik.RemoveApplicationConfig(comp.AppName)
		}

		// 3. Remove source code and log directories
		if h.Config != nil {
			os.RemoveAll(filepath.Join(h.Config.Paths.ComposePath, comp.AppName))
			os.RemoveAll(filepath.Join(h.Config.Paths.LogsPath, comp.AppName))
		}

		// 4. Delete from database
		if err := h.DB.Delete(&schema.Compose{}, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, err
		}
		return true, nil
	}

	r["compose.deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string  `json:"composeId"`
			Title     *string `json:"title"`
		}
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			info, err := h.Queue.EnqueueDeployCompose(in.ComposeID, in.Title)
			if err != nil {
				return nil, err
			}
			return map[string]string{"message": "Deployment queued", "taskId": info.ID}, nil
		}
		return true, nil
	}

	// redeploy = rebuild（仅 compose up，不重新 clone 代码）
	r["compose.redeploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string  `json:"composeId"`
			Title     *string `json:"title"`
		}
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			info, err := h.Queue.EnqueueRebuildCompose(in.ComposeID, in.Title)
			if err != nil {
				return nil, err
			}
			return map[string]string{"message": "Rebuild queued", "taskId": info.ID}, nil
		}
		return true, nil
	}

	// compose.start: 同步执行 docker compose up -d（不 rebuild），与 TS 版一致
	r["compose.start"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		if h.ComposeSvc == nil {
			return nil, &trpcErr{"Compose service not available", "INTERNAL_SERVER_ERROR", 500}
		}
		if err := h.ComposeSvc.Start(in.ComposeID); err != nil {
			return nil, err
		}
		return true, nil
	}

	// compose.stop: 同步执行 docker compose stop，与 TS 版一致
	r["compose.stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		if h.ComposeSvc == nil {
			return nil, &trpcErr{"Compose service not available", "INTERNAL_SERVER_ERROR", 500}
		}
		if err := h.ComposeSvc.Stop(in.ComposeID); err != nil {
			return nil, err
		}
		return true, nil
	}

	r["compose.refreshToken"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		var compose schema.Compose
		if err := h.DB.First(&compose, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		newToken := schema.GenerateAppName("refresh")
		h.DB.Model(&compose).Update("refreshToken", newToken)
		return map[string]string{"token": newToken}, nil
	}

	r["compose.cancelDeployment"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["compose.cleanQueues"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["compose.clearDeployments"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Where("\"composeId\" = ?", in.ComposeID).Delete(&schema.Deployment{})
		return true, nil
	}

	r["compose.killBuild"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID    string `json:"composeId"`
			DeploymentID string `json:"deploymentId"`
		}
		json.Unmarshal(input, &in)
		var dep schema.Deployment
		if err := h.DB.First(&dep, "\"deploymentId\" = ?", in.DeploymentID).Error; err == nil {
			if dep.PID != nil && *dep.PID != "" {
				exec.Command("kill", "-9", *dep.PID).Run()
			}
			status := schema.DeploymentStatusError
			h.DB.Model(&dep).Updates(map[string]interface{}{
				"\"status\"": status,
			})
		}
		return true, nil
	}

	r["compose.disconnectGitProvider"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Model(&schema.Compose{}).Where("\"composeId\" = ?", in.ComposeID).Updates(map[string]interface{}{
			"\"sourceType\"":  "raw",
			"\"githubId\"":    nil,
			"\"gitlabId\"":    nil,
			"\"bitbucketId\"": nil,
			"\"giteaId\"":     nil,
			"\"repository\"":  nil,
			"\"branch\"":      nil,
			"\"owner\"":       nil,
			"\"composePath\"": "./docker-compose.yml",
		})
		return true, nil
	}

	r["compose.move"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID     string `json:"composeId"`
			EnvironmentID string `json:"environmentId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Model(&schema.Compose{}).Where("\"composeId\" = ?", in.ComposeID).
			Update("\"environmentId\"", in.EnvironmentID)
		return true, nil
	}

	r["compose.fetchSourceType"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		var comp schema.Compose
		if err := h.DB.First(&comp, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		return comp.SourceType, nil
	}

	r["compose.randomizeCompose"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["compose.isolatedDeployment"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			h.Queue.EnqueueDeployCompose(in.ComposeID, nil) //nolint:errcheck
		}
		return true, nil
	}

	r["compose.getConvertedCompose"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		var comp schema.Compose
		if err := h.DB.First(&comp, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		return comp.ComposeFile, nil
	}

	r["compose.getDefaultCommand"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		var comp schema.Compose
		if err := h.DB.First(&comp, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		cmd := fmt.Sprintf("docker compose -p %s -f docker-compose.yml up -d --build", comp.AppName)
		return cmd, nil
	}

	r["compose.getTags"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		var comp schema.Compose
		if err := h.DB.First(&comp, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return []string{}, nil
		}
		if h.Config == nil {
			return []string{}, nil
		}
		codeDir := filepath.Join(h.Config.Paths.ComposePath, comp.AppName, "code")
		cmd := exec.Command("git", "tag", "--sort=-creatordate")
		cmd.Dir = codeDir
		out, err := cmd.Output()
		if err != nil {
			return []string{}, nil
		}
		var tags []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" {
				tags = append(tags, line)
			}
		}
		if tags == nil {
			tags = []string{}
		}
		return tags, nil
	}

	r["compose.loadServices"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		var comp schema.Compose
		if err := h.DB.First(&comp, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		if comp.ComposeFile == "" {
			return []interface{}{}, nil
		}
		// Parse compose YAML to extract service names
		var composeData map[string]interface{}
		if err := yaml.Unmarshal([]byte(comp.ComposeFile), &composeData); err != nil {
			return []interface{}{}, nil
		}
		services, ok := composeData["services"].(map[string]interface{})
		if !ok {
			return []interface{}{}, nil
		}
		var result []map[string]interface{}
		for name := range services {
			result = append(result, map[string]interface{}{
				"name": name,
			})
		}
		if result == nil {
			result = []map[string]interface{}{}
		}
		return result, nil
	}

	r["compose.loadMountsByService"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID   string `json:"composeId"`
			ServiceName string `json:"serviceName"`
		}
		json.Unmarshal(input, &in)
		var mounts []schema.Mount
		h.DB.Where("\"composeId\" = ? AND \"serviceName\" = ?", in.ComposeID, in.ServiceName).Find(&mounts)
		return mounts, nil
	}

	r["compose.templates"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BaseURL *string `json:"baseUrl"`
		}
		json.Unmarshal(input, &in)
		baseURL := "https://templates.dokploy.com"
		if in.BaseURL != nil && *in.BaseURL != "" {
			baseURL = *in.BaseURL
		}
		resp, err := http.Get(baseURL + "/meta.json")
		if err != nil {
			return []interface{}{}, nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return []interface{}{}, nil
		}
		var templates []interface{}
		if err := json.NewDecoder(resp.Body).Decode(&templates); err != nil {
			return []interface{}{}, nil
		}
		return templates, nil
	}

	r["compose.deployTemplate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			EnvironmentID string  `json:"environmentId"`
			ServerID      *string `json:"serverId"`
			ID            string  `json:"id"`
			BaseURL       *string `json:"baseUrl"`
		}
		json.Unmarshal(input, &in)

		baseURL := "https://templates.dokploy.com"
		if in.BaseURL != nil && *in.BaseURL != "" {
			baseURL = *in.BaseURL
		}

		// Fetch template files
		dockerCompose, templateConfig, err := fetchTemplateFiles(in.ID, baseURL)
		if err != nil {
			return nil, &trpcErr{"Failed to fetch template: " + err.Error(), "BAD_REQUEST", 400}
		}

		// Get server IP
		serverIP := "127.0.0.1"
		if in.ServerID != nil {
			var server schema.Server
			if err := h.DB.First(&server, "\"serverId\" = ?", *in.ServerID).Error; err == nil {
				serverIP = server.IPAddress
			}
		} else {
			settings, _ := h.getOrCreateSettings()
			if settings != nil && settings.ServerIP != nil && *settings.ServerIP != "" {
				serverIP = *settings.ServerIP
			}
		}

		// Get project name from environment
		var env schema.Environment
		if err := h.DB.Preload("Project").First(&env, "\"environmentId\" = ?", in.EnvironmentID).Error; err != nil {
			return nil, &trpcErr{"Environment not found", "NOT_FOUND", 404}
		}
		projectName := slugify(env.Project.Name + " " + in.ID)
		appName := schema.GenerateAppName(projectName)

		// Process variables
		if templateConfig.Variables == nil {
			templateConfig.Variables = map[string]string{}
		}
		templateConfig.Variables["APP_NAME"] = appName
		processed := processTemplateConfig(templateConfig, serverIP, projectName)

		// Create compose
		envStr := processed.Env
		compose := &schema.Compose{
			Name:          in.ID,
			AppName:       appName,
			EnvironmentID: in.EnvironmentID,
			ComposeFile:   dockerCompose,
			Env:           &envStr,
			SourceType:    schema.SourceTypeComposeRaw,
			ServerID:      in.ServerID,
		}
		if err := h.DB.Create(compose).Error; err != nil {
			return nil, err
		}

		// Create mounts
		for _, mount := range processed.Mounts {
			fp := mount.FilePath
			m := schema.Mount{
				FilePath:  &fp,
				MountPath: "",
				Content:   &mount.Content,
				ComposeID: &compose.ComposeID,
				Type:      schema.MountTypeFile,
			}
			h.DB.Create(&m)
		}

		// Create domains
		for _, domain := range processed.Domains {
			d := schema.Domain{
				Host:        domain.Host,
				Port:        &domain.Port,
				ServiceName: &domain.ServiceName,
				ComposeID:   &compose.ComposeID,
			}
			if domain.Path != "" {
				d.Path = &domain.Path
			}
			h.DB.Create(&d)
		}

		// Queue deployment
		if h.Queue != nil {
			h.Queue.EnqueueDeployCompose(compose.ComposeID, nil)
		}

		return compose, nil
	}

	r["compose.processTemplate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Base64    string `json:"base64"`
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)

		var comp schema.Compose
		if err := h.DB.Preload("Server").First(&comp, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}

		// Decode base64 template data
		decoded, err := base64Decode(in.Base64)
		if err != nil {
			return nil, &trpcErr{"Invalid base64 data", "BAD_REQUEST", 400}
		}

		// Parse template data (expected: {compose: string, config: {...}})
		var templateData struct {
			Compose string         `json:"compose"`
			Config  templateConfig `json:"config"`
		}
		if err := json.Unmarshal(decoded, &templateData); err != nil {
			return nil, &trpcErr{"Invalid template data", "BAD_REQUEST", 400}
		}

		serverIP := "127.0.0.1"
		if comp.ServerID != nil && comp.Server != nil {
			serverIP = comp.Server.IPAddress
		} else {
			settings, _ := h.getOrCreateSettings()
			if settings != nil && settings.ServerIP != nil && *settings.ServerIP != "" {
				serverIP = *settings.ServerIP
			}
		}

		if templateData.Config.Variables == nil {
			templateData.Config.Variables = map[string]string{}
		}
		templateData.Config.Variables["APP_NAME"] = comp.AppName
		processed := processTemplateConfig(templateData.Config, serverIP, comp.AppName)

		return map[string]interface{}{
			"compose":  templateData.Compose,
			"template": processed,
		}, nil
	}

	r["compose.import"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["compose.search"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Query string `json:"query"`
		}
		json.Unmarshal(input, &in)
		var comps []schema.Compose
		h.DB.Where("name ILIKE ?", "%"+in.Query+"%").Find(&comps)
		if comps == nil {
			comps = []schema.Compose{}
		}
		return comps, nil
	}
}

// --- Template processing helpers ---

type templateConfig struct {
	Variables map[string]string      `toml:"variables" json:"variables"`
	Config    templateConfigInner    `toml:"config" json:"config"`
}

type templateConfigInner struct {
	Domains []templateDomain       `toml:"domains" json:"domains"`
	Env     interface{}            `toml:"env" json:"env"`
	Mounts  []templateMount        `toml:"mounts" json:"mounts"`
}

type templateDomain struct {
	ServiceName string `toml:"serviceName" json:"serviceName"`
	Port        int    `toml:"port" json:"port"`
	Path        string `toml:"path" json:"path"`
	Host        string `toml:"host" json:"host"`
}

type templateMount struct {
	FilePath string `toml:"filePath" json:"filePath"`
	Content  string `toml:"content" json:"content"`
}

type processedTemplate struct {
	Envs    []string         `json:"envs"`
	Env     string           `json:"-"`
	Mounts  []templateMount  `json:"mounts"`
	Domains []templateDomain `json:"domains"`
}

func fetchTemplateFiles(templateID, baseURL string) (string, templateConfig, error) {
	// Fetch docker-compose.yml
	composeResp, err := http.Get(baseURL + "/blueprints/" + templateID + "/docker-compose.yml")
	if err != nil {
		return "", templateConfig{}, err
	}
	defer composeResp.Body.Close()
	if composeResp.StatusCode != 200 {
		return "", templateConfig{}, fmt.Errorf("template not found: %s", templateID)
	}
	composeBytes, _ := io.ReadAll(composeResp.Body)

	// Fetch template.toml
	tomlResp, err := http.Get(baseURL + "/blueprints/" + templateID + "/template.toml")
	if err != nil {
		return "", templateConfig{}, err
	}
	defer tomlResp.Body.Close()
	if tomlResp.StatusCode != 200 {
		return "", templateConfig{}, fmt.Errorf("template config not found: %s", templateID)
	}
	tomlBytes, _ := io.ReadAll(tomlResp.Body)

	var cfg templateConfig
	if _, err := toml.Decode(string(tomlBytes), &cfg); err != nil {
		return "", templateConfig{}, fmt.Errorf("invalid template config: %v", err)
	}

	return string(composeBytes), cfg, nil
}

var varPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func processTemplateConfig(cfg templateConfig, serverIP, projectName string) processedTemplate {
	vars := cfg.Variables
	if vars == nil {
		vars = map[string]string{}
	}

	// First pass: resolve generators in variables
	for k, v := range vars {
		vars[k] = processValue(v, vars, serverIP, projectName)
	}
	// Second pass: resolve references
	for k, v := range vars {
		vars[k] = processValue(v, vars, serverIP, projectName)
	}

	// Process env vars
	var envs []string
	switch e := cfg.Config.Env.(type) {
	case map[string]interface{}:
		for k, v := range e {
			val := fmt.Sprintf("%v", v)
			val = processValue(val, vars, serverIP, projectName)
			envs = append(envs, k+"="+val)
		}
	case []interface{}:
		for _, item := range e {
			switch v := item.(type) {
			case string:
				envs = append(envs, processValue(v, vars, serverIP, projectName))
			case map[string]interface{}:
				for k, val := range v {
					envs = append(envs, k+"="+fmt.Sprintf("%v", val))
				}
			}
		}
	}

	// Process domains
	var domains []templateDomain
	for _, d := range cfg.Config.Domains {
		host := d.Host
		if host != "" {
			host = processValue(host, vars, serverIP, projectName)
		} else {
			host = generateRandomDomain(serverIP, projectName)
		}
		domains = append(domains, templateDomain{
			ServiceName: d.ServiceName,
			Port:        d.Port,
			Path:        d.Path,
			Host:        host,
		})
	}

	// Process mounts
	var mounts []templateMount
	for _, m := range cfg.Config.Mounts {
		mounts = append(mounts, templateMount{
			FilePath: processValue(m.FilePath, vars, serverIP, projectName),
			Content:  processValue(m.Content, vars, serverIP, projectName),
		})
	}

	return processedTemplate{
		Envs:    envs,
		Env:     strings.Join(envs, "\n"),
		Mounts:  mounts,
		Domains: domains,
	}
}

func processValue(value string, vars map[string]string, serverIP, projectName string) string {
	return varPattern.ReplaceAllStringFunc(value, func(match string) string {
		varName := match[2 : len(match)-1]

		if varName == "domain" {
			return generateRandomDomain(serverIP, projectName)
		}
		if varName == "base64" {
			return generateBase64String(32)
		}
		if strings.HasPrefix(varName, "base64:") {
			n, _ := strconv.Atoi(strings.TrimPrefix(varName, "base64:"))
			if n == 0 { n = 32 }
			return generateBase64String(n)
		}
		if varName == "password" {
			return generatePassword(16)
		}
		if strings.HasPrefix(varName, "password:") {
			n, _ := strconv.Atoi(strings.TrimPrefix(varName, "password:"))
			if n == 0 { n = 16 }
			return generatePassword(n)
		}
		if varName == "hash" {
			return generateHash(8)
		}
		if strings.HasPrefix(varName, "hash:") {
			n, _ := strconv.Atoi(strings.TrimPrefix(varName, "hash:"))
			if n == 0 { n = 8 }
			return generateHash(n)
		}
		if varName == "uuid" {
			return generateUUID()
		}
		if varName == "randomPort" {
			n, _ := rand.Int(rand.Reader, big.NewInt(65535))
			return strconv.FormatInt(n.Int64(), 10)
		}

		// Variable reference
		if v, ok := vars[varName]; ok {
			return v
		}
		return match
	})
}

func generateRandomDomain(serverIP, projectName string) string {
	slug, _ := gonanoid.Generate("abcdefghijklmnopqrstuvwxyz0123456789", 6)
	return fmt.Sprintf("%s-%s.traefik.me", projectName, slug)
}

func generatePassword(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[n.Int64()]
	}
	return string(b)
}

func generateBase64String(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)[:length]
}

func generateHash(length int) string {
	b := make([]byte, (length+1)/2)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, s)
	// Remove consecutive dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
