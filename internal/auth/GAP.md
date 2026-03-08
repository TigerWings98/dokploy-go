# auth 模块 Gap 分析

## Go 当前实现 (95 行, 1 文件)
- `ValidateSession`: 通过 token 查 session 表 → 查 user 表，返回 User + Session
- `ValidateAPIKey`: 通过 key 查 apikey 表 → 检查 enabled → 查 user 表，返回 User
- `GetSessionTokenFromRequest`: 从 Cookie (`better-auth.session_token`) 或 Authorization Bearer 提取 token
- `GetAPIKeyFromRequest`: 从 `x-api-key` header 提取 API key

## TS 原版实现 (Better Auth 框架)
- Session 验证 + 组织成员查询 + role/ownerId 注入
- API Key 验证 + 构造 mock session (含 activeOrganizationId)
- 用户注册前钩子 (邀请令牌验证/首个用户检查)
- 用户注册后钩子 (自动创建组织/更新 serverIp)
- Session 创建钩子 (设置 activeOrganizationId)
- OAuth 流程 (GitHub/Google)
- 2FA TOTP 验证 + 备份码
- SSO (OIDC/SAML)
- 密码注册/登录 (bcrypt 10轮)
- Cookie 配置 (自托管 lax vs 云模式 secure)

## Gap 详情

### 已实现 ✅
1. Session token 验证
2. API Key 验证 (含 enabled 检查)
3. Cookie/Header token 提取
4. 兼容 Better Auth cookie 名 (`better-auth.session_token`)

### 缺失功能 ❌
1. **validateRequest 中的组织成员查询**: TS 版在验证后会查 member 表获取 role/ownerId/activeOrganizationId，Go 版跳过了此步
   - 影响：Go 端 middleware 层有做部分补偿（在 trpc_routes.go 的 getDefaultMember 中）
2. **用户注册流程**: 注册前/后钩子完全缺失
   - 注册前：邀请令牌验证、首个用户检查
   - 注册后：自动创建组织、设置 serverIp
   - 影响：注册功能依赖原 TS 版 Better Auth 端点，Go 端未自行实现
3. **密码登录**: Go 端未实现 bcrypt 密码验证和登录端点
   - 影响：登录功能依赖原 TS 版
4. **OAuth 流程**: GitHub/Google OAuth 未实现
5. **2FA/TOTP**: 未实现
6. **SSO**: 未实现 (但有 sso handler stub)
7. **Session 过期检查**: Go 端未检查 session.expiresAt
8. **API Key 过期/速率限制**: 未实现 expiresAt 和 rateLimitPerDay 检查

### 设计差异 ⚠️
1. Go 端认证端点 (`handler/auth.go`) 实现了 Better Auth 兼容的注册/登录端点，但逻辑在 handler 层而非 auth 模块
2. TS 版 validateRequest 返回 user 对象附带 role/ownerId 等扩展字段，Go 版通过 middleware 中单独查询 member 补偿

## 影响评估
- **严重度**: 中等。核心的 Session/API Key 验证已可用，但注册/登录/OAuth/2FA 需要完整实现才能脱离 TS 版独立运行。
- **当前状态**: Go 端可以在已有 TS 版创建的 session/用户数据上正常工作，但不能独立完成用户注册和登录。
