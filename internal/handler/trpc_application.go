package handler

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
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
			First(&app, "\"applicationId\" = ?", in.ApplicationID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
			}
			return nil, err
		}
		return app, nil
	}

	r["application.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		result := h.DB.Delete(&schema.Application{}, "\"applicationId\" = ?", in.ApplicationID)
		if result.Error != nil {
			return nil, result.Error
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

	r["application.redeploy"] = r["application.deploy"]

	r["application.stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			_, err := h.Queue.EnqueueStopApplication(in.ApplicationID)
			if err != nil {
				return nil, err
			}
		}
		return true, nil
	}

	r["application.start"] = r["application.deploy"]

	r["application.reload"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			title := "Reload"
			_, err := h.Queue.EnqueueDeployApplication(in.ApplicationID, &title, nil)
			if err != nil {
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
		return map[string]interface{}{
			"data": []interface{}{},
		}, nil
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
