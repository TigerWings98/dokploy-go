// Input: db (Mount 表)
// Output: Mount CRUD 的 tRPC procedure 实现
// Role: 挂载管理 handler，配置 bind/volume/file 类型的挂载点，file 类型同步写入/删除磁盘文件
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerMountRoutes(g *echo.Group) {
	g.POST("", h.CreateMount)
	g.GET("/:mountId", h.GetMount)
	g.PUT("/:mountId", h.UpdateMount)
	g.DELETE("/:mountId", h.DeleteMount)
}

type CreateMountRequest struct {
	Type          string  `json:"type" validate:"required"`
	HostPath      *string `json:"hostPath"`
	VolumeName    *string `json:"volumeName"`
	Content       *string `json:"content"`
	MountPath     string  `json:"mountPath" validate:"required"`
	ServiceName   *string `json:"serviceName"`
	FilePath      *string `json:"filePath"`
	ApplicationID *string `json:"applicationId"`
	PostgresID    *string `json:"postgresId"`
	MariaDBID     *string `json:"mariadbId"`
	MongoID       *string `json:"mongoId"`
	MySQLID       *string `json:"mysqlId"`
	RedisID       *string `json:"redisId"`
	ComposeID     *string `json:"composeId"`
}

func (h *Handler) CreateMount(c echo.Context) error {
	var req CreateMountRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	m := &schema.Mount{
		Type:          schema.MountType(req.Type),
		HostPath:      req.HostPath,
		VolumeName:    req.VolumeName,
		Content:       req.Content,
		MountPath:     req.MountPath,
		ServiceName:   req.ServiceName,
		FilePath:      req.FilePath,
		ApplicationID: req.ApplicationID,
		PostgresID:    req.PostgresID,
		MariaDBID:     req.MariaDBID,
		MongoID:       req.MongoID,
		MySQLID:       req.MySQLID,
		RedisID:       req.RedisID,
		ComposeID:     req.ComposeID,
	}

	if err := h.DB.Create(m).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// 与 TS 版一致：file 类型 mount 创建后写入磁盘
	if m.Type == schema.MountTypeFile {
		h.writeFileMountToDisk(m)
	}

	return c.JSON(http.StatusCreated, m)
}

func (h *Handler) GetMount(c echo.Context) error {
	id := c.Param("mountId")

	var m schema.Mount
	if err := h.DB.First(&m, "\"mountId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mount not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) UpdateMount(c echo.Context) error {
	id := c.Param("mountId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var m schema.Mount
	if err := h.DB.First(&m, "\"mountId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mount not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&m).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// 与 TS 版一致：file 类型 mount 更新后同步磁盘文件
	if m.Type == schema.MountTypeFile {
		// 重新加载完整记录（Updates 可能没有更新所有字段到 m 上）
		h.DB.First(&m, "\"mountId\" = ?", id)
		h.writeFileMountToDisk(&m)
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) DeleteMount(c echo.Context) error {
	id := c.Param("mountId")

	var m schema.Mount
	if err := h.DB.First(&m, "\"mountId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mount not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// 与 TS 版一致：file 类型 mount 删除时清理磁盘文件
	if m.Type == schema.MountTypeFile {
		h.deleteFileMountFromDisk(&m)
	}

	if err := h.DB.Delete(&m).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

// writeFileMountToDisk 将 file 类型 mount 的内容写入磁盘（本地或远程）
// 路径格式：{basePath}/{appName}/files/{filePath}
func (h *Handler) writeFileMountToDisk(m *schema.Mount) {
	appName, serverID := h.resolveMountService(m)
	if appName == "" || m.FilePath == nil {
		return
	}

	basePath := h.getMountBasePath(m)
	fullPath := filepath.Join(basePath, appName, "files", *m.FilePath)
	content := ""
	if m.Content != nil {
		content = *m.Content
	}

	if serverID != nil {
		// 远程服务器：通过 SSH 执行 base64 解码写入（与 TS 版一致）
		var server schema.Server
		if h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", *serverID).Error != nil || server.SSHKey == nil {
			return
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(content))
		dir := filepath.Dir(fullPath)
		cmd := fmt.Sprintf("mkdir -p %s && echo '%s' | base64 -d > %s", dir, encoded, fullPath)
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		process.ExecAsyncRemote(conn, cmd, nil)
	} else {
		// 本地：直接写文件
		dir := filepath.Dir(fullPath)
		os.MkdirAll(dir, 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}
}

// deleteFileMountFromDisk 从磁盘删除 file 类型 mount 的文件
func (h *Handler) deleteFileMountFromDisk(m *schema.Mount) {
	appName, serverID := h.resolveMountService(m)
	if appName == "" || m.FilePath == nil {
		return
	}

	basePath := h.getMountBasePath(m)
	fullPath := filepath.Join(basePath, appName, "files", *m.FilePath)

	if serverID != nil {
		var server schema.Server
		if h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", *serverID).Error != nil || server.SSHKey == nil {
			return
		}
		cmd := fmt.Sprintf("rm -rf %s", fullPath)
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		process.ExecAsyncRemote(conn, cmd, nil)
	} else {
		os.Remove(fullPath)
	}
}

// resolveMountService 从 mount 关联的服务中解析 appName 和 serverID
func (h *Handler) resolveMountService(m *schema.Mount) (appName string, serverID *string) {
	if m.ApplicationID != nil {
		var app schema.Application
		if h.DB.First(&app, "\"applicationId\" = ?", *m.ApplicationID).Error == nil {
			return app.AppName, app.ServerID
		}
	}
	if m.ComposeID != nil {
		var comp schema.Compose
		if h.DB.First(&comp, "\"composeId\" = ?", *m.ComposeID).Error == nil {
			return comp.AppName, comp.ServerID
		}
	}
	if m.PostgresID != nil {
		var pg schema.Postgres
		if h.DB.First(&pg, "\"postgresId\" = ?", *m.PostgresID).Error == nil {
			return pg.AppName, pg.ServerID
		}
	}
	if m.MySQLID != nil {
		var my schema.MySQL
		if h.DB.First(&my, "\"mysqlId\" = ?", *m.MySQLID).Error == nil {
			return my.AppName, my.ServerID
		}
	}
	if m.MariaDBID != nil {
		var ma schema.MariaDB
		if h.DB.First(&ma, "\"mariadbId\" = ?", *m.MariaDBID).Error == nil {
			return ma.AppName, ma.ServerID
		}
	}
	if m.MongoID != nil {
		var mo schema.Mongo
		if h.DB.First(&mo, "\"mongoId\" = ?", *m.MongoID).Error == nil {
			return mo.AppName, mo.ServerID
		}
	}
	if m.RedisID != nil {
		var re schema.Redis
		if h.DB.First(&re, "\"redisId\" = ?", *m.RedisID).Error == nil {
			return re.AppName, re.ServerID
		}
	}
	return "", nil
}

// getMountBasePath 根据 mount 关联的服务类型返回基础路径
func (h *Handler) getMountBasePath(m *schema.Mount) string {
	if h.Config == nil {
		return "/etc/dokploy/applications"
	}
	if m.ComposeID != nil {
		return h.Config.Paths.ComposePath
	}
	return h.Config.Paths.ApplicationsPath
}
