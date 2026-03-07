package handler

import (
	"encoding/json"
	"errors"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerProjectTRPC(r procedureRegistry) {
	// === project.all ===
	r["project.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var projects []schema.Project
		if err := h.DB.
			Preload("Environments").
			Preload("Environments.Applications").
			Preload("Environments.Postgres").
			Preload("Environments.MySQL").
			Preload("Environments.MariaDB").
			Preload("Environments.Mongo").
			Preload("Environments.Redis").
			Preload("Environments.Compose").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Order("\"createdAt\" DESC").
			Find(&projects).Error; err != nil {
			return nil, err
		}
		for i := range projects {
			if projects[i].Environments == nil {
				projects[i].Environments = []schema.Environment{}
			}
			for j := range projects[i].Environments {
				e := &projects[i].Environments[j]
				if e.Applications == nil { e.Applications = []schema.Application{} }
				if e.Postgres == nil { e.Postgres = []schema.Postgres{} }
				if e.MySQL == nil { e.MySQL = []schema.MySQL{} }
				if e.MariaDB == nil { e.MariaDB = []schema.MariaDB{} }
				if e.Mongo == nil { e.Mongo = []schema.Mongo{} }
				if e.Redis == nil { e.Redis = []schema.Redis{} }
				if e.Compose == nil { e.Compose = []schema.Compose{} }
			}
		}
		return projects, nil
	}

	// === project.one ===
	r["project.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ProjectID string `json:"projectId"` }
		json.Unmarshal(input, &in)
		var project schema.Project
		err := h.DB.
			Preload("Environments").
			First(&project, "\"projectId\" = ?", in.ProjectID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &trpcErr{"Project not found", "NOT_FOUND", 404}
			}
			return nil, err
		}
		return project, nil
	}

	// === project.create ===
	r["project.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Name        string  `json:"name"`
			Description *string `json:"description"`
		}
		json.Unmarshal(input, &in)

		project := &schema.Project{
			Name:           in.Name,
			Description:    in.Description,
			OrganizationID: member.OrganizationID,
		}
		if err := h.DB.Create(project).Error; err != nil {
			return nil, err
		}
		return project, nil
	}

	// === project.remove ===
	r["project.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ProjectID string `json:"projectId"` }
		json.Unmarshal(input, &in)
		result := h.DB.Delete(&schema.Project{}, "\"projectId\" = ?", in.ProjectID)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, &trpcErr{"Project not found", "NOT_FOUND", 404}
		}
		return true, nil
	}

	// === project.update ===
	r["project.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		projectID, _ := in["projectId"].(string)
		delete(in, "projectId")

		var project schema.Project
		if err := h.DB.First(&project, "\"projectId\" = ?", projectID).Error; err != nil {
			return nil, &trpcErr{"Project not found", "NOT_FOUND", 404}
		}
		if err := h.DB.Model(&project).Updates(in).Error; err != nil {
			return nil, err
		}
		return project, nil
	}

	// === project.allForPermissions ===
	r["project.allForPermissions"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var projects []schema.Project
		h.DB.
			Preload("Environments").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Order("\"createdAt\" DESC").
			Find(&projects)
		if projects == nil {
			projects = []schema.Project{}
		}
		return projects, nil
	}

	// === project.duplicate ===
	r["project.duplicate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ProjectID string `json:"projectId"`
		}
		json.Unmarshal(input, &in)

		var srcProject schema.Project
		if err := h.DB.Preload("Environments").First(&srcProject, "\"projectId\" = ?", in.ProjectID).Error; err != nil {
			return nil, &trpcErr{"Project not found", "NOT_FOUND", 404}
		}

		newProject := schema.Project{
			Name:           srcProject.Name + " (copy)",
			Description:    srcProject.Description,
			OrganizationID: srcProject.OrganizationID,
		}
		if err := h.DB.Create(&newProject).Error; err != nil {
			return nil, err
		}

		for _, env := range srcProject.Environments {
			newEnv := schema.Environment{
				Name:      env.Name,
				ProjectID: newProject.ProjectID,
			}
			h.DB.Create(&newEnv)
		}

		return newProject, nil
	}

	// === project.search ===
	r["project.search"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Query string `json:"query"`
		}
		json.Unmarshal(input, &in)
		var projects []schema.Project
		h.DB.Where("\"organizationId\" = ? AND name ILIKE ?", member.OrganizationID, "%"+in.Query+"%").Find(&projects)
		if projects == nil {
			projects = []schema.Project{}
		}
		return projects, nil
	}

	// === environment.one ===
	r["environment.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ EnvironmentID string `json:"environmentId"` }
		json.Unmarshal(input, &in)
		var env schema.Environment
		if err := h.DB.
			Preload("Applications").
			Preload("Postgres").
			Preload("MySQL").
			Preload("MariaDB").
			Preload("Mongo").
			Preload("Redis").
			Preload("Compose").
			First(&env, "\"environmentId\" = ?", in.EnvironmentID).Error; err != nil {
			return nil, &trpcErr{"Environment not found", "NOT_FOUND", 404}
		}
		if env.ProjectID != "" {
			var proj schema.Project
			if err := h.DB.First(&proj, "\"projectId\" = ?", env.ProjectID).Error; err == nil {
				env.Project = &proj
			}
		}
		return env, nil
	}

	// === environment.byProjectId ===
	r["environment.byProjectId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ProjectID string `json:"projectId"` }
		json.Unmarshal(input, &in)
		var envs []schema.Environment
		h.DB.
			Preload("Applications").
			Preload("Postgres").
			Preload("MySQL").
			Preload("MariaDB").
			Preload("Mongo").
			Preload("Redis").
			Preload("Compose").
			Where("\"projectId\" = ?", in.ProjectID).Find(&envs)
		if envs == nil {
			envs = []schema.Environment{}
		}
		return envs, nil
	}

	// === environment.create ===
	r["environment.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var env schema.Environment
		json.Unmarshal(input, &env)
		if err := h.DB.Create(&env).Error; err != nil {
			return nil, err
		}
		return env, nil
	}

	// === environment.update ===
	r["environment.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["environmentId"].(string)
		delete(in, "environmentId")

		var env schema.Environment
		if err := h.DB.First(&env, "\"environmentId\" = ?", id).Error; err != nil {
			return nil, &trpcErr{"Environment not found", "NOT_FOUND", 404}
		}
		h.DB.Model(&env).Updates(in)
		return env, nil
	}

	// === environment.duplicate ===
	r["environment.duplicate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			EnvironmentID string `json:"environmentId"`
			Name          string `json:"name"`
		}
		json.Unmarshal(input, &in)

		var srcEnv schema.Environment
		if err := h.DB.First(&srcEnv, "\"environmentId\" = ?", in.EnvironmentID).Error; err != nil {
			return nil, &trpcErr{"Environment not found", "NOT_FOUND", 404}
		}

		newEnv := schema.Environment{
			Name:      in.Name,
			ProjectID: srcEnv.ProjectID,
		}
		if err := h.DB.Create(&newEnv).Error; err != nil {
			return nil, err
		}
		return newEnv, nil
	}

	// === environment.remove ===
	r["environment.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ EnvironmentID string `json:"environmentId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Environment{}, "\"environmentId\" = ?", in.EnvironmentID)
		return true, nil
	}
}
