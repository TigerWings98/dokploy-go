// Input: db (Postgres/MySQL/MariaDB/Mongo/Redis 表), docker, service
// Output: 数据库服务通用操作 (部署/停止/重建/删除) + 环境变量/挂载/端口共享逻辑
// Role: 5 种数据库服务的共享 handler 逻辑，提取部署和管理操作的公共实现
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// dbInfo abstracts database model lookup for shared endpoints.
type dbInfo struct {
	appName  string
	serverID *string
	envID    string
}

func (h *Handler) lookupDB(dbType, id string) (*dbInfo, error) {
	switch dbType {
	case "postgres":
		var pg schema.Postgres
		if err := h.DB.First(&pg, "\"postgresId\" = ?", id).Error; err != nil {
			return nil, err
		}
		return &dbInfo{appName: pg.AppName, serverID: pg.ServerID, envID: pg.EnvironmentID}, nil
	case "mysql":
		var my schema.MySQL
		if err := h.DB.First(&my, "\"mysqlId\" = ?", id).Error; err != nil {
			return nil, err
		}
		return &dbInfo{appName: my.AppName, serverID: my.ServerID, envID: my.EnvironmentID}, nil
	case "mariadb":
		var mdb schema.MariaDB
		if err := h.DB.First(&mdb, "\"mariadbId\" = ?", id).Error; err != nil {
			return nil, err
		}
		return &dbInfo{appName: mdb.AppName, serverID: mdb.ServerID, envID: mdb.EnvironmentID}, nil
	case "mongo":
		var mongo schema.Mongo
		if err := h.DB.First(&mongo, "\"mongoId\" = ?", id).Error; err != nil {
			return nil, err
		}
		return &dbInfo{appName: mongo.AppName, serverID: mongo.ServerID, envID: mongo.EnvironmentID}, nil
	case "redis":
		var redis schema.Redis
		if err := h.DB.First(&redis, "\"redisId\" = ?", id).Error; err != nil {
			return nil, err
		}
		return &dbInfo{appName: redis.AppName, serverID: redis.ServerID, envID: redis.EnvironmentID}, nil
	}
	return nil, fmt.Errorf("unknown database type: %s", dbType)
}

// Generic database operations shared across all 5 DB types.

func (h *Handler) dbStart(c echo.Context, dbType, idParam, idColumn string) error {
	id := c.Param(idParam)

	if _, err := h.lookupDB(dbType, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("%s not found", dbType))
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// 与 TS 版对齐：通过 service 层启动（支持远程服务器）
	if h.DBSvc != nil {
		if err := h.DBSvc.StartDatabase(id, schema.DatabaseType(dbType)); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("%s started", dbType)})
}

func (h *Handler) dbReload(c echo.Context, dbType, idParam, idColumn string) error {
	id := c.Param(idParam)

	info, err := h.lookupDB(dbType, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("%s not found", dbType))
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Docker != nil {
		if err := h.Docker.RestartService(context.Background(), info.appName); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("%s restarted", dbType)})
}

func (h *Handler) dbRebuild(c echo.Context, dbType, idParam string) error {
	id := c.Param(idParam)

	if _, err := h.lookupDB(dbType, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("%s not found", dbType))
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// 与 TS 版对齐：内联执行，不走队列
	if h.DBSvc != nil {
		go h.DBSvc.RebuildDatabase(id, schema.DatabaseType(dbType))
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Rebuild started"})
}

type ChangeStatusRequest struct {
	ApplicationStatus string `json:"applicationStatus" validate:"required"`
}

func (h *Handler) dbChangeStatus(c echo.Context, dbType, idParam, idColumn string) error {
	id := c.Param(idParam)
	var req ChangeStatusRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if _, err := h.lookupDB(dbType, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("%s not found", dbType))
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.updateDBStatus(dbType, idColumn, id, schema.ApplicationStatus(req.ApplicationStatus))
	return c.JSON(http.StatusOK, map[string]string{"message": "Status updated"})
}

type SaveExternalPortRequest struct {
	ExternalPort *int `json:"externalPort"`
}

func (h *Handler) dbSaveExternalPort(c echo.Context, dbType, idParam, idColumn, tableName string) error {
	id := c.Param(idParam)
	var req SaveExternalPortRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	result := h.DB.Table(tableName).Where(fmt.Sprintf("\"%s\" = ?", idColumn), id).
		Update("externalPort", req.ExternalPort)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("%s not found", dbType))
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "External port saved"})
}

type SaveEnvironmentRequest struct {
	Env string `json:"env"`
}

func (h *Handler) dbSaveEnvironment(c echo.Context, dbType, idParam, idColumn, tableName string) error {
	id := c.Param(idParam)
	var req SaveEnvironmentRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	result := h.DB.Table(tableName).Where(fmt.Sprintf("\"%s\" = ?", idColumn), id).
		Update("env", req.Env)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("%s not found", dbType))
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Environment saved"})
}

type MoveRequest struct {
	TargetEnvironmentID string `json:"targetEnvironmentId" validate:"required"`
}

func (h *Handler) dbMove(c echo.Context, dbType, idParam, idColumn, tableName string) error {
	id := c.Param(idParam)
	var req MoveRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Verify target environment exists
	var env schema.Environment
	if err := h.DB.First(&env, "\"environmentId\" = ?", req.TargetEnvironmentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Target environment not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	result := h.DB.Table(tableName).Where(fmt.Sprintf("\"%s\" = ?", idColumn), id).
		Update("environmentId", req.TargetEnvironmentID)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("%s not found", dbType))
	}
	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("%s moved", dbType)})
}

func (h *Handler) updateDBStatus(dbType, idColumn, id string, status schema.ApplicationStatus) {
	var tableName string
	switch dbType {
	case "postgres":
		tableName = "postgres"
	case "mysql":
		tableName = "mysql"
	case "mariadb":
		tableName = "mariadb"
	case "mongo":
		tableName = "mongo"
	case "redis":
		tableName = "redis"
	}
	h.DB.Table(tableName).Where(fmt.Sprintf("\"%s\" = ?", idColumn), id).
		Update("applicationStatus", status)
}
