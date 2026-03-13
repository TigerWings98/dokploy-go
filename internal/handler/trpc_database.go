// Input: procedureRegistry, db (Postgres/MySQL/MariaDB/Mongo/Redis 表)
// Output: registerDatabaseTRPC - 5 种数据库服务的 tRPC procedure 注册
// Role: Database tRPC 路由注册，将 postgres/mysql/mariadb/mongo/redis.* procedure 绑定到数据库管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
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
			if err := h.DB.Create(model).Error; err != nil {
				return nil, err
			}
			return model, nil
		}

		r[d.routerName+".remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			h.DB.Exec(fmt.Sprintf("DELETE FROM \"%s\" WHERE %s = ?", tableName, quotedID), id)
			return true, nil
		}

		r[d.routerName+".update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			delete(in, d.idField)
			// 过滤不属于当前数据库表的字段（前端共享 mutation 会发送所有 ID）
			in = h.filterColumns(d.newModel(), in)
			h.DB.Table(tableName).Where(quotedID+" = ?", id).Updates(in)
			return true, nil
		}

		r[d.routerName+".deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			if h.Queue != nil {
				if _, err := h.Queue.EnqueueDeployDatabase(id, d.routerName); err != nil {
					return nil, err
				}
			}
			return true, nil
		}
		r[d.routerName+".start"] = r[d.routerName+".deploy"]
		r[d.routerName+".reload"] = r[d.routerName+".deploy"]

		r[d.routerName+".stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			if h.Queue != nil {
				if _, err := h.Queue.EnqueueStopDatabase(id, d.routerName); err != nil {
					return nil, err
				}
			}
			return true, nil
		}

		r[d.routerName+".rebuild"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[d.idField].(string)
			if h.Queue != nil {
				if _, err := h.Queue.EnqueueRebuildDatabase(id, d.routerName); err != nil {
					return nil, err
				}
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
