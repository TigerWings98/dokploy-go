# auth

Session 和 API Key 认证验证。
兼容原 TS 版 Better Auth 的 session 表和 apikey 表，提供请求级用户身份解析。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| auth.go | 输入: session token 或 API key / 输出: User + Session 或错误 | 从数据库验证 session/API key 并返回关联用户 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
