# middleware

Echo HTTP 中间件，提供请求级认证和权限验证。
从 Cookie/Header 提取凭证，调用 auth 模块验证后将 User/Session 注入请求上下文。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| auth.go | 输入: HTTP 请求 (Cookie/x-api-key) / 输出: echo.Context 中注入 User 和 Session | 认证中间件链：提取 token → 验证 → 设置上下文；提供 GetUser/GetSession 辅助函数 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
