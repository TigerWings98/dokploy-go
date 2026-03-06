# 认证与授权系统

## 1. 模块概述

Dokploy 使用 Better Auth 作为认证框架，支持邮箱密码、GitHub/Google OAuth、SSO、2FA 和 API Key 认证。采用基于组织（Organization）的多租户模型，通过 member 表实现细粒度权限控制。

## 2. Better Auth 配置

**源文件**: `packages/server/src/lib/auth.ts`

### 2.1 核心配置

```typescript
betterAuth({
    database: drizzleAdapter(db, { provider: "pg", schema }),
    secret: BETTER_AUTH_SECRET,                    // 加密密钥
    appName: "Dokploy",
    session: {
        expiresIn: 60 * 60 * 24 * 3,              // Session 有效期 3 天
        updateAge: 60 * 60 * 24,                   // 每天刷新一次
    },
})
```

### 2.2 Cookie 配置

**自托管模式**（IS_CLOUD = false）:
```
sameSite: "lax"
secure: false        # 允许 HTTP
httpOnly: true
path: "/"
```

**云模式**: 使用 Better Auth 默认的安全 Cookie 设置。

### 2.3 认证方式

#### 邮箱密码
- 密码使用 `bcrypt` 哈希（10 轮）
- 自托管模式：注册后自动登录，不要求邮箱验证
- 云模式：要求邮箱验证

#### Social OAuth
- GitHub: `GITHUB_CLIENT_ID` + `GITHUB_CLIENT_SECRET`
- Google: `GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET`

#### SSO（企业功能）
- 通过 `@better-auth/sso` 插件
- 支持 OIDC/SAML 提供商
- SSO 用户自动加入对应组织

#### 2FA
- 通过 `better-auth/plugins/twoFactor` 插件
- TOTP + 备份码

#### API Key
- 通过 `better-auth/plugins/apiKey` 插件
- 请求头 `x-api-key` 传递
- 支持速率限制和过期

### 2.4 禁用的路径

```typescript
disabledPaths: [
    "/sso/register",
    "/organization/create",
    "/organization/update",
    "/organization/delete",
]
```

组织的 CRUD 由 Dokploy 自行管理，不使用 Better Auth 的组织 API。

## 3. 用户注册流程

### 3.1 注册前钩子（user.create.before）

**自托管模式**:
1. 检查请求头 `x-dokploy-token`（邀请令牌）
2. 如果有令牌 → 通过 `getUserByToken` 验证
3. 如果无令牌且非 SSO → 检查是否已存在 owner
4. 如果已有 owner → 拒绝注册（只允许第一个用户直接注册）

**云模式**: 无限制

### 3.2 注册后钩子（user.create.after）

1. 自托管模式：更新 `webServerSettings.serverIp`
2. 云模式：提交 HubSpot 追踪
3. 自动创建组织:
   - 云模式 或 首个用户 → 创建 "My Organization" + owner member
   - SSO 用户 → 加入 SSO 提供商对应的组织（role: member）

### 3.3 Session 创建钩子（session.create.before）

- 查找用户的默认组织（isDefault=true 优先，其次按创建时间）
- 设置 `activeOrganizationId`

## 4. 请求验证流程

**源文件**: `packages/server/src/lib/auth.ts` — `validateRequest` 函数

```
请求进入
  │
  ├── 有 x-api-key 头？
  │     ├── YES → 验证 API Key
  │     │     ├── 有效 → 查找 apikey 记录 → 获取用户 → 查找组织成员 → 返回 mock session
  │     │     └── 无效 → 返回 null session
  │     │
  │     └── NO → 验证 Cookie Session
  │           ├── 有效 → 查找 member → 设置 role/ownerId/organizationId → 返回 session
  │           └── 无效 → 返回 null session
```

**返回结构**:
```typescript
{
    session: {
        userId: string,
        activeOrganizationId: string,
        impersonatedBy?: string
    },
    user: {
        id, name, email, emailVerified, image,
        role: "owner" | "member" | "admin",
        ownerId: string,                          // 组织所有者 ID
        enableEnterpriseFeatures: boolean,
        isValidEnterpriseLicense: boolean,
    }
}
```

## 5. tRPC 中间件层

**源文件**: `apps/dokploy/server/api/trpc.ts`

### 5.1 上下文（Context）

```typescript
interface TRPCContext {
    session: Session & { activeOrganizationId: string } | null;
    user: User & { role, ownerId, enableEnterpriseFeatures, isValidEnterpriseLicense } | null;
    db: Database;
    req: IncomingMessage;
    res: ServerResponse;
}
```

### 5.2 Procedure 类型

| Procedure | 认证要求 | 角色要求 | 说明 |
|-----------|---------|---------|------|
| `publicProcedure` | 无 | 无 | 公开接口 |
| `protectedProcedure` | 需登录 | 任意 | 需要有效 session |
| `cliProcedure` | 需登录 | owner/admin | CLI 工具使用 |
| `adminProcedure` | 需登录 | owner/admin | 管理员操作 |
| `enterpriseProcedure` | 需登录 | owner/admin + 有效许可证 | 企业功能 |

### 5.3 中间件实现模式

```typescript
// protectedProcedure: 验证 session 存在
if (!ctx.session || !ctx.user) {
    throw new TRPCError({ code: "UNAUTHORIZED" });
}

// adminProcedure: 额外检查角色
if (ctx.user.role !== "owner" && ctx.user.role !== "admin") {
    throw new TRPCError({ code: "UNAUTHORIZED" });
}

// enterpriseProcedure: 额外检查许可证
const hasValidLicenseResult = await hasValidLicense(ctx.session.activeOrganizationId);
if (!hasValidLicenseResult) {
    throw new TRPCError({ code: "FORBIDDEN", message: "Valid enterprise license required" });
}
```

## 6. 权限模型

### 6.1 组织级权限（member 表）

member.role 三个级别：
- **owner**: 完全控制权，可以管理组织和所有资源
- **admin**: 可以管理大部分资源，等同 owner（在当前实现中）
- **member**: 受限访问，由以下布尔权限控制

### 6.2 细粒度权限

| 权限字段 | 说明 |
|---------|------|
| canCreateProjects | 创建项目 |
| canDeleteProjects | 删除项目 |
| canCreateServices | 创建服务 |
| canDeleteServices | 删除服务 |
| canCreateEnvironments | 创建环境 |
| canDeleteEnvironments | 删除环境 |
| canAccessToDocker | 访问 Docker |
| canAccessToTraefikFiles | 访问 Traefik 配置 |
| canAccessToAPI | 访问 API |
| canAccessToSSHKeys | 访问 SSH 密钥 |
| canAccessToGitProviders | 访问 Git 提供商 |

### 6.3 资源级访问控制

| 字段 | 说明 |
|------|------|
| accessedProjects | 可访问的项目 ID 数组 |
| accessedEnvironments | 可访问的环境 ID 数组 |
| accessedServices | 可访问的服务 ID 数组 |

空数组表示可访问所有资源。

## 7. Trusted Origins（信任来源）

自托管模式下动态计算：
1. `http://{serverIp}:3000` — 服务器 IP 直接访问
2. `https://{host}` — 配置的域名
3. 开发环境额外添加 localhost
4. 从数据库加载的额外信任来源

## 8. API 路由入口

**源文件**: `apps/dokploy/pages/api/auth/[...all].ts`

Better Auth 的 handler 处理所有 `/api/auth/*` 路由：
- `/api/auth/sign-in/email` — 邮箱登录
- `/api/auth/sign-up/email` — 邮箱注册
- `/api/auth/sign-out` — 登出
- `/api/auth/session` — 获取 Session
- `/api/auth/callback/{provider}` — OAuth 回调
- `/api/auth/two-factor/*` — 2FA 相关
- `/api/auth/api-key/*` — API Key 管理
- `/api/auth/sso/*` — SSO 相关

## 9. 源文件清单

| 文件 | 说明 |
|------|------|
| `packages/server/src/lib/auth.ts` | Better Auth 核心配置 + validateRequest |
| `apps/dokploy/server/api/trpc.ts` | tRPC 上下文和中间件 |
| `apps/dokploy/pages/api/auth/[...all].ts` | Auth 路由处理器 |
| `packages/server/src/constants/index.ts` | BETTER_AUTH_SECRET, IS_CLOUD |
| `packages/server/src/db/schema/account.ts` | account, organization, member, invitation, twoFactor, apikey 表 |
| `packages/server/src/db/schema/user.ts` | user 表 |
| `packages/server/src/db/schema/session.ts` | session 表 |
| `packages/server/src/db/schema/sso.ts` | SSO 提供商配置 |
| `packages/server/src/services/admin.ts` | getUserByToken, getTrustedOrigins 等 |
| `packages/server/src/services/user.ts` | 用户 CRUD |
| `packages/server/src/verification/send-verification-email.tsx` | 邮件发送 |

## 10. Go 重写注意事项

- **Better Auth 没有 Go 版本**，需要自行实现兼容的认证系统
- 必须保持与现有前端的 Cookie 格式和 API 路径兼容
- Session 表结构和 Cookie 名称需要一致
- 推荐: 实现相同的 `/api/auth/*` REST API
- bcrypt 密码哈希: Go 中使用 `golang.org/x/crypto/bcrypt`
- OAuth 流程: Go 中使用 `golang.org/x/oauth2`
- 2FA (TOTP): Go 中使用 `github.com/pquerna/otp`
- API Key 验证: 自行实现，需兼容现有 apikey 表结构
- **关键**: activeOrganizationId 在 session 中传递的方式必须保持一致
