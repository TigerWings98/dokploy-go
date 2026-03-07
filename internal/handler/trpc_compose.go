package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
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

	r["compose.redeploy"] = r["compose.deploy"]
	r["compose.start"] = r["compose.deploy"]

	r["compose.stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			_, err := h.Queue.EnqueueStopCompose(in.ComposeID)
			if err != nil {
				return nil, err
			}
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
		return []string{}, nil
	}

	r["compose.loadServices"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
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
		return []interface{}{}, nil
	}

	r["compose.deployTemplate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["compose.processTemplate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
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
