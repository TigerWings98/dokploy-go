# 11 - 部署队列与任务调度

## 1. 模块概述

部署队列模块负责将应用和 Compose 的部署/重部署操作序列化为异步任务，通过 Redis 支撑的 BullMQ 队列进行调度和执行。该模块是 Dokploy 部署流水线的核心入口——API 层将部署请求加入队列，Worker 按序消费并调用实际的构建/部署函数。

在 `IS_CLOUD` 模式（托管 SaaS 版本）下，队列和 Worker 均被替换为 noop 空实现，避免 Redis 连接错误。

**核心职责：**
- 接收部署任务并入队（按 application / compose / preview 分类）
- Worker 逐个消费任务、更新状态、执行部署
- 提供队列清理、任务取消、Docker 构建进程终止能力
- 优雅关闭（SIGTERM 信号处理）

**在系统架构中的位置：**
```
API 层（tRPC Router） -> 队列（BullMQ） -> Worker -> 服务层（deploy/rebuild） -> Docker
                           |
                        Redis（任务持久化）
```

## 2. 设计详解

### 2.1 Redis 连接配置

```typescript
// redis-connection.ts
export const redisConfig: ConnectionOptions = {
    host:
        process.env.NODE_ENV === "production"
            ? process.env.REDIS_HOST || "dokploy-redis"
            : "127.0.0.1",
};
```

生产环境优先读取 `REDIS_HOST` 环境变量，默认连接到 Docker 网络内的 `dokploy-redis` 容器；开发环境连接 `127.0.0.1`。仅配置 `host`，端口使用 Redis 默认 6379。

### 2.2 DeploymentJob 类型定义

```typescript
// queue-types.ts
type DeployJob =
    | {
        applicationId: string;
        titleLog: string;
        descriptionLog: string;
        server?: boolean;
        type: "deploy" | "redeploy";
        applicationType: "application";
        serverId?: string;
    }
    | {
        composeId: string;
        titleLog: string;
        descriptionLog: string;
        server?: boolean;
        type: "deploy" | "redeploy";
        applicationType: "compose";
        serverId?: string;
    }
    | {
        applicationId: string;
        titleLog: string;
        descriptionLog: string;
        server?: boolean;
        type: "deploy" | "redeploy";
        applicationType: "application-preview";
        previewDeploymentId: string;
        serverId?: string;
    };

export type DeploymentJob = DeployJob;
```

这是一个辨别联合类型（Discriminated Union），通过 `applicationType` 字段区分三种任务：

| applicationType | 标识字段 | 说明 |
|-----------------|---------|------|
| `"application"` | `applicationId` | 普通应用部署 |
| `"compose"` | `composeId` | Docker Compose 部署 |
| `"application-preview"` | `applicationId` + `previewDeploymentId` | PR 预览部署 |

所有变体共享 `titleLog`、`descriptionLog`、`type`（deploy/redeploy）、`server`、`serverId` 字段。

### 2.3 部署 Worker

```typescript
// deployments-queue.ts
const createDeploymentWorker = () =>
    new Worker(
        "deployments",
        async (job: Job<DeploymentJob>) => {
            try {
                if (job.data.applicationType === "application") {
                    await updateApplicationStatus(job.data.applicationId, "running");
                    if (job.data.type === "redeploy") {
                        await rebuildApplication({
                            applicationId: job.data.applicationId,
                            titleLog: job.data.titleLog,
                            descriptionLog: job.data.descriptionLog,
                        });
                    } else if (job.data.type === "deploy") {
                        await deployApplication({
                            applicationId: job.data.applicationId,
                            titleLog: job.data.titleLog,
                            descriptionLog: job.data.descriptionLog,
                        });
                    }
                } else if (job.data.applicationType === "compose") {
                    await updateCompose(job.data.composeId, { composeStatus: "running" });
                    if (job.data.type === "deploy") {
                        await deployCompose({ ... });
                    } else if (job.data.type === "redeploy") {
                        await rebuildCompose({ ... });
                    }
                } else if (job.data.applicationType === "application-preview") {
                    await updatePreviewDeployment(job.data.previewDeploymentId, {
                        previewStatus: "running",
                    });
                    // deploy 或 redeploy 预览应用（额外传递 previewDeploymentId）
                }
            } catch (error) {
                console.log("Error", error);
            }
        },
        { autorun: false, connection: redisConfig },
    );
```

**处理流程：**
1. 根据 `applicationType` 分发到对应的状态更新函数
2. 将状态设为 `"running"`
3. 根据 `type` 调用 `deploy*` 或 `rebuild*` 函数
4. 错误仅 console.log，不会导致 Worker 崩溃

Worker 以 `autorun: false` 创建，需要外部显式调用 `worker.run()` 启动消费。

**任务分派表：**

| applicationType | type | 处理函数 | 前置操作 |
|-----------------|------|---------|---------|
| `application` | `deploy` | `deployApplication()` | `updateApplicationStatus("running")` |
| `application` | `redeploy` | `rebuildApplication()` | `updateApplicationStatus("running")` |
| `compose` | `deploy` | `deployCompose()` | `updateCompose({ composeStatus: "running" })` |
| `compose` | `redeploy` | `rebuildCompose()` | `updateCompose({ composeStatus: "running" })` |
| `application-preview` | `deploy` | `deployPreviewApplication()` | `updatePreviewDeployment({ previewStatus: "running" })` |
| `application-preview` | `redeploy` | `rebuildPreviewApplication()` | `updatePreviewDeployment({ previewStatus: "running" })` |

### 2.4 IS_CLOUD Noop 实现

#### Noop Worker

```typescript
const noopWorker = {
    run: () => Promise.resolve(),
    close: () => Promise.resolve(),
    cancelJob: () => Promise.resolve(),
    cancelAllJobs: () => Promise.resolve(),
};

export const deploymentWorker = !IS_CLOUD
    ? createDeploymentWorker()
    : (noopWorker as unknown as Worker<DeploymentJob>);
```

#### Noop Queue

```typescript
const createNoopQueue = () => ({
    getJobs: () => Promise.resolve([] as Job[]),
    add: () => Promise.resolve({ id: "noop", remove: () => Promise.resolve() } as Job),
    close: () => Promise.resolve(),
    on: () => {},
});

const myQueue = !IS_CLOUD
    ? new Queue("deployments", { connection: redisConfig })
    : (createNoopQueue() as unknown as Queue);
```

云端模式下所有方法均返回空 Promise，类型强转以保持接口兼容。

### 2.5 队列设置与管理

**导出的管理函数：**

| 函数 | 签名 | 说明 |
|------|------|------|
| `getJobsByApplicationId` | `(applicationId: string) => Promise<Job[]>` | 获取队列中某应用的所有任务 |
| `getJobsByComposeId` | `(composeId: string) => Promise<Job[]>` | 获取队列中某 Compose 的所有任务 |
| `cleanQueuesByApplication` | `(applicationId: string) => Promise<void>` | 移除 waiting/delayed 状态中某应用的任务 |
| `cleanQueuesByCompose` | `(composeId: string) => Promise<void>` | 移除 waiting/delayed 状态中某 Compose 的任务 |
| `cleanAllDeploymentQueue` | `() => Promise<boolean>` | 取消 Worker 中所有正在执行的任务 |
| `killDockerBuild` | `(type, serverId) => Promise<void>` | 通过 pkill 终止 docker build/compose 进程 |

**信号处理与错误恢复：**

```typescript
if (!IS_CLOUD) {
    process.on("SIGTERM", () => {
        myQueue.close();
        process.exit(0);
    });

    myQueue.on("error", (error) => {
        if ((error as any).code === "ECONNREFUSED") {
            console.error("Make sure you have installed Redis and it is running.", error);
        }
    });
}
```

### 2.6 终止 Docker 构建

```typescript
export const killDockerBuild = async (
    type: "application" | "compose",
    serverId: string | null,
) => {
    const command = type === "application"
        ? `pkill -2 -f "docker build"`
        : `pkill -2 -f "docker compose"`;

    if (serverId) {
        await execAsyncRemote(serverId, command);
    } else {
        await execAsync(command);
    }
};
```

> **[可复用]** `pkill -2 -f "docker build"` 和 `pkill -2 -f "docker compose"` 是与语言无关的 Shell 命令，Go 重写时可直接使用。

### 2.7 任务生命周期

```
                    +----------+
                    | 创建任务  |  myQueue.add(...)
                    +----+-----+
                         |
                    +----v-----+
                    | 等待中    |  waiting 状态
                    | (waiting) |
                    +----+-----+
                         |  Worker 取出任务
                    +----v-----+
                    | 执行中    |  更新 status = "running"
                    | (active)  |  调用 deploy/rebuild 函数
                    +----+-----+
                         |
              +----------+----------+
              |          |          |
         +----v---+ +---v----+ +--v------+
         | 完成   | | 失败   | | 取消    |
         |(done)  | |(error) | |(cancel) |
         +--------+ +--------+ +---------+
```

## 3. 源文件清单

| 文件路径 | 说明 |
|----------|------|
| `apps/dokploy/server/queues/redis-connection.ts` | Redis 连接配置 |
| `apps/dokploy/server/queues/queue-types.ts` | DeploymentJob 类型定义 |
| `apps/dokploy/server/queues/deployments-queue.ts` | Worker 创建、noop Worker、任务处理逻辑 |
| `apps/dokploy/server/queues/queueSetup.ts` | Queue 实例、管理函数、信号处理、killDockerBuild |

## 4. 对外接口

### 导出的 Worker

```typescript
export const deploymentWorker: Worker<DeploymentJob>
```

### 导出的 Queue 及函数

```typescript
export { myQueue }  // BullMQ Queue 实例

export const getJobsByApplicationId: (applicationId: string) => Promise<Job[]>
export const getJobsByComposeId: (composeId: string) => Promise<Job[]>
export const cleanQueuesByApplication: (applicationId: string) => Promise<void>
export const cleanQueuesByCompose: (composeId: string) => Promise<void>
export const cleanAllDeploymentQueue: () => Promise<boolean>
export const killDockerBuild: (
    type: "application" | "compose",
    serverId: string | null,
) => Promise<void>
```

### 类型导出

```typescript
export type DeploymentJob = DeployJob  // 辨别联合类型
```

## 5. 依赖关系

### 上游依赖

| 依赖 | 用途 |
|------|------|
| `bullmq` (Queue, Worker, Job) | 任务队列核心库 |
| Redis 服务 | 队列持久化存储后端 |
| `@dokploy/server` | deployApplication, rebuildApplication, deployCompose, rebuildCompose, deployPreviewApplication, rebuildPreviewApplication, updateApplicationStatus, updateCompose, updatePreviewDeployment, IS_CLOUD |
| `@dokploy/server/utils/process/execAsync` | execAsync, execAsyncRemote（用于 killDockerBuild） |

### 下游消费者

- **tRPC API 路由** -- 调用 `myQueue.add()` 入队部署任务
- **Webhook 处理器** -- 自动部署触发时入队
- **应用/Compose 管理模块** -- 调用 `cleanQueuesByApplication` / `cleanQueuesByCompose` 清理队列
- **取消部署 API** -- 调用 `cleanAllDeploymentQueue()` 和 `killDockerBuild()`
- **服务器启动** -- 调用 `deploymentWorker.run()` 启动消费

## 6. Go 重写注意事项

### 队列选型

BullMQ 是 Node.js 专用库，Go 重写需要选择替代方案：
- **asynq** (github.com/hibiken/asynq) -- 基于 Redis 的 Go 任务队列，API 风格接近 BullMQ
- **machinery** -- 更重量级的分布式任务队列
- 或自研基于 Redis List/Stream 的简单队列（Dokploy 场景相对简单）

### 可复用的 Shell 命令

以下命令可在 Go 中通过 `os/exec` 直接执行：

```bash
# 终止 docker build 进程
pkill -2 -f "docker build"

# 终止 docker compose 进程
pkill -2 -f "docker compose"
```

### 任务类型映射

DeploymentJob 的辨别联合类型在 Go 中可映射为：

```go
type DeploymentJob struct {
    ApplicationID       string `json:"applicationId,omitempty"`
    ComposeID           string `json:"composeId,omitempty"`
    PreviewDeploymentID string `json:"previewDeploymentId,omitempty"`
    TitleLog            string `json:"titleLog"`
    DescriptionLog      string `json:"descriptionLog"`
    Server              bool   `json:"server,omitempty"`
    Type                string `json:"type"`            // "deploy" | "redeploy"
    ApplicationType     string `json:"applicationType"` // "application" | "compose" | "application-preview"
    ServerID            string `json:"serverId,omitempty"`
}
```

### IS_CLOUD 模式

Go 版本可通过 `interface` + 不同实现来处理 noop 逻辑，比 TypeScript 的类型强转更自然：

```go
type DeployQueue interface {
    Add(ctx context.Context, job DeploymentJob) error
    GetJobsByApplicationID(ctx context.Context, appID string) ([]Job, error)
    CleanByApplication(ctx context.Context, appID string) error
    CleanByCompose(ctx context.Context, composeID string) error
    CancelAll(ctx context.Context) error
    Close() error
}

// 实际队列实现
type redisDeployQueue struct { ... }

// Cloud 模式空实现
type noopDeployQueue struct {}
```

### Redis 连接

Redis 连接配置（host 选择逻辑）是环境相关的，Go 中直接使用 `go-redis` 库即可。连接字符串逻辑保持一致：生产环境读 `REDIS_HOST` 环境变量，默认 `dokploy-redis`。

### 信号处理

Go 的 `os/signal` 包配合 `context.Context` 可以实现更优雅的关闭机制：

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
defer stop()
```

### Worker 并发控制

Go 的 goroutine 天然支持并发，但部署操作应串行执行以避免资源竞争。建议设置 Worker 并发数为 1（或按 serverId 分组串行执行）。
