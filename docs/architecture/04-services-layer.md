# 服务层（核心业务逻辑）

## 1. 模块概述

服务层位于 `packages/server/src/services/`，包含 43 个服务文件。每个文件导出一组业务逻辑函数，被 tRPC 路由层调用。服务层是 Dokploy 的核心，封装了所有数据库操作和业务规则。

在系统架构中的位置：
```
tRPC 路由层 → 服务层 → 数据库 / Docker / SSH / Traefik
```

## 2. 服务清单与职责

### 2.1 核心实体服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `project.ts` | createProject, findProjectById, updateProject, removeProject, duplicateProject | 项目 CRUD |
| `environment.ts` | createEnvironment, findEnvironmentById, updateEnvironment, removeEnvironment, duplicateEnvironment | 环境 CRUD |
| `application.ts` | createApplication, findApplicationById, updateApplication, removeApplication, deployApplication, redeployApplication, startService, stopService | 应用 CRUD + 部署触发 |
| `compose.ts` | createCompose, findComposeById, updateCompose, removeCompose, deployCompose, redeployCompose, startCompose, stopCompose | Compose 服务管理 |
| `deployment.ts` | createDeployment, findDeploymentById, updateDeployment, findAllByApplication, findAllByCompose, findAllCentralized | 部署记录管理 |
| `preview-deployment.ts` | createPreviewDeployment, findPreviewDeploymentById, removePreviewDeployment | 预览部署管理 |

### 2.2 数据库服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `mysql.ts` | createMysql, findMySqlById, updateMySql, removeMySql, deployMySql | MySQL 实例管理 |
| `postgres.ts` | createPostgres, findPostgresById, updatePostgres, removePostgres, deployPostgres | PostgreSQL 实例管理 |
| `redis.ts` | createRedis, findRedisById, updateRedis, removeRedis, deployRedis | Redis 实例管理 |
| `mariadb.ts` | createMariadb, findMariadbById, updateMariadb, removeMariadb, deployMariadb | MariaDB 实例管理 |
| `mongo.ts` | createMongo, findMongoById, updateMongo, removeMongo, deployMongo | MongoDB 实例管理 |

### 2.3 基础设施服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `server.ts` | createServer, findServerById, updateServer, removeServer, validateServer | 远程服务器管理 |
| `docker.ts` | getContainers, getContainersByAppName, getConfig, getStats | Docker 容器查询 |
| `cluster.ts` | addWorker, removeWorker | Docker Swarm 集群 |
| `domain.ts` | createDomain, findDomainById, updateDomain, removeDomain, manageDomain | 域名管理 |
| `certificate.ts` | createCertificate, findCertificateById, removeCertificate | SSL 证书管理 |

### 2.4 配置服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `mount.ts` | createMount, findMountById, updateMount, removeMount | 挂载点管理 |
| `port.ts` | createPort, findPortById, updatePort, removePort | 端口映射 |
| `security.ts` | createSecurity, findSecurityById, updateSecurity, removeSecurity | 安全策略 |
| `redirect.ts` | createRedirect, findRedirectById, updateRedirect, removeRedirect | HTTP 重定向 |
| `registry.ts` | createRegistry, findRegistryById, updateRegistry, removeRegistry | Docker 仓库 |

### 2.5 Git 集成服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `git-provider.ts` | createGitProvider, findGitProviderById, updateGitProvider, removeGitProvider | Git 提供商基础 |
| `github.ts` | findGithubById, getGithubRepositories, getGithubBranches, cloneGithubRepository | GitHub 集成 |
| `gitlab.ts` | findGitlabById, getGitlabRepositories, getGitlabBranches, cloneGitlabRepository | GitLab 集成 |
| `gitea.ts` | findGiteaById, getGiteaRepositories, getGiteaBranches, cloneGiteaRepository | Gitea 集成 |
| `bitbucket.ts` | findBitbucketById, getBitbucketRepositories, getBitbucketBranches, cloneBitbucketRepository | Bitbucket 集成 |
| `ssh-key.ts` | createSSHKey, findSSHKeyById, updateSSHKey, removeSSHKey, generateSSHKey | SSH 密钥管理 |

### 2.6 运维服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `backup.ts` | createBackup, findBackupById, updateBackup, removeBackup, runManualBackup, getBackupFiles, restoreBackup | 数据库备份 |
| `destination.ts` | createDestination, findDestinationById, updateDestination, removeDestination, testDestinationConnection | 备份目的地 |
| `volume-backups.ts` | createVolumeBackup, findVolumeBackupById, updateVolumeBackup, removeVolumeBackup | 卷备份 |
| `rollbacks.ts` | createRollback, findRollbackById, removeRollback | 回滚管理 |
| `schedule.ts` | createSchedule, findScheduleById, updateSchedule, removeSchedule | 定时任务 |
| `notification.ts` | createNotification, findNotificationById, updateNotification, removeNotification, testNotification | 通知配置 |

### 2.7 管理与设置

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `admin.ts` | findAdmin, updateAdmin, getUserByToken, getTrustedOrigins, getTrustedProviders | 管理员操作 |
| `settings.ts` | getWebServerSettings, getReleaseTag | 系统设置 |
| `user.ts` | findUserById, updateUser, removeUser | 用户管理 |
| `web-server-settings.ts` | getWebServerSettings, updateWebServerSettings | Web 服务器设置 |

### 2.8 辅助服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `patch.ts` | createPatch, runPatch | 补丁操作 |
| `patch-repo.ts` | createPatchRepo, removePatchRepo | 补丁仓库 |
| `ai.ts` | chat, getModels | AI 功能 |
| `cdn.ts` | CDNProvider 接口, isIPInCIDR, checkCloudflareIP 等 | CDN 提供商 IP 检测（Cloudflare 等） |

### 2.9 企业服务

| 服务文件 | 主要导出函数 | 职责 |
|---------|------------|------|
| `proprietary/license-key.ts` | activateLicenseKey, validateLicenseKey, hasValidLicense | 许可证管理 |
| `proprietary/sso.ts` | createSSOProvider, updateSSOProvider, deleteSSOProvider, getSSOProviders | SSO 管理 |

## 3. 服务层公共模式

### 3.1 findXxxById 查找模式

```typescript
export const findApplicationById = async (applicationId: string) => {
    const result = await db.query.applications.findFirst({
        where: eq(applications.applicationId, applicationId),
        with: {
            // 关联加载
            environment: { with: { project: true } },
            domains: true,
            deployments: true,
            mounts: true,
            security: true,
            redirects: true,
            ports: true,
            registry: true,
            github: true,
            gitlab: true,
            gitea: true,
            bitbucket: true,
            server: true,
            buildServer: true,
            previewDeployments: true,
        },
    });
    if (!result) {
        throw new TRPCError({ code: "NOT_FOUND", message: "Application not found" });
    }
    return result;
};
```

### 3.2 createXxx CRUD 模式

```typescript
export const createApplication = async (input: typeof apiCreateApplication._type) => {
    const newApp = await db.insert(applications).values({
        ...input,
        // 默认值
    }).returning().then(res => res[0]);

    if (!newApp) {
        throw new TRPCError({ code: "BAD_REQUEST", message: "Failed to create" });
    }

    // 后续操作（如创建 Traefik 配置）
    await createTraefikConfig(newApp.appName);

    return newApp;
};
```

### 3.3 错误处理模式

```typescript
// 使用 TRPCError 抛出业务错误
throw new TRPCError({
    code: "NOT_FOUND",      // HTTP 404
    message: "Entity not found",
});

throw new TRPCError({
    code: "BAD_REQUEST",     // HTTP 400
    message: "Validation failed",
});

throw new TRPCError({
    code: "UNAUTHORIZED",    // HTTP 401
    message: "Not authorized",
});
```

### 3.4 事务使用模式

```typescript
await db.transaction(async (tx) => {
    const entity = await tx.insert(table).values({...}).returning();
    await tx.insert(relatedTable).values({...});
    return entity;
});
```

## 4. 关键服务详解

### 4.1 application.ts

最复杂的服务文件，核心功能：

- `findApplicationById`: 深度嵌套加载所有关联数据
- `createApplication`: 创建应用 + Traefik 配置
- `deployApplication`: 触发部署流程
  1. 创建 deployment 记录
  2. 根据 sourceType 准备代码（克隆/拉取）
  3. 根据 buildType 选择构建器
  4. 创建/更新 Docker Service
  5. 设置 Traefik 路由
  6. 发送通知
- `removeApplication`: 删除应用
  1. 停止 Docker 服务
  2. 删除 Traefik 配置
  3. 删除代码目录
  4. 删除监控数据
  5. 删除数据库记录

### 4.2 compose.ts

Docker Compose 服务管理：

- 支持两种模式: `docker-compose`（本地 compose up）和 `stack`（Swarm stack deploy）
- 支持 Git 源码或直接编辑 compose 文件（raw 模式）
- `randomizeCompose`: 给 Compose 文件中的服务名、卷名、网络名添加随机后缀
- 域名注入: 将 Traefik 路由标签注入到 Compose 文件中

### 4.3 server.ts

服务器管理核心：

- `createServer`: 注册远程服务器
- `validateServer`: 验证 SSH 连接和环境（Docker、Swarm、构建工具等）
- `setupServer`: 在远程服务器上安装 Dokploy 组件

## 5. 依赖关系

```
服务层依赖：
├── db (Drizzle ORM)
├── utils/builders/ (构建系统)
├── utils/docker/ (Docker 操作)
├── utils/traefik/ (Traefik 配置)
├── utils/providers/ (Git 提供商)
├── utils/process/ (命令执行)
├── utils/filesystem/ (文件操作)
├── utils/notifications/ (通知发送)
├── utils/backups/ (备份操作)
├── utils/servers/ (远程 Docker)
└── utils/cluster/ (集群操作)
```

## 6. 源文件清单

```
packages/server/src/services/
├── admin.ts
├── ai.ts
├── application.ts          ← 最核心
├── backup.ts
├── bitbucket.ts
├── cdn.ts
├── certificate.ts
├── cluster.ts
├── compose.ts              ← 第二核心
├── deployment.ts
├── destination.ts
├── docker.ts
├── domain.ts
├── environment.ts
├── git-provider.ts
├── gitea.ts
├── github.ts
├── gitlab.ts
├── mariadb.ts
├── mongo.ts
├── mount.ts
├── mysql.ts
├── notification.ts
├── patch.ts
├── patch-repo.ts
├── port.ts
├── postgres.ts
├── preview-deployment.ts
├── project.ts
├── proprietary/
│   ├── license-key.ts
│   └── sso.ts
├── redirect.ts
├── redis.ts
├── registry.ts
├── rollbacks.ts
├── schedule.ts
├── security.ts
├── server.ts
├── settings.ts
├── ssh-key.ts
├── user.ts
├── volume-backups.ts
└── web-server-settings.ts
```

## 7. Go 重写注意事项

- 每个服务文件对应一个 Go package 或文件
- CRUD 操作可使用 Repository 模式封装
- TRPCError 映射到 HTTP 状态码：NOT_FOUND→404, BAD_REQUEST→400, UNAUTHORIZED→401
- 关联加载（with）在 Go ORM 中需要显式配置
- 事务在 Go 中使用 `db.Transaction(func(tx *gorm.DB) error { ... })` 模式
- 建议保持与 TypeScript 版相同的函数命名，便于对照
