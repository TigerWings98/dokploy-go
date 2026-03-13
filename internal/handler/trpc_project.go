// Input: procedureRegistry, db (Project 表)
// Output: registerProjectTRPC - Project 领域的 tRPC procedure 注册
// Role: Project tRPC 路由注册，将 project.* procedure 绑定到项目 CRUD 操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
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
		if project.Environments == nil {
			project.Environments = []schema.Environment{}
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
		in = h.filterColumns(&schema.Project{}, in)

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
		if err := h.DB.
			Preload("Environments").
			Preload("Environments.Applications").
			Preload("Environments.Applications.Domains").
			Preload("Environments.Applications.Ports").
			Preload("Environments.Applications.Mounts").
			Preload("Environments.Applications.Redirects").
			Preload("Environments.Applications.Security").
			Preload("Environments.Postgres").
			Preload("Environments.MySQL").
			Preload("Environments.MariaDB").
			Preload("Environments.Mongo").
			Preload("Environments.Redis").
			Preload("Environments.Compose").
			First(&srcProject, "\"projectId\" = ?", in.ProjectID).Error; err != nil {
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

			// Duplicate applications
			for _, app := range env.Applications {
				newApp := app
				newApp.ApplicationID = ""
				newApp.AppName = schema.GenerateAppName("app")
				newApp.EnvironmentID = newEnv.EnvironmentID
				newApp.Deployments = nil
				newApp.Domains = nil
				newApp.Ports = nil
				newApp.Mounts = nil
				newApp.Redirects = nil
				newApp.Security = nil
				newApp.Environment = nil
				newApp.Server = nil
				newApp.Registry = nil
				newApp.CreatedAt = ""
				h.DB.Create(&newApp)

				for _, d := range app.Domains {
					d.DomainID = ""
					d.ApplicationID = &newApp.ApplicationID
					h.DB.Create(&d)
				}
				for _, p := range app.Ports {
					p.PortID = ""
					p.ApplicationID = &newApp.ApplicationID
					h.DB.Create(&p)
				}
				for _, m := range app.Mounts {
					m.MountID = ""
					m.ApplicationID = &newApp.ApplicationID
					h.DB.Create(&m)
				}
				for _, rd := range app.Redirects {
					rd.RedirectID = ""
					rd.ApplicationID = &newApp.ApplicationID
					h.DB.Create(&rd)
				}
				for _, s := range app.Security {
					s.SecurityID = ""
					s.ApplicationID = &newApp.ApplicationID
					h.DB.Create(&s)
				}
			}

			// Duplicate database services
			for _, pg := range env.Postgres {
				pg.PostgresID = ""
				pg.AppName = schema.GenerateAppName("pg")
				pg.EnvironmentID = newEnv.EnvironmentID
				pg.CreatedAt = ""
				h.DB.Create(&pg)
			}
			for _, my := range env.MySQL {
				my.MySQLID = ""
				my.AppName = schema.GenerateAppName("mysql")
				my.EnvironmentID = newEnv.EnvironmentID
				my.CreatedAt = ""
				h.DB.Create(&my)
			}
			for _, ma := range env.MariaDB {
				ma.MariaDBID = ""
				ma.AppName = schema.GenerateAppName("maria")
				ma.EnvironmentID = newEnv.EnvironmentID
				ma.CreatedAt = ""
				h.DB.Create(&ma)
			}
			for _, mo := range env.Mongo {
				mo.MongoID = ""
				mo.AppName = schema.GenerateAppName("mongo")
				mo.EnvironmentID = newEnv.EnvironmentID
				mo.CreatedAt = ""
				h.DB.Create(&mo)
			}
			for _, rd := range env.Redis {
				rd.RedisID = ""
				rd.AppName = schema.GenerateAppName("redis")
				rd.EnvironmentID = newEnv.EnvironmentID
				rd.CreatedAt = ""
				h.DB.Create(&rd)
			}

			// Duplicate compose services
			for _, comp := range env.Compose {
				comp.ComposeID = ""
				comp.AppName = schema.GenerateAppName("compose")
				comp.EnvironmentID = newEnv.EnvironmentID
				comp.Deployments = nil
				comp.Domains = nil
				comp.Mounts = nil
				comp.Security = nil
				comp.Redirects = nil
				comp.Environment = nil
				comp.Server = nil
				comp.CreatedAt = ""
				h.DB.Create(&comp)
			}
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
		// 确保 slice 字段不为 null
		if env.Applications == nil { env.Applications = []schema.Application{} }
		if env.Postgres == nil { env.Postgres = []schema.Postgres{} }
		if env.MySQL == nil { env.MySQL = []schema.MySQL{} }
		if env.MariaDB == nil { env.MariaDB = []schema.MariaDB{} }
		if env.Mongo == nil { env.Mongo = []schema.Mongo{} }
		if env.Redis == nil { env.Redis = []schema.Redis{} }
		if env.Compose == nil { env.Compose = []schema.Compose{} }
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
		// 确保每个 environment 的 slice 字段不为 null
		for i := range envs {
			if envs[i].Applications == nil { envs[i].Applications = []schema.Application{} }
			if envs[i].Postgres == nil { envs[i].Postgres = []schema.Postgres{} }
			if envs[i].MySQL == nil { envs[i].MySQL = []schema.MySQL{} }
			if envs[i].MariaDB == nil { envs[i].MariaDB = []schema.MariaDB{} }
			if envs[i].Mongo == nil { envs[i].Mongo = []schema.Mongo{} }
			if envs[i].Redis == nil { envs[i].Redis = []schema.Redis{} }
			if envs[i].Compose == nil { envs[i].Compose = []schema.Compose{} }
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
		in = h.filterColumns(&schema.Environment{}, in)

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

	// === environment.search ===
	r["environment.search"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Q           *string `json:"q"`
			Name        *string `json:"name"`
			Description *string `json:"description"`
			ProjectID   *string `json:"projectId"`
			Limit       int     `json:"limit"`
			Offset      int     `json:"offset"`
		}
		json.Unmarshal(input, &in)
		if in.Limit <= 0 {
			in.Limit = 20
		}
		if in.Limit > 100 {
			in.Limit = 100
		}

		query := h.DB.Table("environment").
			Joins("JOIN project ON environment.\"projectId\" = project.\"projectId\"").
			Where("project.\"organizationId\" = ?", member.OrganizationID)

		if in.ProjectID != nil && *in.ProjectID != "" {
			query = query.Where("environment.\"projectId\" = ?", *in.ProjectID)
		}
		if in.Q != nil && *in.Q != "" {
			term := "%" + *in.Q + "%"
			query = query.Where("(environment.name ILIKE ? OR environment.description ILIKE ?)", term, term)
		}
		if in.Name != nil && *in.Name != "" {
			query = query.Where("environment.name ILIKE ?", "%"+*in.Name+"%")
		}
		if in.Description != nil && *in.Description != "" {
			query = query.Where("environment.description ILIKE ?", "%"+*in.Description+"%")
		}

		var total int64
		query.Count(&total)

		var items []schema.Environment
		query.Offset(in.Offset).Limit(in.Limit).Order("environment.\"createdAt\" DESC").
			Select("environment.*").Find(&items)
		if items == nil {
			items = []schema.Environment{}
		}

		return map[string]interface{}{
			"items": items,
			"total": total,
		}, nil
	}
}
