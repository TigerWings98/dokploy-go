# 应用生命周期

## 1. 模块概述

本文档描述 Dokploy 中应用和 Compose 服务从创建到销毁的完整生命周期。涵盖部署触发、构建执行、服务创建、预览部署、以及部署队列机制。

生命周期概览：
```
创建应用 → 配置域名/环境变量/挂载 → 触发部署 → [队列] → 克隆代码 → 构建镜像 → 创建/更新 Docker Service → 通知 → 运行中
                                                                                                          ↕
                                                              停止 ← 缩容(scale=0)    启动 → 扩容(scale=1)
                                                                                                          ↓
                                                              销毁 ← 删除服务 + 删除配置 + 删除代码 + 删除监控数据
```

## 2. 应用创建

### 2.1 createApplication

```typescript
// services/application.ts
export const createApplication = async (input) => {
    // 1. 生成唯一 appName
    const appName = buildAppName("app", input.appName); // e.g. "app-myapp-a1b2c3d4"

    // 2. 验证名称唯一性
    const valid = await validUniqueServerAppName(appName);

    // 3. 事务创建
    return await db.transaction(async (tx) => {
        const newApplication = await tx.insert(applications).values({ ...input, appName });

        // 4. 开发环境创建 Traefik 配置
        if (process.env.NODE_ENV === "development") {
            createTraefikConfig(newApplication.appName);
        }
        return newApplication;
    });
};
```

`buildAppName` 格式：`{prefix}-{userAppName}-{nanoid(8)}`

## 3. 部署队列

### 3.1 BullMQ 部署 Worker

```typescript
// apps/dokploy/server/queues/deployments-queue.ts
const deploymentWorker = new Worker("deployments", async (job: Job<DeploymentJob>) => {
    switch (job.data.applicationType) {
        case "application":
            // deploy 或 redeploy
            await updateApplicationStatus(id, "running");
            if (job.data.type === "deploy") await deployApplication({...});
            if (job.data.type === "redeploy") await rebuildApplication({...});
            break;
        case "compose":
            await updateCompose(id, { composeStatus: "running" });
            if (job.data.type === "deploy") await deployCompose({...});
            if (job.data.type === "redeploy") await rebuildCompose({...});
            break;
        case "application-preview":
            await updatePreviewDeployment(id, { previewStatus: "running" });
            if (job.data.type === "deploy") await deployPreviewApplication({...});
            if (job.data.type === "redeploy") await rebuildPreviewApplication({...});
            break;
    }
}, { autorun: false, connection: redisConfig });
```

### 3.2 DeploymentJob 类型

```typescript
// queues/queue-types.ts
type DeploymentJob = {
    applicationId: string;
    titleLog: string;
    descriptionLog: string;
    type: "deploy" | "redeploy";
    applicationType: "application" | "compose" | "application-preview";
    composeId?: string;
    previewDeploymentId?: string;
};
```

### 3.3 Redis 连接

使用内置 Redis（`redis://127.0.0.1:6379`，IS_CLOUD 模式下禁用）。

## 4. 应用部署流程

### 4.1 deployApplication - 完整部署

```typescript
export const deployApplication = async ({ applicationId, titleLog, descriptionLog }) => {
    // 1. 查找应用（深度加载所有关联）
    const application = await findApplicationById(applicationId);
    const serverId = application.buildServerId || application.serverId;

    // 2. 创建部署记录
    const deployment = await createDeployment({ applicationId, title, description });
    // deployment.logPath = "/etc/dokploy/logs/{appName}/{timestamp}.log"

    try {
        // 3. 构建 shell 命令
        let command = "set -e;";

        // 4. 根据 sourceType 克隆代码
        switch (application.sourceType) {
            case "github":    command += await cloneGithubRepository(app); break;
            case "gitlab":    command += await cloneGitlabRepository(app); break;
            case "gitea":     command += await cloneGiteaRepository(app); break;
            case "bitbucket": command += await cloneBitbucketRepository(app); break;
            case "git":       command += await cloneGitRepository(app); break;
            case "docker":    command += await buildRemoteDocker(app); break;
        }

        // 5. 应用补丁（patches）
        if (sourceType !== "docker") {
            command += await generateApplyPatchesCommand({ id, type: "application", serverId });
        }

        // 6. 构建镜像 + Registry 推送
        command += await getBuildCommand(application);

        // 7. 执行命令（日志重定向到部署日志文件）
        const commandWithLog = `(${command}) >> ${deployment.logPath} 2>&1`;
        if (serverId) {
            await execAsyncRemote(serverId, commandWithLog);
        } else {
            await execAsync(commandWithLog);
        }

        // 8. 创建/更新 Docker Swarm Service
        await mechanizeDockerContainer(application);

        // 9. 更新状态
        await updateDeploymentStatus(deployment.deploymentId, "done");
        await updateApplicationStatus(applicationId, "done");

        // 10. 发送成功通知
        await sendBuildSuccessNotifications({...});

    } catch (error) {
        // 错误处理：写入日志、更新状态、发送错误通知
        await updateDeploymentStatus(deployment.deploymentId, "error");
        await updateApplicationStatus(applicationId, "error");
        await sendBuildErrorNotifications({...});
    } finally {
        // 提取 git commit 信息更新部署记录
        if (sourceType !== "docker") {
            const commitInfo = await getGitCommitInfo({ appName, type: "application", serverId });
            if (commitInfo) {
                await updateDeployment(deployment.deploymentId, {
                    title: commitInfo.message,
                    description: `Commit: ${commitInfo.hash}`,
                });
            }
        }
    }
};
```

### 4.2 rebuildApplication - 重新构建

与 `deployApplication` 的区别：**跳过代码克隆**，直接在已有代码上重新执行构建命令。

### 4.3 部署日志

- 日志路径：`/etc/dokploy/logs/{appName}/{timestamp}.log`（LOGS_PATH = `/etc/dokploy/logs`）
- 通过 shell 重定向捕获：`(command) >> logPath 2>&1`
- 错误消息通过 base64 编码后 `echo` 追加到日志
- WebSocket 实时推送日志到前端（见 10-websocket-layer.md）

## 5. 预览部署

### 5.1 deployPreviewApplication

预览部署用于 Pull Request 的临时环境：

1. 查找应用和预览部署配置
2. 创建部署记录（关联到 previewDeploymentId）
3. 在 GitHub PR 中创建/更新评论（显示部署状态）
4. 覆盖应用属性：
   - `appName` → 预览部署的 appName
   - `env` → `previewEnv + DOKPLOY_DEPLOY_URL`
   - `buildArgs` → `previewBuildArgs + DOKPLOY_DEPLOY_URL`
   - `buildSecrets` → `previewBuildSecrets + DOKPLOY_DEPLOY_URL`
   - 禁用 rollback、registry
5. 克隆指定分支代码
6. 构建并创建 Docker Service
7. 更新 PR 评论为成功/失败状态

### 5.2 rebuildPreviewApplication

跳过代码克隆，直接重新构建预览部署。

### 5.3 PR 评论状态

| 状态 | 评论内容 |
|------|---------|
| running | 🔨 Building... |
| success | ✅ Deployed at `{previewDomain}` |
| error | ❌ Deployment failed |

## 6. Compose 部署

### 6.1 deployCompose 流程

```
1. 克隆代码（按 sourceType）
2. 应用补丁
3. 注入域名标签到 compose 文件
4. 创建 .env 文件
5. 执行 docker compose up -d 或 docker stack deploy
6. 更新状态和通知
```

### 6.2 两种模式

| 模式 | 命令 | 适用场景 |
|------|------|---------|
| `docker-compose` | `docker compose -p {appName} -f {path} up -d --build --remove-orphans` | 单机部署 |
| `stack` | `docker stack deploy -c {path} {appName} --prune --with-registry-auth` | Swarm 集群部署 |

### 6.3 隔离部署（isolatedDeployment）

- 创建独立 Docker 网络：`docker network create --attachable {appName}`
- 连接 Traefik 到该网络：`docker network connect {appName} $(docker ps --filter "name=dokploy-traefik" -q)`
- Compose 服务名添加 appName 前缀防止冲突

## 7. Docker Service 管理

### 7.1 mechanizeDockerContainer

创建或更新 Docker Swarm Service：

```typescript
// 尝试更新
try {
    const service = docker.getService(appName);
    const inspect = await service.inspect();
    await service.update({
        version: inspect.Version.Index,  // 乐观锁
        ...settings,
        ForceUpdate: inspect.Spec.TaskTemplate.ForceUpdate + 1,  // 强制更新
    });
} catch {
    // 首次创建
    await docker.createService(settings);
}
```

### 7.2 启动/停止/删除

| 操作 | 本地命令 | 远程命令 |
|------|---------|---------|
| 启动 | `docker service scale {appName}=1` | `execAsyncRemote(serverId, ...)` |
| 停止 | `docker service scale {appName}=0` | `execAsyncRemote(serverId, ...)` |
| 删除 | `docker service rm {appName}` | `execAsyncRemote(serverId, ...)` |

## 8. 应用删除

删除应用需要清理多个资源：

1. **停止 Docker Service** - `docker service rm {appName}`
2. **删除 Traefik 配置** - 删除 `{DYNAMIC_TRAEFIK_PATH}/{appName}.yml`
3. **删除中间件** - 清理 `middlewares.yml` 中的相关条目
4. **删除代码目录** - `rm -rf {APPLICATIONS_PATH}/{appName}`
5. **删除监控数据** - `rm -rf {MONITORING_PATH}/{appName}`
6. **删除数据库记录** - 级联删除（deployments, domains, mounts, ports, security, redirects 等）

## 9. 状态机

### 9.1 Application 状态

```
idle → running → done
              → error
```

| 状态 | 含义 |
|------|------|
| `idle` | 初始/空闲 |
| `running` | 部署中 |
| `done` | 部署成功 |
| `error` | 部署失败 |

### 9.2 Deployment 状态

```
running → done
       → error
```

### 9.3 Preview Deployment 状态

```
running → done
       → error
```

## 10. 通知集成

部署完成后发送通知：

| 事件 | 函数 | 包含信息 |
|------|------|---------|
| 构建成功 | `sendBuildSuccessNotifications` | 项目名、应用名、类型、构建链接、域名列表、环境名 |
| 构建失败 | `sendBuildErrorNotifications` | 项目名、应用名、类型、错误消息、构建链接 |

通知渠道支持：Slack、Discord、Telegram、Email 等（见 15-notification-system.md）。

## 11. 服务器选择逻辑

```
构建服务器 = application.buildServerId || application.serverId
部署服务器 = application.serverId
```

- **buildServerId**: 专用构建服务器（代码克隆和镜像构建在此执行）
- **serverId**: 运行服务器（Docker Service 运行在此）
- 如果使用构建服务器，构建完成后通过 Registry 分发镜像到部署服务器

## 12. 依赖关系

```
应用生命周期依赖：
├── services/deployment.ts (部署记录管理)
├── services/preview-deployment.ts (预览部署管理)
├── services/domain.ts (域名管理)
├── services/github.ts (PR 评论)
├── services/patch.ts (补丁系统)
├── utils/builders/ (构建系统)
├── utils/providers/ (代码克隆)
├── utils/docker/ (Docker 操作)
├── utils/traefik/ (路由配置)
├── utils/notifications/ (通知发送)
├── utils/process/ (命令执行)
├── queues/ (BullMQ 部署队列)
└── monitoring/ (统计数据)
```

## 13. 源文件清单

```
packages/server/src/services/
├── application.ts            ← 应用 CRUD + 部署逻辑（核心）
├── compose.ts                ← Compose 服务 CRUD + 部署逻辑
├── deployment.ts             ← 部署记录 CRUD
├── preview-deployment.ts     ← 预览部署 CRUD

apps/dokploy/server/queues/
├── deployments-queue.ts      ← BullMQ 部署 Worker
├── queue-types.ts            ← DeploymentJob 类型定义
├── queueSetup.ts             ← 队列初始化
└── redis-connection.ts       ← Redis 连接配置
```

## 14. Go 重写注意事项

- **部署队列**: 使用 Go 的任务队列替代 BullMQ（如 `asynq`、`machinery` 或自建基于 channel 的队列）
- **Redis 依赖**: 如果不使用 BullMQ，可以去掉 Redis 依赖，改用内存队列或 PostgreSQL 队列
- **Shell 命令**: 所有 clone + build + deploy 命令是 shell 脚本字符串，语言无关可直接复用
- **日志重定向**: `>> logPath 2>&1` 模式是 shell 特性，直接复用
- **Docker Service API**: 使用 Go Docker SDK 的 `ServiceCreate`/`ServiceUpdate`/`ServiceRemove`
- **PR 评论**: GitHub API 调用需在 Go 中使用 `google/go-github` 库
- **通知**: 各渠道的 HTTP API 调用是语言无关的
- **状态机**: 可以用 Go 的 enum/const + 状态转换函数实现
- **事务**: 使用 `gorm.DB.Transaction` 或 `sqlx.Tx`
