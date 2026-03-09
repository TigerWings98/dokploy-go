// Input: procedureRegistry, db (Application 表), service, docker, traefik, queue
// Output: registerApplicationTRPC - Application 领域的 tRPC procedure 注册
// Role: Application tRPC 路由注册，将 application.* procedure 绑定到应用管理和部署操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/ws"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerApplicationTRPC(r procedureRegistry) {
	r["application.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		_ = member
		var in struct {
			Name          string  `json:"name"`
			Description   *string `json:"description"`
			EnvironmentID string  `json:"environmentId"`
			ServerID      *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		app := &schema.Application{
			Name:          in.Name,
			Description:   in.Description,
			EnvironmentID: in.EnvironmentID,
			ServerID:      in.ServerID,
		}
		if err := h.DB.Create(app).Error; err != nil {
			return nil, err
		}
		return app, nil
	}

	r["application.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)

		var app schema.Application
		err := h.DB.
			Preload("Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC").Limit(10)
			}).
			Preload("Domains").
			Preload("Mounts").
			Preload("Redirects").
			Preload("Security").
			Preload("Ports").
			Preload("Registry").
			Preload("Server").
			Preload("Github").
			Preload("Github.GitProvider").
			Preload("Gitlab").
			Preload("Gitlab.GitProvider").
			Preload("Gitea").
			Preload("Gitea.GitProvider").
			Preload("Bitbucket").
			Preload("Bitbucket.GitProvider").
			First(&app, "\"applicationId\" = ?", in.ApplicationID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
			}
			return nil, err
		}
		// Ensure slices are never null in JSON
		if app.Deployments == nil { app.Deployments = []schema.Deployment{} }
		if app.Domains == nil { app.Domains = []schema.Domain{} }
		if app.Mounts == nil { app.Mounts = []schema.Mount{} }
		if app.Redirects == nil { app.Redirects = []schema.Redirect{} }
		if app.Security == nil { app.Security = []schema.Security{} }
		if app.Ports == nil { app.Ports = []schema.Port{} }

		// Load Environment.Project chain (needed by frontend for org context)
		if app.EnvironmentID != "" {
			var env schema.Environment
			if err := h.DB.First(&env, "\"environmentId\" = ?", app.EnvironmentID).Error; err == nil {
				if env.ProjectID != "" {
					var proj schema.Project
					if err := h.DB.First(&proj, "\"projectId\" = ?", env.ProjectID).Error; err == nil {
						env.Project = &proj
					}
				}
				app.Environment = &env
			}
		}
		return app, nil
	}

	r["application.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)

		var app schema.Application
		if err := h.DB.Preload("Server").Preload("Server.SSHKey").
			First(&app, "\"applicationId\" = ?", in.ApplicationID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}

		// 1. Remove Docker service
		if h.Docker != nil {
			h.Docker.RemoveService(context.Background(), app.AppName)
		}

		// 2. Remove Traefik config
		if h.Traefik != nil {
			h.Traefik.RemoveApplicationConfig(app.AppName)
		}

		// 3. Remove source code directory
		if h.Config != nil {
			codeDir := filepath.Join(h.Config.Paths.ApplicationsPath, app.AppName)
			os.RemoveAll(codeDir)

			// 4. Remove log files
			logDir := filepath.Join(h.Config.Paths.LogsPath, app.AppName)
			os.RemoveAll(logDir)
		}

		// 5. Delete from database (cascades to deployments, domains, etc.)
		if err := h.DB.Delete(&schema.Application{}, "\"applicationId\" = ?", in.ApplicationID).Error; err != nil {
			return nil, err
		}
		return true, nil
	}

	r["application.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		appID, _ := in["applicationId"].(string)
		delete(in, "applicationId")

		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}
		if err := h.DB.Model(&app).Updates(in).Error; err != nil {
			return nil, err
		}
		return app, nil
	}

	r["application.deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string  `json:"applicationId"`
			Title         *string `json:"title"`
			Description   *string `json:"description"`
		}
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			info, err := h.Queue.EnqueueDeployApplication(in.ApplicationID, in.Title, in.Description)
			if err != nil {
				return nil, err
			}
			return map[string]string{"message": "Deployment queued", "taskId": info.ID}, nil
		}
		return true, nil
	}

	// redeploy = rebuild（仅 build，不重新 clone 代码）
	r["application.redeploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string  `json:"applicationId"`
			Title         *string `json:"title"`
			Description   *string `json:"description"`
		}
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			info, err := h.Queue.EnqueueRebuildApplication(in.ApplicationID, in.Title, in.Description)
			if err != nil {
				return nil, err
			}
			return map[string]string{"message": "Rebuild queued", "taskId": info.ID}, nil
		}
		return true, nil
	}

	// stop/start 是同步操作（与 TS 版一致），不走队列
	r["application.stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		if h.AppSvc != nil {
			if err := h.AppSvc.Stop(in.ApplicationID); err != nil {
				return nil, err
			}
		}
		return true, nil
	}

	r["application.start"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		if h.AppSvc != nil {
			if err := h.AppSvc.Start(in.ApplicationID); err != nil {
				return nil, err
			}
		}
		return true, nil
	}

	// reload = 仅重启容器（同步操作，不 build，与 TS 版一致）
	r["application.reload"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		if h.AppSvc != nil {
			if err := h.AppSvc.Reload(in.ApplicationID); err != nil {
				return nil, err
			}
		}
		return true, nil
	}

	r["application.refreshToken"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", in.ApplicationID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}
		newToken := schema.GenerateAppName("refresh")
		h.DB.Model(&app).Update("refreshToken", newToken)
		return map[string]string{"token": newToken}, nil
	}

	// saveEnvironment, saveBuildType, saveXxxProvider - generic update
	for _, proc := range []string{
		"saveEnvironment", "saveBuildType",
		"saveGithubProvider", "saveGitlabProvider", "saveBitbucketProvider",
		"saveGiteaProvider", "saveDockerProvider", "saveGitProvider",
		"disconnectGitProvider", "markRunning",
		"updateTraefikConfig", "cancelDeployment", "cleanQueues",
	} {
		procName := proc
		r["application."+procName] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			appID, _ := in["applicationId"].(string)
			delete(in, "applicationId")

			var app schema.Application
			if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
				return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
			}
			if len(in) > 0 {
				h.DB.Model(&app).Updates(in)
			}
			return app, nil
		}
	}

	r["application.readTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", in.ApplicationID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}
		if h.Traefik != nil {
			config, _ := h.Traefik.ReadServiceConfig(app.AppName)
			return config, nil
		}
		return "", nil
	}

	r["application.clearDeployments"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string `json:"applicationId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Where("\"applicationId\" = ?", in.ApplicationID).Delete(&schema.Deployment{})
		return true, nil
	}

	r["application.dropDeployment"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DeploymentID string `json:"deploymentId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Deployment{}, "\"deploymentId\" = ?", in.DeploymentID)
		return true, nil
	}

	r["application.killBuild"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string `json:"applicationId"`
			DeploymentID  string `json:"deploymentId"`
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

	r["application.move"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string `json:"applicationId"`
			EnvironmentID string `json:"environmentId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Model(&schema.Application{}).Where("\"applicationId\" = ?", in.ApplicationID).
			Update("\"environmentId\"", in.EnvironmentID)
		return true, nil
	}

	r["application.readAppMonitoring"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName string `json:"appName"`
		}
		json.Unmarshal(input, &in)
		monPath := ""
		if h.Config != nil {
			monPath = h.Config.Paths.MonitoringPath
		}
		return ws.ReadAllStats(monPath, in.AppName), nil
	}

	r["application.search"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Query string `json:"query"`
		}
		json.Unmarshal(input, &in)
		var apps []schema.Application
		h.DB.Where("name ILIKE ?", "%"+in.Query+"%").Find(&apps)
		if apps == nil {
			apps = []schema.Application{}
		}
		return apps, nil
	}

	// Ensure strings import is used
	_ = strings.TrimSpace
}
