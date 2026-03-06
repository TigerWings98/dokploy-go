# tRPC 路由层

## 1. 模块概述

tRPC 路由层是 Dokploy 后端的 API 入口。所有前端请求通过 tRPC 协议到达路由层，路由层负责验证输入、检查权限，然后调用服务层。共有 43 个路由文件，注册在一个统一的 `appRouter` 中。

## 2. tRPC 初始化

**源文件**: `apps/dokploy/server/api/trpc.ts`

### 2.1 序列化

使用 `superjson` 作为 transformer，支持 Date、BigInt 等类型的序列化。

### 2.2 错误格式化

Zod 验证错误会被扁平化后返回给前端：

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

### 2.3 OpenAPI 元数据

使用 `@dokploy/trpc-openapi` 支持 OpenAPI 规范生成，每个 procedure 可以附加 OpenAPI 元数据。

## 3. 路由注册

**源文件**: `apps/dokploy/server/api/root.ts`

```typescript
export const appRouter = createTRPCRouter({
    // 管理
    admin: adminRouter,
    settings: settingsRouter,
    user: userRouter,
    organization: organizationRouter,

    // 项目与环境
    project: projectRouter,
    environment: environmentRouter,

    // 应用与部署
    application: applicationRouter,
    compose: composeRouter,
    deployment: deploymentRouter,
    previewDeployment: previewDeploymentRouter,
    rollback: rollbackRouter,

    // 数据库服务
    mysql: mysqlRouter,
    postgres: postgresRouter,
    redis: redisRouter,
    mongo: mongoRouter,
    mariadb: mariadbRouter,

    // 基础设施
    docker: dockerRouter,
    server: serverRouter,
    cluster: clusterRouter,
    swarm: swarmRouter,
    domain: domainRouter,
    certificates: certificateRouter,

    // 配置
    mounts: mountRouter,
    port: portRouter,
    security: securityRouter,
    redirects: redirectsRouter,
    registry: registryRouter,
    notification: notificationRouter,

    // Git 集成
    gitProvider: gitProviderRouter,
    github: githubRouter,
    gitlab: gitlabRouter,
    gitea: giteaRouter,
    bitbucket: bitbucketRouter,
    sshKey: sshRouter,

    // 运维
    backup: backupRouter,
    destination: destinationRouter,
    schedule: scheduleRouter,
    volumeBackups: volumeBackupsRouter,
    patch: patchRouter,

    // 企业功能
    stripe: stripeRouter,
    ai: aiRouter,
    licenseKey: licenseKeyRouter,
    sso: ssoRouter,
});
```

## 4. 路由文件清单与功能

### 4.1 管理类

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `admin.ts` | admin | one (query), update (mutation), cleanAll, cleanDockerBuilder, cleanMonitoring, cleanSSHKeys, cleanUnusedImages, cleanUnusedVolumes, getDokployVersion, readTraefikConfig, updateTraefikConfig, readMiddlewareTraefikConfig, updateMiddlewareTraefikConfig, readDirs, readDir, enableDashboard |
| `settings.ts` | settings | health (query), updateSettingsApp, saveSSHKey, generateSSHKey, cleanGitProviders, assignDomainServer, getUrlInfo, readAccessLogs |
| `user.ts` | user | all (query), one, update (mutation), remove, updateByAdmin |
| `organization.ts` | organization | one (query), update (mutation), getMembers |

### 4.2 项目与环境

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `project.ts` | project | all (query), one, create (mutation), update, remove, duplicate |
| `environment.ts` | environment | all (query), one, create (mutation), update, remove, duplicate |

### 4.3 应用与部署

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `application.ts` | application | one (query), create (mutation), delete, deploy, redeploy, reload, start, stop, update, saveBuildType, saveGithubProvider, saveGitlabProvider, saveBitbucketProvider, saveGiteaProvider, saveDockerProvider, saveGitProvider, saveEnvironment, refreshToken, markRunning, readAppMonitoringByAppName, update |
| `compose.ts` | compose | one (query), create, createByTemplate, delete, deploy, redeploy, update, start, stop, refreshToken, fetchServices, saveGithubProvider, saveGitlabProvider, saveBitbucketProvider, saveGiteaProvider, saveGitProvider, saveEnvironment, randomizeCompose, markRunning |
| `deployment.ts` | deployment | all (query), allByApplication, allByCompose, allByServer, allByType, allCentralized, cancelQueued, cancelRunning |
| `preview-deployment.ts` | previewDeployment | all (query), one, delete |
| `rollbacks.ts` | rollback | all (query), one, create (mutation) |

### 4.4 数据库服务

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `mysql.ts` | mysql | one (query), create (mutation), deploy, update, delete, start, stop, saveEnvironment, saveExternalPort, rebuild, changeStatus |
| `postgres.ts` | postgres | 同上 |
| `redis.ts` | redis | 同上 |
| `mongo.ts` | mongo | 同上 |
| `mariadb.ts` | mariadb | 同上 |

### 4.5 基础设施

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `docker.ts` | docker | getContainers (query), getConfig, getContainersByAppName, getContainersByAppNameRemote |
| `server.ts` | server | all (query), one, create (mutation), update, remove, setup, validateServer |
| `cluster.ts` | cluster | addWorker (mutation), removeWorker |
| `swarm.ts` | swarm | getNodes (query), getServices, getServiceInfo |
| `domain.ts` | domain | byApplication (query), byCompose, one, create (mutation), update, delete |
| `certificate.ts` | certificates | all (query), create (mutation), delete |

### 4.6 配置

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `mount.ts` | mounts | byServiceId (query), one, create (mutation), update, delete |
| `port.ts` | port | byApplicationId (query), create (mutation), delete, update |
| `security.ts` | security | byApplicationId (query), create (mutation), delete, update |
| `redirects.ts` | redirects | byApplicationId (query), create (mutation), delete, update |
| `registry.ts` | registry | all (query), one, create (mutation), update, delete, testConnection |
| `notification.ts` | notification | all (query), one, create (mutation), update, delete, test |

### 4.7 Git 集成

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `git-provider.ts` | gitProvider | all (query), one, create (mutation), update, remove |
| `github.ts` | github | all (query), one, getRepositories, getBranches |
| `gitlab.ts` | gitlab | all (query), one, getRepositories, getBranches |
| `gitea.ts` | gitea | all (query), one, getRepositories, getBranches |
| `bitbucket.ts` | bitbucket | all (query), one, getRepositories, getBranches |
| `ssh-key.ts` | sshKey | all (query), one, create (mutation), update, remove, generate |

### 4.8 运维

| 路由文件 | 路由名 | 主要 Procedures |
|---------|--------|----------------|
| `backup.ts` | backup | all (query), one, create (mutation), update, delete, manualBackup, getFiles, deleteFile, restore |
| `destination.ts` | destination | all (query), one, create (mutation), update, delete, testConnection |
| `schedule.ts` | schedule | all (query), one, create (mutation), update, delete |
| `volume-backups.ts` | volumeBackups | all (query), one, create (mutation), update, delete |
| `patch.ts` | patch | create (mutation), run |

### 4.9 企业功能

| 路由文件 | 路由名 | Procedures |
|---------|--------|-----------|
| `stripe.ts` | stripe | getPlans (query), createCheckout, getSubscription |
| `ai.ts` | ai | getModels (query), chat (mutation) |
| `proprietary/license-key.ts` | licenseKey | activate (mutation), validate, getStatus (query) |
| `proprietary/sso.ts` | sso | create (mutation), update, delete, getProviders (query) |

## 5. 典型路由模式

### 5.1 Query（查询）

```typescript
// 查找单个实体
one: protectedProcedure
    .input(apiFindOneApplication)         // Zod 验证
    .query(async ({ input, ctx }) => {
        // 可选: 权限检查
        // 调用服务层
        return findApplicationById(input.applicationId);
    }),
```

### 5.2 Mutation（变更）

```typescript
// 创建实体
create: protectedProcedure
    .input(apiCreateApplication)
    .mutation(async ({ input, ctx }) => {
        // 权限检查
        // 调用服务层
        return createApplication(input);
    }),
```

### 5.3 权限检查模式

在路由层进行的权限检查通常包括：
1. `protectedProcedure` 中间件确保已登录
2. `adminProcedure` 确保是 owner/admin
3. 路由内通过查询 member 表进行资源级权限检查

## 6. API 端点入口

**源文件**: `apps/dokploy/pages/api/trpc/[trpc].ts`

tRPC 通过 Next.js API Route 处理所有请求：
- URL: `/api/trpc/{procedurePath}`
- 方法: GET（query）/ POST（mutation）
- 数据格式: SuperJSON 编码

## 7. 源文件清单

| 文件 | 说明 |
|------|------|
| `apps/dokploy/server/api/trpc.ts` | tRPC 初始化和中间件 |
| `apps/dokploy/server/api/root.ts` | 路由注册 |
| `apps/dokploy/server/api/routers/*.ts` | 41 个主路由文件 |
| `apps/dokploy/server/api/routers/proprietary/*.ts` | 2 个企业路由（共 43 个路由注册在 appRouter 中） |
| `apps/dokploy/pages/api/trpc/[trpc].ts` | tRPC API 入口 |

## 8. Go 重写注意事项

- tRPC 是 TypeScript 专用协议，Go 版本需要改用标准 REST API
- 可以参考 `openapi.json` 生成等价的 REST 端点
- 前端需要从 tRPC 客户端切换到 REST 客户端（或保持 tRPC 协议但用 Go 实现兼容的 HTTP 处理器）
- 方案选择：
  1. **推荐**: 实现与 tRPC HTTP 协议兼容的处理器（GET query + POST mutation + SuperJSON）
  2. **备选**: 改用 REST API 并修改前端
- 中间件模式在 Go 中用 HTTP middleware chain 实现
- 输入验证用 struct tag 或 validator 库
