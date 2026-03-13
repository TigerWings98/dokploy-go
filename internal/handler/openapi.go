// Input: echo, procedure registry
// Output: OpenAPI REST 兼容层，暴露 /api/{procedure} 格式的 REST 端点
// Role: 兼容 TS 版 trpc-openapi，让外部工具（Swagger/GitHub Actions/curl）通过标准 REST 调用 tRPC procedure
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// HandleOpenAPI 处理 /api/{procedure} 格式的 REST 请求。
// 与 TS 版 @dokploy/trpc-openapi 的行为一致：
//   - query procedure → GET /api/{name}?input={json}
//   - mutation procedure → POST /api/{name} (body: plain JSON, 非 tRPC 包装)
//   - 响应：直接返回结果 JSON（不包裹在 tRPC {result:{data:{json:...}}} 中）
//   - 认证：通过 x-api-key header（API Key）或 session cookie
func (h *Handler) HandleOpenAPI(c echo.Context) error {
	// 从路径提取 procedure 名称
	// /api/compose.one → compose.one
	// /api/settings.health → settings.health
	path := c.Request().URL.Path
	procedure := strings.TrimPrefix(path, "/api/")
	if procedure == "" || procedure == path {
		return c.JSON(http.StatusNotFound, map[string]string{"message": "No procedure specified"})
	}

	// 提取输入参数
	var input json.RawMessage
	if c.Request().Method == http.MethodGet || c.Request().Method == http.MethodDelete {
		// OpenAPI GET/DELETE: 先检查 ?input={json} 格式，再回退到直接 query 参数映射
		// TS 版 trpc-openapi 对 GET 请求直接从 query 参数映射到 input 对象
		// 例如 ?composeId=xxx → {"composeId": "xxx"}
		inputStr := c.QueryParam("input")
		if inputStr != "" {
			decoded, err := url.QueryUnescape(inputStr)
			if err != nil {
				decoded = inputStr
			}
			input = json.RawMessage(decoded)
		} else {
			// 将所有 query 参数直接映射为 JSON 对象
			// TS 版 trpc-openapi 会根据 Zod schema 自动强转类型，
			// Go 版无 schema 信息，使用启发式方法：数字 → number，布尔 → bool，其余保持 string
			params := c.QueryParams()
			if len(params) > 0 {
				obj := make(map[string]interface{}, len(params))
				for key, values := range params {
					if len(values) == 1 {
						obj[key] = coerceQueryValue(values[0])
					} else {
						obj[key] = values
					}
				}
				input, _ = json.Marshal(obj)
			}
		}
	} else {
		// POST/PUT/PATCH: body 就是直接的 JSON input（不是 tRPC 的 {json: ...} 格式）
		var body json.RawMessage
		if err := json.NewDecoder(c.Request().Body).Decode(&body); err == nil && len(body) > 0 {
			input = body
		}
	}

	// 调用 procedure
	result, err := h.callProcedure(c, procedure, input)
	if err != nil {
		if te, ok := err.(*trpcErr); ok {
			return c.JSON(te.status, map[string]interface{}{
				"message": te.message,
				"code":    te.code,
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"message": err.Error(),
			"code":    "INTERNAL_SERVER_ERROR",
		})
	}

	return c.JSON(http.StatusOK, result)
}

// GenerateOpenAPIDocument 生成 OpenAPI 3.0 文档，列出所有已注册的 procedure。
// 与 TS 版 generateOpenApiDocument 行为一致。
func (h *Handler) GenerateOpenAPIDocument() map[string]interface{} {
	registry := h.buildRegistry()

	// 已知的 query procedure（GET 方法）前缀/名称
	// tRPC 中 query → GET，mutation → POST
	queryProcedures := map[string]bool{}
	mutationProcedures := map[string]bool{}

	for name := range registry {
		// 判断是 query 还是 mutation：
		// 常见规则：get/one/all/list/by/read/check/have/load/show → query
		// 其余 → mutation
		parts := strings.Split(name, ".")
		action := ""
		if len(parts) > 1 {
			action = strings.ToLower(parts[len(parts)-1])
		}
		if isQueryAction(action) {
			queryProcedures[name] = true
		} else {
			mutationProcedures[name] = true
		}
	}

	paths := map[string]interface{}{}

	// 按名称排序
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		path := "/" + name
		tag := strings.Split(name, ".")[0]

		method := "post"
		if queryProcedures[name] {
			method = "get"
		}

		operation := map[string]interface{}{
			"operationId": name,
			"tags":        []string{tag},
			"security":    []map[string][]string{{"apiKey": {}}},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Successful response",
				},
			},
		}

		if method == "get" {
			operation["parameters"] = []map[string]interface{}{
				{
					"name":     "input",
					"in":       "query",
					"required": false,
					"schema":   map[string]string{"type": "string"},
				},
			}
		} else {
			operation["requestBody"] = map[string]interface{}{
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]string{"type": "object"},
					},
				},
			}
		}

		paths[path] = map[string]interface{}{
			method: operation,
		}
	}

	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "Dokploy API",
			"description": "Endpoints for dokploy",
			"version":     "v0.28.5",
		},
		"paths": paths,
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"apiKey": map[string]interface{}{
					"type": "apiKey",
					"in":   "header",
					"name": "x-api-key",
				},
			},
		},
		"security": []map[string][]string{
			{"apiKey": {}},
		},
	}
}

// coerceQueryValue 对 query 参数值做类型强转，模拟 TS 版 trpc-openapi 的行为。
// 规则：纯数字 → number（int 或 float），"true"/"false" → bool，其余保持 string。
// 注意：nanoid 等 ID 字段虽然可能以数字开头，但包含非数字字符，不会被误转。
func coerceQueryValue(s string) interface{} {
	// 布尔值
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	// 空字符串保持 string
	if s == "" {
		return s
	}
	// 尝试整数（避免 nanoid 等含字母的 ID 被误转）
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	// 尝试浮点数
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// isQueryAction 判断 procedure action 是否为 query（GET）类型
func isQueryAction(action string) bool {
	queryPrefixes := []string{
		"get", "one", "all", "list", "by", "read", "check", "have",
		"load", "show", "find", "fetch", "search", "count", "health",
		"templates", "tags", "session",
	}
	for _, prefix := range queryPrefixes {
		if strings.HasPrefix(action, prefix) || action == prefix {
			return true
		}
	}
	// 以 ById, ByXxx 结尾的也是 query
	if strings.Contains(action, "By") {
		return true
	}
	return false
}
