// Input: db (Project 表 + 关联的 Application/Compose/Database)
// Output: Project CRUD 的 tRPC procedure 实现
// Role: 项目管理 handler，处理项目的创建/更新/删除/列表/详情查询
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerProjectRoutes(g *echo.Group) {
	g.POST("", h.CreateProject)
	g.GET("/:projectId", h.GetProject)
	g.GET("", h.ListProjects)
	g.PUT("/:projectId", h.UpdateProject)
	g.DELETE("/:projectId", h.DeleteProject)
}

type CreateProjectRequest struct {
	Name        string  `json:"name" validate:"required,min=1"`
	Description *string `json:"description"`
	Env         string  `json:"env"`
}

func (h *Handler) CreateProject(c echo.Context) error {
	var req CreateProjectRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	// Get user's default organization
	var member schema.Member
	err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	project := &schema.Project{
		Name:           req.Name,
		Description:    req.Description,
		Env:            req.Env,
		OrganizationID: member.OrganizationID,
	}

	if err := h.DB.Create(project).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Create default environment
	env := &schema.Environment{
		Name:      "Production",
		ProjectID: project.ProjectID,
		IsDefault: true,
	}
	if err := h.DB.Create(env).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Reload with relations
	h.DB.Preload("Environments").First(project, "\"projectId\" = ?", project.ProjectID)

	return c.JSON(http.StatusCreated, project)
}

func (h *Handler) GetProject(c echo.Context) error {
	projectID := c.Param("projectId")

	var project schema.Project
	err := h.DB.
		Preload("Environments").
		Preload("Environments.Applications").
		Preload("Environments.Postgres").
		Preload("Environments.MySQL").
		Preload("Environments.MariaDB").
		Preload("Environments.Mongo").
		Preload("Environments.Redis").
		Preload("Environments.Compose").
		First(&project, "\"projectId\" = ?", projectID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Project not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, project)
}

func (h *Handler) ListProjects(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var projects []schema.Project
	err = h.DB.
		Preload("Environments").
		Where("\"organizationId\" = ?", member.OrganizationID).
		Order("\"createdAt\" DESC").
		Find(&projects).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, projects)
}

type UpdateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Env         *string `json:"env"`
}

func (h *Handler) UpdateProject(c echo.Context) error {
	projectID := c.Param("projectId")

	var req UpdateProjectRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var project schema.Project
	if err := h.DB.First(&project, "\"projectId\" = ?", projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Project not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	updates := map[string]interface{}{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Env != nil {
		updates["env"] = *req.Env
	}

	if len(updates) > 0 {
		if err := h.DB.Model(&project).Updates(updates).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, project)
}

func (h *Handler) DeleteProject(c echo.Context) error {
	projectID := c.Param("projectId")

	result := h.DB.Delete(&schema.Project{}, "\"projectId\" = ?", projectID)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Project not found")
	}

	return c.NoContent(http.StatusNoContent)
}
