# 前端 API 契约

## 1. 模块概述

Dokploy 前后端通过 tRPC v11 实现类型安全的 API 通信。前端（Next.js）和后端（Node.js）共享同一套 TypeScript 类型定义，在编译时即可检测 API 调用错误。通信支持三种模式：HTTP 批量请求、HTTP 单请求（FormData 上传）、WebSocket 订阅。

tRPC 是 TypeScript 生态专有的 RPC 框架，它的核心价值在于消除前后端之间的 API 定义重复。后端的路由定义直接成为前端的类型来源，不需要 OpenAPI 规范、不需要代码生成。

核心数据流：
```
前端 React 组件
    | api.{router}.{procedure}.useQuery() / useMutation()
tRPC Client（自动生成类型安全调用）
    | HTTP / WebSocket
tRPC Server（路由 + 中间件 + 验证）
    |
服务层（业务逻辑）
    |
数据库 / Docker / SSH
```

## 2. 设计详解

### 2.1 tRPC 服务端初始化

#### 2.1.1 上下文（Context）

**源文件**: `apps/dokploy/server/api/trpc.ts`

每个请求创建独立的上下文对象：

```typescript
interface CreateContextOptions {
    user: (User & {
        role: "member" | "admin" | "owner";
        ownerId: string;
        enableEnterpriseFeatures: boolean;
        isValidEnterpriseLicense: boolean;
    }) | null;
    session: (Session & {
        activeOrganizationId: string;
        impersonatedBy?: string;
    }) | null;
    req: CreateNextContextOptions["req"];
    res: CreateNextContextOptions["res"];
}

const createInnerTRPCContext = (opts: CreateContextOptions) => ({
    session: opts.session,
    db,                    // PostgreSQL 数据库实例
    req: opts.req,
    res: opts.res,
    user: opts.user,
});
```

上下文创建流程：
1. 接收 Next.js 的 `req`/`res`
2. 调用 `validateRequest(req)` -- 从 Cookie 或 `x-api-key` 请求头解析身份
3. 组装上下文对象，包含 `session`、`user`、`db` 实例

#### 2.1.2 序列化

使用 `superjson` 作为 transformer，支持 Date、BigInt、Map、Set 等 JavaScript 原生类型的序列化/反序列化。前后端必须使用相同的 transformer。

这是 Go 重写的关键兼容点：superjson 会将 `Date` 对象序列化为特殊格式 `{ json: "2024-01-01T00:00:00.000Z", meta: { values: { "date": ["Date"] } } }`，而非标准 JSON。

#### 2.1.3 错误格式化

Zod 验证错误会被扁平化后返回：
```typescript
errorFormatter({ shape, error }) {
    return {
        ...shape,
        data: {
            ...shape.data,
            zodError: error.cause instanceof ZodError ? error.cause.flatten() : null,
        },
    };
}
```

标准 tRPC 错误响应结构：
```json
{
    "error": {
        "message": "...",
        "code": -32600,
        "data": {
            "code": "BAD_REQUEST",
            "httpStatus": 400,
            "zodError": { "fieldErrors": {...}, "formErrors": [...] }
        }
    }
}
```

#### 2.1.4 OpenAPI 元数据

使用 `@dokploy/trpc-openapi` 为每个 procedure 附加 OpenAPI 元数据，支持生成 OpenAPI 规范文档。这为 Go 重写提供了 API 参考。

### 2.2 权限中间件（Procedure 类型）

| Procedure | 认证要求 | 角色要求 | 额外检查 | 使用场景 |
|-----------|---------|---------|---------|---------|
| `publicProcedure` | 无 | 无 | 无 | 健康检查、monitoring 回调 |
| `protectedProcedure` | session + user 非空 | 任意 | 无 | 大多数读写操作 |
| `adminProcedure` | session + user 非空 | owner 或 admin | 无 | 管理操作（通知、设置等） |
| `cliProcedure` | 同 adminProcedure | owner 或 admin | 无 | CLI 专用 |
| `enterpriseProcedure` | session + user 非空 | owner 或 admin | 有效企业许可证 | SSO 等企业功能 |

中间件实现模式（以 `protectedProcedure` 为例）：

```typescript
export const protectedProcedure = t.procedure.use(({ ctx, next }) => {
    if (!ctx.session || !ctx.user) {
        throw new TRPCError({ code: "UNAUTHORIZED" });
    }
    return next({
        ctx: {
            session: ctx.session,   // 类型收窄为非空
            user: ctx.user,
        },
    });
});
```

中间件链：
```
publicProcedure:     输入验证 -> 处理器
protectedProcedure:  认证检查 -> 输入验证 -> 处理器
adminProcedure:      认证检查 -> 角色检查(owner/admin) -> 输入验证 -> 处理器
enterpriseProcedure: 认证检查 -> 角色检查 -> 许可证检查 -> 输入验证 -> 处理器
```

### 2.3 路由注册

**源文件**: `apps/dokploy/server/api/root.ts`

共注册 43 个子路由（41 个主路由 + 2 个 proprietary）：

```typescript
export const appRouter = createTRPCRouter({
    // 管理与用户
    admin: adminRouter,       user: userRouter,
    organization: organizationRouter,  settings: settingsRouter,

    // 项目与环境
    project: projectRouter,   environment: environmentRouter,

    // 应用与部署
    application: applicationRouter,    compose: composeRouter,
    deployment: deploymentRouter,      previewDeployment: previewDeploymentRouter,
    rollback: rollbackRouter,          patch: patchRouter,

    // 数据库服务
    mysql: mysqlRouter,       postgres: postgresRouter,
    redis: redisRouter,       mongo: mongoRouter,        mariadb: mariadbRouter,

    // 基础设施
    docker: dockerRouter,     server: serverRouter,
    cluster: clusterRouter,   swarm: swarmRouter,
    domain: domainRouter,     certificates: certificateRouter,

    // 配置
    mounts: mountRouter,      port: portRouter,
    security: securityRouter, redirects: redirectsRouter,
    registry: registryRouter, notification: notificationRouter,

    // Git 集成
    gitProvider: gitProviderRouter,    gitea: giteaRouter,
    bitbucket: bitbucketRouter,        gitlab: gitlabRouter,
    github: githubRouter,              sshKey: sshRouter,

    // 备份与运维
    backup: backupRouter,     destination: destinationRouter,
    schedule: scheduleRouter, volumeBackups: volumeBackupsRouter,

    // 企业功能
    stripe: stripeRouter,     ai: aiRouter,
    licenseKey: licenseKeyRouter,      sso: ssoRouter,
});

export type AppRouter = typeof appRouter;
```

`AppRouter` 类型是整个 API 契约的根类型，前端通过它推断所有 API 的输入/输出类型。

### 2.4 典型路由模式

#### 2.4.1 Query（读操作）

```typescript
one: protectedProcedure
    .input(apiFindOneApplication)          // Zod Schema 验证
    .query(async ({ input, ctx }) => {
        return await findApplicationById(input.applicationId);
    }),
```

HTTP: `GET /api/trpc/application.one?input={"applicationId":"xxx"}`

#### 2.4.2 Mutation（写操作）

```typescript
create: protectedProcedure
    .input(apiCreateApplication)
    .mutation(async ({ input, ctx }) => {
        // 权限检查
        const environment = await findEnvironmentById(input.environmentId);
        const project = await findProjectById(environment.projectId);
        if (project.organizationId !== ctx.session.activeOrganizationId) {
            throw new TRPCError({ code: "UNAUTHORIZED" });
        }
        return await createApplication(input);
    }),
```

HTTP: `POST /api/trpc/application.create` Body: `{ "json": { ... } }`

#### 2.4.3 输入验证 Schema 命名规范

所有输入通过 Zod Schema 验证，绝大多数与对应的表定义共存于 `packages/server/src/db/schema/*.ts` 文件中（少量定义在 `packages/server/src/db/validations/` 目录）：

```
apiCreate{Entity}     -- 创建验证
apiFindOne{Entity}    -- 查找验证（通常只含 ID）
apiUpdate{Entity}     -- 更新验证（partial）
apiDeploy{Entity}     -- 部署验证
apiSave{Feature}      -- 特定功能保存
apiTest{Channel}Connection -- 通知渠道连接测试
```

#### 2.4.4 权限检查模式

路由内的资源级权限检查遵循统一模式：

```typescript
// 1. 中间件层确保认证 (protectedProcedure / adminProcedure)
// 2. 路由内确认资源归属
const resource = await findResourceById(input.resourceId);
if (resource.organizationId !== ctx.session.activeOrganizationId) {
    throw new TRPCError({ code: "UNAUTHORIZED" });
}
// 3. member 角色额外检查细粒度权限
if (ctx.user.role === "member") {
    await checkServiceAccess(ctx.user.id, projectId, ...);
}
```

### 2.5 tRPC 客户端配置

**源文件**: `apps/dokploy/utils/api.ts`

#### 2.5.1 客户端初始化

```typescript
export const api = createTRPCNext<AppRouter>({
    config() {
        return { links };
    },
    ssr: false,             // 禁用 SSR，纯客户端调用
    transformer: superjson,
});
```

#### 2.5.2 链路配置（Links）

前端根据运行环境和操作类型选择不同的传输链路：

```typescript
const links = typeof window !== "undefined"
    ? [
        splitLink({
            condition: (op) => op.type === "subscription",
            true: wsLink({ client: wsClient, transformer: superjson }),
            false: splitLink({
                condition: (op) => op.input instanceof FormData,
                true: httpLink({ url: "/api/trpc", transformer: superjson }),
                false: httpBatchLink({ url: "/api/trpc", transformer: superjson }),
            }),
        }),
    ]
    : [httpBatchLink({ url: "/api/trpc", transformer: superjson })];
```

路由决策：
```
浏览器端:
|-- subscription 操作 -> wsLink（WebSocket）
|-- FormData 输入   -> httpLink（单请求，不批量）
|-- 其他操作        -> httpBatchLink（批量请求）

服务器端（SSR，实际未启用）:
|-- 所有操作        -> httpBatchLink
```

#### 2.5.3 WebSocket 连接

```typescript
const wsClient = createWSClient({
    url: `${protocol}${host}/drawer-logs`,
    lazy: { enabled: true, closeMs: 3000 },   // 延迟连接，3秒无活动断开
    retryDelayMs: () => 3000,                   // 断线重连间隔 3 秒
});
```

WebSocket 连接到 `/drawer-logs` 端点，采用惰性连接模式。使用单例模式 `getOrCreateWSClient()` 确保全局只有一个 WS 连接。

#### 2.5.4 类型推断工具

```typescript
export type RouterInputs = inferRouterInputs<AppRouter>;
export type RouterOutputs = inferRouterOutputs<AppRouter>;
```

前端任意位置可推断 procedure 的类型：
```typescript
type CreateAppInput = RouterInputs["application"]["create"];
type AppDetail = RouterOutputs["application"]["one"];
```

### 2.6 前端调用模式

#### 2.6.1 Query

```typescript
const { data, isLoading, error } = api.application.one.useQuery({
    applicationId: "xxx",
});
```

TanStack Query 自动管理缓存、失效、重新请求。

#### 2.6.2 Mutation

```typescript
const { mutateAsync } = api.application.create.useMutation();
await mutateAsync({ name: "my-app", environmentId: "xxx", ... });
```

#### 2.6.3 HTTP 批量请求

同一渲染周期内的多个 query 会被自动批量合并：
```
GET /api/trpc/application.one,project.all,settings.one?batch=1&input=...
```

### 2.7 tRPC HTTP 协议细节

这些细节对 Go 重写维护兼容性至关重要。

#### 2.7.1 Query 请求格式

```
GET /api/trpc/{router}.{procedure}?input={superjson_encoded_input}
```

input 参数是 superjson 编码后的 URL-encoded JSON。

#### 2.7.2 Mutation 请求格式

```
POST /api/trpc/{router}.{procedure}
Content-Type: application/json

{ "json": { /* input data */ }, "meta": { /* superjson metadata */ } }
```

#### 2.7.3 Batch 请求格式

```
GET /api/trpc/{p1},{p2},{p3}?batch=1&input={"0":...,"1":...,"2":...}
```

响应是数组：
```json
[
    { "result": { "data": { "json": ..., "meta": ... } } },
    { "result": { "data": { "json": ..., "meta": ... } } },
    ...
]
```

#### 2.7.4 成功响应格式

```json
{
    "result": {
        "data": {
            "json": { /* actual response data */ },
            "meta": { /* superjson type metadata */ }
        }
    }
}
```

#### 2.7.5 错误响应格式

```json
{
    "error": {
        "message": "Error message",
        "code": -32600,
        "data": {
            "code": "BAD_REQUEST",
            "httpStatus": 400,
            "path": "application.create",
            "zodError": null
        }
    }
}
```

tRPC 错误码映射：

| tRPC Code | HTTP Status | JSON-RPC Code |
|-----------|-------------|---------------|
| UNAUTHORIZED | 401 | -32001 |
| FORBIDDEN | 403 | -32003 |
| NOT_FOUND | 404 | -32004 |
| BAD_REQUEST | 400 | -32600 |
| INTERNAL_SERVER_ERROR | 500 | -32603 |

### 2.8 非 tRPC 的 WebSocket 处理器

除 tRPC subscription 外，Dokploy 有 6 个独立的 WebSocket 处理器（非 tRPC 协议）：

| 处理器 | 路径 | 认证方式 | 用途 |
|--------|------|---------|------|
| `setupDrawerLogsWebSocketServer` | `/drawer-logs` | Cookie session | 抽屉面板日志（也用于 tRPC WS） |
| `setupDeploymentLogsWebSocketServer` | `/deployment-logs` | Cookie session | 部署日志流 |
| `setupDockerContainerLogsWebSocketServer` | `/container-logs` | Cookie session | 容器日志 |
| `setupDockerContainerTerminalWebSocketServer` | `/container-terminal` | Cookie session | 容器终端 |
| `setupTerminalWebSocketServer` | `/terminal` | Cookie session | 服务器终端 |
| `setupDockerStatsMonitoringSocketServer` | `/docker-stats` | Cookie session | Docker 统计（仅自托管） |

这些 WebSocket 不使用 tRPC 协议，而是直接传输原始消息（日志文本、终端数据）。

### 2.9 非 tRPC 的 HTTP 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/api/auth/[...all]` | GET/POST | Better Auth 认证路由 |
| `/api/deploy/[webhookId]` | POST | Git Webhook 触发部署 |
| `/api/providers/[provider]` | GET | Git OAuth 回调 |
| `/api/stripe/webhook` | POST | Stripe 支付 Webhook |

## 3. 源文件清单

| 文件 | 说明 |
|------|------|
| `apps/dokploy/server/api/root.ts` | appRouter 路由注册，定义 AppRouter 类型 |
| `apps/dokploy/server/api/trpc.ts` | tRPC 初始化、上下文创建、5 种 Procedure 中间件 |
| `apps/dokploy/utils/api.ts` | tRPC 客户端配置、链路选择、WebSocket 连接 |
| `apps/dokploy/server/api/routers/*.ts` | 43 个路由文件（41 个主路由 + 2 个 proprietary） |
| `apps/dokploy/pages/api/trpc/[trpc].ts` | tRPC Next.js API Route 适配器 |
| `apps/dokploy/server/server.ts` | HTTP 服务器 + WebSocket 处理器注册 |
| `apps/dokploy/server/wss/*.ts` | 6 个 WebSocket 处理器 |
| `packages/server/src/db/schema/*.ts` | 数据库 Schema + Zod 验证 Schema（API 输入/输出类型来源） |
| `packages/server/src/db/validations/*.ts` | 少量额外 Zod 验证 Schema（domain.ts, index.ts） |
| `packages/server/src/lib/auth.ts` | 认证配置（validateRequest 函数） |

## 4. 对外接口

### 4.1 tRPC API 完整路由表

共 43 个顶级路由，约 200+ 个 procedure。核心路由的 procedure 列表：

| 路由 | 主要 Query | 主要 Mutation |
|------|-----------|-------------|
| `admin` | one | update, cleanAll, cleanDockerBuilder, cleanMonitoring, cleanSSHKeys, cleanUnusedImages, cleanUnusedVolumes |
| `application` | one | create, delete, deploy, redeploy, reload, start, stop, update, saveBuildType, saveEnvironment, refreshToken |
| `compose` | one | create, createByTemplate, delete, deploy, redeploy, update, start, stop, fetchServices |
| `project` | all, one | create, update, remove, duplicate |
| `environment` | all, one | create, update, remove, duplicate |
| `deployment` | all, allByApplication, allByCompose, allCentralized | cancelQueued, cancelRunning |
| `notification` | all, one, getEmailProviders | create{Channel}, update{Channel}, test{Channel}Connection, remove |
| `server` | all, one | create, update, remove, setup, validateServer |
| `backup` | all, one | create, update, delete, manualBackup, getFiles, deleteFile, restore |
| `settings` | health | updateSettingsApp, saveSSHKey, generateSSHKey, assignDomainServer |
| `docker` | getContainers, getConfig, getContainersByAppName | - |

### 4.2 API 端点入口

```
tRPC:
  POST /api/trpc/{router}.{procedure}      <- mutation
  GET  /api/trpc/{router}.{procedure}      <- query
  GET  /api/trpc/{p1},{p2},...             <- batch query
  WS   /drawer-logs                        <- subscription

非 tRPC:
  /api/auth/[...all]       <- Better Auth
  /api/deploy/[webhookId]  <- Git Webhook
  /api/providers/[provider] <- Git OAuth
  /api/stripe/webhook      <- Stripe
```

### 4.3 前端类型推断

```typescript
// 全局类型
export type AppRouter = typeof appRouter;
export type RouterInputs = inferRouterInputs<AppRouter>;
export type RouterOutputs = inferRouterOutputs<AppRouter>;

// 具体类型使用
type CreateAppInput = RouterInputs["application"]["create"];
// => { name: string; environmentId: string; ... }

type AppDetail = RouterOutputs["application"]["one"];
// => { applicationId: string; name: string; appName: string; ... }
```

## 5. 依赖关系

### 上游依赖

```
前端 API 契约依赖:
|-- @trpc/client v11      <- tRPC 客户端
|-- @trpc/next            <- Next.js 集成
|-- @trpc/server v11      <- tRPC 服务端
|-- superjson             <- 序列化（前后端共用）
|-- zod                   <- 输入验证 Schema
|-- @tanstack/react-query <- 数据缓存和状态管理
|-- better-auth           <- 认证（上下文中的 session/user）
|-- drizzle-orm           <- 数据库（上下文中的 db，类型推断来源）
|-- @dokploy/trpc-openapi <- OpenAPI 规范生成
|-- ws                    <- WebSocket 服务端
```

### 下游被依赖

```
前端所有页面和组件都通过 api 对象访问后端:
|-- apps/dokploy/components/ (所有 React 组件)
|-- apps/dokploy/pages/ (所有页面)
|-- apps/dokploy/hooks/ (自定义 Hooks)
```

## 6. Go 重写注意事项

### 6.1 核心挑战：tRPC 无法在 Go 中使用

tRPC 是 TypeScript 生态专有的 RPC 框架，Go 没有等价实现。重写后端意味着必须选择新的 API 协议，并且前端需要相应修改。

### 6.2 方案对比

| 方案 | 说明 | 类型安全 | 前端改动量 |
|------|------|---------|----------|
| **REST API + OpenAPI**（推荐） | Go HTTP 框架 + OpenAPI 规范 + openapi-fetch | 代码生成 | 中等 |
| **tRPC 协议兼容** | Go 实现 tRPC HTTP 协议 + superjson | 保持原有 | 最小 |
| **gRPC + grpc-web** | Protocol Buffers 定义 API | 原生 | 大 |
| **Connect-Go** | 兼容 gRPC 但支持 JSON/HTTP | connect-web | 中等 |

### 6.3 推荐方案：REST API + OpenAPI

1. **API 定义**: 使用 OpenAPI 3.0 规范描述所有 API，可基于现有 `@dokploy/trpc-openapi` 生成的规范作为起点
2. **Go 服务端**: 使用 Echo/Chi + `oapi-codegen` 或手写路由
3. **路径映射**: `POST /api/v1/{router}/{procedure}` 对应 tRPC 的 `{router}.{procedure}`
4. **前端客户端**: 使用 `openapi-fetch` 或 `orval` 从 OpenAPI 规范自动生成类型安全客户端
5. **输入验证**: 使用 `go-playground/validator` 替代 Zod
6. **序列化**: 标准 JSON（放弃 superjson，Date 统一使用 ISO 8601 字符串）

### 6.4 tRPC 协议兼容方案（最小前端改动）

如果希望最小化前端改动，可以在 Go 中实现 tRPC HTTP 协议兼容层：

1. 解析 `GET /api/trpc/{procedure}?input=...` 格式的 query 请求
2. 解析 `POST /api/trpc/{procedure}` 格式的 mutation 请求
3. 支持 `?batch=1` 批量请求
4. 实现 superjson 兼容的序列化/反序列化
5. 返回 `{ result: { data: { json: ..., meta: ... } } }` 格式的响应
6. 返回 `{ error: { message: ..., code: ..., data: { code: ..., httpStatus: ... } } }` 格式的错误

难点在于 superjson 的完整实现，需要处理 Date、BigInt、Map、Set 等类型的元数据。

### 6.5 WebSocket 迁移

- tRPC subscription 替换为标准 WebSocket 或 SSE
- 6 个独立 WebSocket 处理器可直接用 Go 的 `gorilla/websocket` 或 `nhooyr.io/websocket` 重写
- 前端 WebSocket 连接代码改动较小，主要是消息格式适配
- 关键点：保持相同的 URL 路径（`/drawer-logs`, `/deployment-logs` 等）

### 6.6 保持前端兼容的过渡策略

1. **阶段一**: Go 后端实现 REST API，同时运行 Node.js tRPC 作为代理层
2. **阶段二**: 前端逐步从 `api.xxx.useQuery()` 迁移到 `useQuery()` + fetch
3. **阶段三**: 移除 Node.js 代理层，前端完全对接 Go 后端
4. 保持相同的 JSON 响应结构（字段名不变），减少前端改动
5. 使用 OpenAPI 规范文件作为前后端契约的单一事实来源

### 6.7 语言无关可复用的部分

| 可复用项 | 说明 |
|---------|------|
| API 路由命名 | 43 个路由名和约 200 个 procedure 名可保持不变 |
| Zod Schema 对应的验证规则 | 字段名、类型、约束可直接转为 Go struct tag |
| 错误码定义 | UNAUTHORIZED/FORBIDDEN/NOT_FOUND/BAD_REQUEST 等对应 HTTP 状态码 |
| WebSocket 路径 | 6 个 WebSocket 端点路径不变 |
| 非 tRPC API 路径 | `/api/auth/`, `/api/deploy/`, `/api/providers/` 路径不变 |
