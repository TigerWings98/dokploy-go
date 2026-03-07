package handler

import (
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerDeploymentTRPC(r procedureRegistry) {
	r["deployment.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var deployments []schema.Deployment
		h.DB.Where("\"applicationId\" = ?", in.ApplicationID).
			Order("\"createdAt\" DESC").
			Find(&deployments)
		return deployments, nil
	}

	r["deployment.allByCompose"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		var deployments []schema.Deployment
		h.DB.Where("\"composeId\" = ?", in.ComposeID).
			Order("\"createdAt\" DESC").
			Find(&deployments)
		return deployments, nil
	}

	r["deployment.allByServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID string `json:"serverId"` }
		json.Unmarshal(input, &in)
		var deployments []schema.Deployment
		h.DB.Where("\"serverId\" = ?", in.ServerID).
			Order("\"createdAt\" DESC").
			Find(&deployments)
		return deployments, nil
	}

	r["deployment.removeDeployment"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ DeploymentID string `json:"deploymentId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Deployment{}, "\"deploymentId\" = ?", in.DeploymentID)
		return true, nil
	}

	r["deployment.allByType"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		json.Unmarshal(input, &in)

		colMap := map[string]string{
			"application":       "applicationId",
			"compose":           "composeId",
			"server":            "serverId",
			"schedule":          "scheduleId",
			"previewDeployment": "previewDeploymentId",
			"backup":            "backupId",
			"volumeBackup":      "volumeBackupId",
		}
		col, ok := colMap[in.Type]
		if !ok {
			return nil, &trpcErr{"Invalid deployment type", "BAD_REQUEST", 400}
		}

		var deployments []schema.Deployment
		h.DB.Where(fmt.Sprintf("\"%s\" = ?", col), in.ID).
			Order("\"createdAt\" DESC").
			Find(&deployments)
		if deployments == nil {
			deployments = []schema.Deployment{}
		}
		return deployments, nil
	}

	r["deployment.allCentralized"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var deployments []schema.Deployment
		h.DB.
			Preload("Application").
			Preload("Application.Environment").
			Preload("Application.Server").
			Preload("Compose").
			Preload("Compose.Environment").
			Preload("Compose.Server").
			Preload("Server").
			Preload("BuildServer").
			Order("\"createdAt\" DESC").Limit(50).Find(&deployments)
		if deployments == nil {
			deployments = []schema.Deployment{}
		}

		for i := range deployments {
			if deployments[i].Application != nil && deployments[i].Application.Environment != nil {
				env := deployments[i].Application.Environment
				if env.ProjectID != "" {
					var project schema.Project
					if err := h.DB.First(&project, "\"projectId\" = ?", env.ProjectID).Error; err == nil {
						env.Project = &project
					}
				}
			}
			if deployments[i].Compose != nil && deployments[i].Compose.Environment != nil {
				env := deployments[i].Compose.Environment
				if env.ProjectID != "" {
					var project schema.Project
					if err := h.DB.First(&project, "\"projectId\" = ?", env.ProjectID).Error; err == nil {
						env.Project = &project
					}
				}
			}
		}

		return deployments, nil
	}

	r["deployment.queueList"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	r["deployment.killProcess"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DeploymentID string `json:"deploymentId"`
		}
		json.Unmarshal(input, &in)

		var dep schema.Deployment
		if err := h.DB.First(&dep, "\"deploymentId\" = ?", in.DeploymentID).Error; err != nil {
			return nil, &trpcErr{"Deployment not found", "NOT_FOUND", 404}
		}
		if dep.PID != nil && *dep.PID != "" {
			exec.Command("kill", "-9", *dep.PID).Run()
		}
		status := schema.DeploymentStatusError
		h.DB.Model(&dep).Update("\"status\"", status)
		return true, nil
	}

	// Rollback
	r["rollback.rollback"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ RollbackID string `json:"rollbackId"` }
		json.Unmarshal(input, &in)
		var rb schema.Rollback
		if err := h.DB.First(&rb, "\"rollbackId\" = ?", in.RollbackID).Error; err != nil {
			return nil, &trpcErr{"Rollback not found", "NOT_FOUND", 404}
		}
		if h.Queue != nil && rb.ApplicationID != "" {
			title := fmt.Sprintf("Rollback to %s", rb.DockerImage)
			h.Queue.EnqueueDeployApplication(rb.ApplicationID, &title, nil)
		}
		return true, nil
	}

	r["rollback.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ RollbackID string `json:"rollbackId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Rollback{}, "\"rollbackId\" = ?", in.RollbackID)
		return true, nil
	}
}
