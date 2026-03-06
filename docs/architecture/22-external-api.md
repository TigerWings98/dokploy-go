# 外部 REST API（部署队列服务）

## 1. 模块概述

外部 API 服务（`apps/api`）是 Dokploy 的独立部署队列服务，基于 **Hono** HTTP 框架和 **Inngest** 事件驱动平台实现。它将部署请求从主服务中解耦，作为一个单独的 Node.js 进程运行，提供：

1. **部署队列管理** - 通过 Inngest 事件系统实现部署任务的排队、执行和取消
2. **并发控制** - 按 `serverId` 维度限制并发部署数为 1，防止同一服务器上的部署冲突
3. **部署取消** - 支持按 `applicationId` 或 `composeId` 取消正在排队或执行的部署
4. **任务状态查询** - 提供 BullMQ 兼容格式的任务列表 API，供前端 UI 展示
5. **部署执行** - 调用 `@dokploy/server` 包中的核心部署函数完成实际部署

在系统架构中的位置：
```
Dokploy 主服务 ─── HTTP POST /deploy ──→ API 服务 ──→ Inngest ──→ 部署执行
              ←── HTTP GET /jobs ──────←           ←── 状态查询 ←
              ─── HTTP POST /cancel ──→            ──→ 取消部署
```

注意：此服务与 `apps/dokploy/server/queues/` 中的 BullMQ 队列是两套并存的实现方案。BullMQ 方案用于自托管模式（非云版本），Inngest 方案用于 API 服务独立部署场景。两者处理相同的 `DeploymentJob` 类型。

## 2. 设计详解

### 2.1 Hono 应用与认证 - index.ts

应用使用 Hono 框架创建，认证通过 `X-API-Key` 请求头实现：

```typescript
const app = new Hono();

// 认证中间件：跳过 /health 和 /api/inngest 路径
app.use(async (c, next) => {
    if (c.req.path === "/health" || c.req.path === "/api/inngest") return next();
    const authHeader = c.req.header("X-API-Key");
    if (process.env.API_KEY !== authHeader) {
        return c.json({ message: "Invalid API Key" }, 403);
    }
    return next();
});
```

环境变量：
- `API_KEY` - API 认证密钥
- `PORT` - 服务端口（默认 3000）
- `INNGEST_BASE_URL` - Inngest API 地址（用于查询任务状态）
- `INNGEST_SIGNING_KEY` - Inngest 签名密钥

### 2.2 Inngest 事件驱动部署

#### 客户端初始化

```typescript
export const inngest = new Inngest({
    id: "dokploy-deployments",
    name: "Dokploy Deployment Service",
});
```

#### 部署函数定义

```typescript
export const deploymentFunction = inngest.createFunction(
    {
        id: "deploy-application",
        name: "Deploy Application",
        concurrency: [{
            key: "event.data.serverId",  // 按服务器分组
            limit: 1,                     // 同一服务器同时只允许一个部署
        }],
        retries: 0,                       // 部署失败不自动重试
        cancelOn: [{
            event: "deployment/cancelled",
            if: "async.data.applicationId == event.data.applicationId || async.data.composeId == event.data.composeId",
            timeout: "1h",               // 允许取消的时间窗口
        }],
    },
    { event: "deployment/requested" },    // 触发事件
    async ({ event, step }) => {
        return await step.run("execute-deployment", async () => {
            const result = await deploy(jobData);
            // 成功 → 发送 deployment/completed 事件
            // 失败 → 发送 deployment/failed 事件，并 throw
        });
    },
);
```

#### 事件流

```
POST /deploy
  └→ inngest.send({ name: "deployment/requested", data })
       └→ deploymentFunction 触发
            ├→ 成功 → inngest.send({ name: "deployment/completed", data: {..., status: "success"} })
            └→ 失败 → inngest.send({ name: "deployment/failed", data: {..., status: "failed"} })

POST /cancel-deployment
  └→ inngest.send({ name: "deployment/cancelled", data })
       └→ cancelOn 规则匹配 → 取消正在执行的 deploymentFunction
```

#### Inngest 端点注册

```typescript
app.on(["GET", "POST", "PUT"], "/api/inngest", serveInngest({
    client: inngest,
    functions: [deploymentFunction],
}));
```

Inngest SDK 通过此端点发现和调用注册的函数。

### 2.3 请求/响应 Schema - schema.ts

使用 Zod discriminated union 定义部署请求验证：

```typescript
export const deployJobSchema = z.discriminatedUnion("applicationType", [
    z.object({
        applicationId: z.string(),
        type: z.enum(["deploy", "redeploy"]),
        applicationType: z.literal("application"),
        serverId: z.string().min(1),
        titleLog: z.string().optional(),
        descriptionLog: z.string().optional(),
        server: z.boolean().optional(),
    }),
    z.object({
        composeId: z.string(),
        type: z.enum(["deploy", "redeploy"]),
        applicationType: z.literal("compose"),
        serverId: z.string().min(1),
        // ...同上可选字段
    }),
    z.object({
        applicationId: z.string(),
        previewDeploymentId: z.string(),
        type: z.enum(["deploy", "redeploy"]),
        applicationType: z.literal("application-preview"),
        serverId: z.string().min(1),
        // ...同上可选字段
    }),
]);
```

取消部署 Schema：

```typescript
export const cancelDeploymentSchema = z.discriminatedUnion("applicationType", [
    z.object({ applicationId: z.string(), applicationType: z.literal("application") }),
    z.object({ composeId: z.string(), applicationType: z.literal("compose") }),
]);
```

### 2.4 部署执行逻辑 - utils.ts

`deploy()` 函数根据 `applicationType` 和 `type` 分发到不同的部署函数：

```
deploy(job)
  ├── applicationType === "application"
  │   ├── updateApplicationStatus(id, "running")
  │   ├── type === "deploy"    → deployApplication(...)
  │   └── type === "redeploy"  → rebuildApplication(...)
  │
  ├── applicationType === "compose"
  │   ├── updateCompose(id, { composeStatus: "running" })
  │   ├── type === "deploy"    → deployCompose(...)
  │   └── type === "redeploy"  → rebuildCompose(...)
  │
  └── applicationType === "application-preview"
      ├── updatePreviewDeployment(id, { previewStatus: "running" })
      ├── type === "deploy"    → deployPreviewApplication(...)
      └── type === "redeploy"  → rebuildPreviewApplication(...)

异常处理：
  catch → 将状态更新为 "error" → re-throw
```

所有核心部署函数来自 `@dokploy/server` 包。

### 2.5 任务查询服务 - service.ts

`fetchDeploymentJobs(serverId)` 通过 Inngest REST API 查询部署任务，返回 BullMQ 兼容格式：

```
fetchDeploymentJobs(serverId)
  1. fetchInngestEvents() → GET /v1/events (分页，每页 100，最多 500 条)
  2. 过滤 name === "deployment/requested" && data.serverId === serverId
  3. 取前 50 个事件
  4. 并发调用 fetchInngestRunsForEvent(eventId) → GET /v1/events/{eventId}/runs
  5. buildDeploymentRowsFromRuns() → 合并事件和运行记录，按时间戳降序排序
```

状态映射（Inngest -> BullMQ 兼容格式）：

| Inngest 状态 | 映射结果 |
|-------------|---------|
| Running | active |
| Completed | completed |
| Failed | failed |
| Cancelled | cancelled |
| Queued | pending |
| 无 run 记录 | pending |

返回的 `DeploymentJobRow` 结构：

```typescript
type DeploymentJobRow = {
    id: string;         // run_id 或 event_id
    name: string;       // 事件名
    data: Record<string, unknown>; // 部署数据
    timestamp: number;  // 时间戳 ms
    processedOn?: number;
    finishedOn?: number;
    failedReason?: string;
    state: string;      // pending/active/completed/failed/cancelled
};
```

### 2.6 日志 - logger.ts

```typescript
import pino from "pino";
export const logger = pino({
    transport: { target: "pino-pretty", options: { colorize: true } },
});
```

## 3. 源文件清单

```
apps/api/src/
├── index.ts                             -- Hono 应用入口、路由注册、Inngest 函数定义、认证中间件
├── schema.ts                            -- Zod Schema（deployJobSchema, cancelDeploymentSchema）
├── utils.ts                             -- deploy() 部署执行逻辑（分发到 @dokploy/server 函数）
├── service.ts                           -- Inngest REST API 集成、任务查询、BullMQ 格式转换
└── logger.ts                            -- Pino 日志配置（pretty print）
```

## 4. 对外接口

### HTTP API

| 方法 | 路径 | 认证 | 请求体/参数 | 响应 |
|------|------|------|-------------|------|
| POST | `/deploy` | X-API-Key | `DeployJob` JSON | `{"message": "...", "serverId": "..."}` 200 / 500 |
| POST | `/cancel-deployment` | X-API-Key | `CancelDeploymentJob` JSON | `{"message": "...", "applicationType": "..."}` 200 / 500 |
| GET | `/jobs` | X-API-Key | `?serverId=xxx`（必需） | `DeploymentJobRow[]` 200 / 400 / 503 |
| GET | `/health` | 无 | 无 | `{"status": "ok"}` |
| GET/POST/PUT | `/api/inngest` | Inngest SDK | Inngest 内部协议 | Inngest 内部协议 |

### DeployJob 请求体类型

| applicationType | 必需字段 | 可选字段 |
|-----------------|----------|----------|
| `application` | `applicationId`, `serverId`, `type` | `titleLog`, `descriptionLog`, `server` |
| `compose` | `composeId`, `serverId`, `type` | `titleLog`, `descriptionLog`, `server` |
| `application-preview` | `applicationId`, `previewDeploymentId`, `serverId`, `type` | `titleLog`, `descriptionLog`, `server` |

- `type`: `"deploy"` 或 `"redeploy"`
- `serverId`: 必需，最少 1 字符

### CancelDeploymentJob 请求体类型

| applicationType | 必需字段 |
|-----------------|----------|
| `application` | `applicationId` |
| `compose` | `composeId` |

### 调用的 @dokploy/server 函数

```typescript
// 状态更新
updateApplicationStatus(applicationId: string, status: string)
updateCompose(composeId: string, data: { composeStatus: string })
updatePreviewDeployment(previewDeploymentId: string, data: { previewStatus: string })

// 部署执行
deployApplication({ applicationId, titleLog, descriptionLog })
rebuildApplication({ applicationId, titleLog, descriptionLog })
deployCompose({ composeId, titleLog, descriptionLog })
rebuildCompose({ composeId, titleLog, descriptionLog })
deployPreviewApplication({ applicationId, titleLog, descriptionLog, previewDeploymentId })
rebuildPreviewApplication({ applicationId, titleLog, descriptionLog, previewDeploymentId })
```

## 5. 依赖关系

### 本模块依赖

```
API 服务依赖：
├── hono                                 -- HTTP 框架
├── @hono/node-server                    -- Node.js HTTP 适配器
├── @hono/zod-validator                  -- 请求体 Zod 验证中间件
├── inngest                              -- 事件驱动任务队列 SDK
├── zod                                  -- Schema 定义和验证
├── pino / pino-pretty                   -- 结构化日志
├── dotenv                               -- 环境变量加载
└── @dokploy/server                      -- 部署核心逻辑包
    ├── deployApplication / rebuildApplication
    ├── deployCompose / rebuildCompose
    ├── deployPreviewApplication / rebuildPreviewApplication
    └── updateApplicationStatus / updateCompose / updatePreviewDeployment
```

### 被谁依赖

```
├── Dokploy 主服务                        -- 通过 HTTP 调用 /deploy, /cancel-deployment, /jobs
├── Inngest 平台/服务                     -- 通过 /api/inngest 端点注册函数和接收事件回调
└── Dokploy 前端 UI                       -- 间接通过主服务查询部署任务状态
```

### 与 BullMQ 队列的关系

`apps/dokploy/server/queues/` 中的 BullMQ 实现是同一功能的另一套方案：

| 特性 | API 服务 (Inngest) | 主服务队列 (BullMQ) |
|------|-------------------|---------------------|
| 队列后端 | Inngest 平台 | Redis |
| 部署方式 | 独立进程 | 主服务内嵌 |
| 并发控制 | Inngest concurrency 配置 | BullMQ Worker 单例 |
| 云模式 | 可用 | 禁用 (`IS_CLOUD` 检查) |
| 取消机制 | Inngest cancelOn 事件 | `worker.cancelAllJobs()` + `pkill docker` |

## 6. Go 重写注意事项

### 需要重新实现的部分

- **HTTP 框架**: Hono 替换为 Go HTTP 框架（Fiber/Echo/Chi/标准库），路由结构简单可直接映射
- **请求验证**: Zod Schema 替换为 Go struct tag + `validator` 库（如 `go-playground/validator`），或使用 JSON Schema 验证
- **日志**: Pino 替换为 Go 日志库（如 `slog`、`zerolog`、`zap`）

### Inngest 替代方案

Go 版本可能不使用 Inngest，可选方案：

1. **goroutine + channel** - 最简方案，按 serverId 维护一个 channel 作为队列，goroutine 串行消费
2. **`github.com/hibiken/asynq`** - 基于 Redis 的 Go 任务队列，支持并发控制、重试、定时任务
3. **`github.com/riverqueue/river`** - 基于 PostgreSQL 的 Go 任务队列
4. **内嵌实现** - 使用 `sync.Map` + 每个 serverId 一个 worker goroutine

关键需求：
- **按 serverId 限制并发为 1** - 同一服务器上的部署必须排队
- **支持任务取消** - 通过 `context.WithCancel` + goroutine 实现
- **任务状态查询** - 维护内存或数据库中的任务状态供 API 查询

### 服务合并可能性

Go 版本中，部署队列可以作为主服务的一个内部模块而非独立服务：
- 部署函数直接在同一进程中可用，无需 HTTP 调用
- 通过 goroutine 和 channel 实现队列功能
- 减少网络开销和部署复杂度
- BullMQ 队列和 Inngest API 服务可合并为统一的部署队列模块

### 可直接复用的部分 [可直接复用]

- **部署执行逻辑**: `utils.ts` 中的分发逻辑（按 applicationType 和 type 分发）是语言无关的业务逻辑
- **DeployJob 类型定义**: 三种 applicationType 的 discriminated union 结构
- **BullMQ 兼容响应格式**: `DeploymentJobRow` 结构（如果前端 UI 仍需相同格式）
- **状态映射**: Running/Completed/Failed/Cancelled/Queued -> active/completed/failed/cancelled/pending
- **API 路由设计**: `/deploy`, `/cancel-deployment`, `/jobs`, `/health` 端点设计
- **认证方式**: X-API-Key 头认证模式
