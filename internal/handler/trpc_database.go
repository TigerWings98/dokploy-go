// Input: procedureRegistry, db (Postgres/MySQL/MariaDB/Mongo/Redis 表)
// Output: registerDatabaseTRPC - 5 种数据库服务的 tRPC procedure 注册
// Role: Database tRPC 路由注册，将 postgres/mysql/mariadb/mongo/redis.* procedure 绑定到数据库管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerDatabaseTRPC(r procedureRegistry) {
	type dbDef struct {
		routerName  string
		modelPrefix string
		idField     string
		newModel    func() interface{}
	}

	dbs := []dbDef{
		{"postgres", "Postgres", "postgresId", func() interface{} { return &schema.Postgres{} }},
		{"mysql", "MySql", "mysqlId", func() interface{} { return &schema.MySQL{} }},
		{"mariadb", "Mariadb", "mariadbId", func() interface{} { return &schema.MariaDB{} }},
		{"mongo", "Mongo", "mongoId", func() interface{} { return &schema.Mongo{} }},
		{"redis", "Redis", "redisId", func() interface{} { return &schema.Redis{} }},
	}

	for _, d := range dbs {
		d := d
		tableName := strings.ToLower(d.modelPrefix)
		quotedID := fmt.Sprintf("\"%s\"", d.idField)

		r[d.routerName+".one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			model := d.newModel()
			if err := h.findDatabaseService(model, d.idField, id); err != nil {
				return nil, &trpcErr{d.modelPrefix + " not found", "NOT_FOUND", 404}
			}
			return model, nil
		}

		r[d.routerName+".create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			model := d.newModel()
			json.Unmarshal(input, model)

			// 自动生成空密码（与 TS 版 generatePassword() 一致）
			h.ensureDatabasePasswords(model)

			if err := h.DB.Create(model).Error; err != nil {
				return nil, err
			}
			return model, nil
		}

		r[d.routerName+".remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)

			// 1. 查找数据库服务（需要 appName 和 server 信息来清理 Docker）
			model := d.newModel()
			h.DB.Preload("Server").Preload("Server.SSHKey").
				Where(quotedID+" = ?", id).First(model)

			appName, serverID := h.extractDBServiceInfo(model)

			// 2. 移除 Docker Swarm 服务
			if appName != "" {
				cmd := fmt.Sprintf("docker service rm %s 2>/dev/null || true", appName)
				if serverID != nil {
					var server schema.Server
					if h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", *serverID).Error == nil && server.SSHKey != nil {
						conn := process.SSHConnection{
							Host:       server.IPAddress,
							Port:       server.Port,
							Username:   server.Username,
							PrivateKey: server.SSHKey.PrivateKey,
						}
						process.ExecAsyncRemote(conn, cmd, nil)
					}
				} else if h.Docker != nil {
					h.Docker.RemoveService(context.Background(), appName)
				}
			}

			// 3. 取消关联的备份定时任务（Redis 除外）
			if d.routerName != "redis" {
				var backups []schema.Backup
				h.DB.Where(fmt.Sprintf("%s = ?", quotedID), id).Find(&backups)
				if h.Queue != nil {
					for _, b := range backups {
						h.Queue.CancelJobsByFilter("backupId", b.BackupID)
					}
				}
			}

			// 4. 删除 Traefik 配置
			if appName != "" && h.Traefik != nil {
				h.Traefik.RemoveApplicationConfig(appName)
			}

			// 5. 删除数据库记录
			h.DB.Exec(fmt.Sprintf("DELETE FROM \"%s\" WHERE %s = ?", tableName, quotedID), id)
			return true, nil
		}

		r[d.routerName+".update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			delete(in, d.idField)
			// 与 TS 版一致：禁止修改 appName 和 serverId
			delete(in, "appName")
			delete(in, "serverId")
			// 排除自动生成的不可变字段
			delete(in, "createdAt")
			delete(in, "organizationId")
			// 过滤不属于当前数据库表的字段（前端共享 mutation 会发送所有 ID）
			in = h.filterColumns(d.newModel(), in)
			h.DB.Table(tableName).Where(quotedID+" = ?", id).Updates(in)
			return true, nil
		}

		// 与 TS 版对齐：数据库 deploy/stop/rebuild 内联执行，不走队列
		r[d.routerName+".deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			if h.DBSvc != nil {
				go h.DBSvc.DeployByType(id, d.routerName, nil)
			}
			return true, nil
		}

		r[d.routerName+".start"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			if h.DBSvc != nil {
				go h.DBSvc.StartDatabase(id, schema.DatabaseType(d.routerName))
			}
			return true, nil
		}

		r[d.routerName+".reload"] = r[d.routerName+".deploy"]

		r[d.routerName+".stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			if h.DBSvc != nil {
				go h.DBSvc.StopDatabase(id, schema.DatabaseType(d.routerName))
			}
			return true, nil
		}

		r[d.routerName+".rebuild"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			if h.DBSvc != nil {
				go h.DBSvc.RebuildDatabase(id, schema.DatabaseType(d.routerName))
			}
			return true, nil
		}

		r[d.routerName+".saveEnvironment"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			env, _ := in["env"].(string)
			h.DB.Table(tableName).Where(quotedID+" = ?", id).Update("env", env)
			return true, nil
		}

		r[d.routerName+".saveExternalPort"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			port := in["externalPort"]
			h.DB.Table(tableName).Where(quotedID+" = ?", id).Update("\"externalPort\"", port)
			return true, nil
		}

		r[d.routerName+".changeStatus"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			status, _ := in["applicationStatus"].(string)
			h.DB.Table(tableName).Where(quotedID+" = ?", id).Update("\"applicationStatus\"", status)
			return true, nil
		}

		r[d.routerName+".move"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			envID, _ := in["environmentId"].(string)
			h.DB.Table(tableName).Where(quotedID+" = ?", id).
				Update("\"environmentId\"", envID)
			return true, nil
		}

		r[d.routerName+".search"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in struct {
				Query string `json:"query"`
			}
			json.Unmarshal(input, &in)
			var results []map[string]interface{}
			h.DB.Table(tableName).
				Where("name ILIKE ?", "%"+in.Query+"%").
				Order("\"createdAt\" DESC").
				Find(&results)
			if results == nil {
				results = []map[string]interface{}{}
			}
			return results, nil
		}
	}
}

// ensureDatabasePasswords 为空密码字段自动生成随机密码（与 TS 版 generatePassword() 一致）
func (h *Handler) ensureDatabasePasswords(model interface{}) {
	switch m := model.(type) {
	case *schema.Postgres:
		if m.DatabasePassword == "" {
			m.DatabasePassword = generatePassword(16)
		}
	case *schema.MySQL:
		if m.DatabasePassword == "" {
			m.DatabasePassword = generatePassword(16)
		}
		if m.DatabaseRootPassword == "" {
			m.DatabaseRootPassword = generatePassword(16)
		}
	case *schema.MariaDB:
		if m.DatabasePassword == "" {
			m.DatabasePassword = generatePassword(16)
		}
		if m.DatabaseRootPassword == "" {
			m.DatabaseRootPassword = generatePassword(16)
		}
	case *schema.Mongo:
		if m.DatabasePassword == "" {
			m.DatabasePassword = generatePassword(16)
		}
	case *schema.Redis:
		if m.DatabasePassword == "" {
			m.DatabasePassword = generatePassword(16)
		}
	}
}

// extractDBServiceInfo 从数据库模型中提取 appName 和 serverID（用于清理 Docker 服务）
func (h *Handler) extractDBServiceInfo(model interface{}) (appName string, serverID *string) {
	switch m := model.(type) {
	case *schema.Postgres:
		return m.AppName, m.ServerID
	case *schema.MySQL:
		return m.AppName, m.ServerID
	case *schema.MariaDB:
		return m.AppName, m.ServerID
	case *schema.Mongo:
		return m.AppName, m.ServerID
	case *schema.Redis:
		return m.AppName, m.ServerID
	}
	return "", nil
}
