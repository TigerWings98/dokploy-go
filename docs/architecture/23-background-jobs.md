# 后台任务服务

## 1. 模块概述

后台任务服务（`apps/schedules`）是 Dokploy 的定时任务管理组件，基于 **Hono** HTTP 框架和 **BullMQ**（Redis 队列）实现。它负责管理和执行所有周期性后台任务：

1. **数据库备份** - PostgreSQL、MySQL、MongoDB、MariaDB 的定时备份
2. **Compose 备份** - Docker Compose 项目的定时备份
3. **卷备份** - Docker Volume 的定时备份
4. **服务器清理** - Docker 资源的定时清理（容器、镜像、构建缓存）
5. **自定义计划任务** - 用户定义的 cron 命令执行

在系统架构中的位置：
```
Dokploy 主服务 → HTTP API → Schedules 服务 → BullMQ Queue → Redis
                                              ← Workers 消费 →  执行备份/清理/命令
```

## 2. Hono 应用与认证

### 2.1 index.ts - 应用入口

```typescript
const app = new Hono();

// 启动时清理队列并初始化所有已有任务
cleanQueue();
initializeJobs();

// API Key 认证（跳过 /health）
app.use(async (c, next) => {
    if (c.req.path === "/health") return next();
    if (process.env.API_KEY !== c.req.header("X-API-Key")) {
        return c.json({ message: "Invalid API Key" }, 403);
    }
    return next();
});
```

启动时首先清空队列（`obliterate`），然后从数据库重新加载所有启用的定时任务。这确保了服务重启后任务状态的一致性。

#### 优雅关闭

```typescript
export const gracefulShutdown = async (signal: string) => {
    await firstWorker.close();
    await secondWorker.close();
    await thirdWorker.close();
    process.exit(0);
};
process.on("SIGINT", () => gracefulShutdown("SIGINT"));
process.on("SIGTERM", () => gracefulShutdown("SIGTERM"));
```

## 3. REST 端点

| 方法 | 路径 | 认证 | 功能 |
|------|------|------|------|
| POST | `/create-backup` | API Key | 创建新的定时任务 |
| POST | `/update-backup` | API Key | 更新已有定时任务（先删除旧任务再创建新任务） |
| POST | `/remove-job` | API Key | 移除定时任务 |
| GET | `/health` | 无 | 健康检查 |

## 4. 任务 Schema

### 4.1 schema.ts - Zod 验证

```typescript
export const jobQueueSchema = z.discriminatedUnion("type", [
    z.object({ type: z.literal("backup"),        backupId: z.string(),       cronSchedule: z.string() }),
    z.object({ type: z.literal("server"),         serverId: z.string(),       cronSchedule: z.string() }),
    z.object({ type: z.literal("schedule"),       scheduleId: z.string(),     cronSchedule: z.string(), timezone: z.string().optional() }),
    z.object({ type: z.literal("volume-backup"),  volumeBackupId: z.string(), cronSchedule: z.string() }),
]);
```

四种任务类型：

| 类型 | 标识字段 | 说明 |
|------|----------|------|
| `backup` | `backupId` | 数据库/Compose 备份 |
| `server` | `serverId` | 服务器 Docker 资源清理 |
| `schedule` | `scheduleId` | 自定义计划命令（支持时区） |
| `volume-backup` | `volumeBackupId` | Docker 卷备份 |

## 5. BullMQ 队列管理

### 5.1 queue.ts - 队列操作

队列名称：`backupQueue`，连接 Redis（通过 `REDIS_URL` 环境变量）。

```typescript
export const jobQueue = new Queue("backupQueue", {
    connection: { url: process.env.REDIS_URL },
    defaultJobOptions: {
        removeOnComplete: true,  // 完成后自动删除
        removeOnFail: true,      // 失败后自动删除
    },
});
```

| 函数 | 功能 |
|------|------|
| `cleanQueue()` | 强制清空队列（`obliterate`），启动时调用 |
| `scheduleJob(job)` | 添加 repeatable job，使用 `cron pattern` 调度 |
| `removeJob(data)` | 按任务 ID 和 cron pattern 移除 repeatable job |
| `getJobRepeatable(data)` | 查找已存在的 repeatable job |

任务命名规则：
- `backup` → 任务名为 `backupId`
- `server` → 任务名为 `{serverId}-cleanup`
- `schedule` → 任务名为 `scheduleId`，支持 `timezone` 参数
- `volume-backup` → 任务名为 `volumeBackupId`

## 6. Worker 消费者

### 6.1 workers.ts - 三个并行 Worker

系统创建**三个相同配置的 Worker** 来并行消费 `backupQueue`：

```typescript
export const firstWorker = new Worker("backupQueue",
    async (job: Job<QueueJob>) => { await runJobs(job.data); },
    { concurrency: 100, connection: { url: process.env.REDIS_URL } },
);
// secondWorker, thirdWorker 配置相同
```

每个 Worker 的并发度为 100，三个 Worker 共计可同时处理 300 个任务。

## 7. 任务执行逻辑

### 7.1 utils.ts - runJobs()

```typescript
export const runJobs = async (job: QueueJob) => {
    if (job.type === "backup") {
        // 根据 databaseType 分发到对应的备份函数
        // postgres → runPostgresBackup()
        // mysql → runMySqlBackup()
        // mongo → runMongoBackup()
        // mariadb → runMariadbBackup()
        // compose → runComposeBackup()
        // 备份后执行 keepLatestNBackups() 清理旧备份
    } else if (job.type === "server") {
        // 执行 cleanupAll(serverId) - Docker 资源清理
    } else if (job.type === "schedule") {
        // 检查 schedule.enabled 后执行 runCommand(scheduleId)
    } else if (job.type === "volume-backup") {
        // 检查 volumeBackup.enabled 后执行 runVolumeBackup(volumeBackupId)
    }
};
```

所有任务在执行前会检查关联服务器的状态，`inactive` 服务器上的任务会被跳过。

### 7.2 utils.ts - initializeJobs()

系统启动时从数据库加载所有已启用的任务：

1. **服务器清理任务** - 查询 `enableDockerCleanup=true` 且 `serverStatus=active` 的服务器，使用 `CLEANUP_CRON_JOB`（"50 23 * * *"）调度
2. **数据库/Compose 备份** - 查询 `enabled=true` 的 backups，使用各自的 `schedule` cron 表达式
3. **自定义计划任务** - 查询 `enabled=true` 的 schedules，过滤掉关联服务器/应用/Compose 不活跃的任务
4. **卷备份** - 查询 `enabled=true` 的 volumeBackups，过滤掉关联服务器不活跃的任务

调用的 `@dokploy/server` 核心函数：

| 函数 | 来源 | 功能 |
|------|------|------|
| `runPostgresBackup(postgres, backup)` | server 包 | PostgreSQL 备份 |
| `runMySqlBackup(mysql, backup)` | server 包 | MySQL 备份 |
| `runMongoBackup(mongo, backup)` | server 包 | MongoDB 备份 |
| `runMariadbBackup(mariadb, backup)` | server 包 | MariaDB 备份 |
| `runComposeBackup(compose, backup)` | server 包 | Compose 备份 |
| `runVolumeBackup(volumeBackupId)` | server 包 | 卷备份 |
| `keepLatestNBackups(backup, serverId)` | server 包 | 保留最近 N 份备份 |
| `cleanupAll(serverId)` | server 包 | Docker 资源全面清理 |
| `runCommand(scheduleId)` | server 包 | 执行自定义计划命令 |

## 8. 数据库访问

`initializeJobs()` 直接使用 `@dokploy/server/db` 中的 Drizzle ORM 查询：

```typescript
import { db, eq, and, server, backups, schedules, volumeBackups } from "@dokploy/server/db";

// 查询活跃服务器
const servers = await db.query.server.findMany({
    where: and(eq(server.enableDockerCleanup, true), eq(server.serverStatus, "active")),
});

// 查询备份（关联加载）
const backupsResult = await db.query.backups.findMany({
    where: eq(backups.enabled, true),
    with: { mariadb: true, mysql: true, postgres: true, mongo: true, compose: true },
});
```

## 9. 依赖关系

```
Schedules 服务依赖：
├── hono (HTTP 框架)
├── @hono/node-server (Node.js 适配器)
├── @hono/zod-validator (请求验证)
├── bullmq (Redis 任务队列)
├── zod (Schema 验证)
├── pino / pino-pretty (日志)
├── dotenv (环境变量)
├── Redis (BullMQ 存储后端，通过 REDIS_URL)
└── @dokploy/server (核心业务逻辑)
    ├── 备份函数 (postgres/mysql/mongo/mariadb/compose/volume)
    ├── cleanupAll (Docker 清理)
    ├── runCommand (命令执行)
    ├── findBackupById / findScheduleById / findServerById / findVolumeBackupById
    └── db (Drizzle ORM 直接查询)
```

被依赖：
```
├── Dokploy 主服务 (通过 HTTP 调用 /create-backup, /update-backup, /remove-job)
└── Redis (BullMQ 队列存储)
```

## 10. 源文件清单

```
apps/schedules/src/
├── index.ts                             ← Hono 应用入口、路由、优雅关闭
├── schema.ts                            ← Zod Schema（4种任务类型定义）
├── queue.ts                             ← BullMQ 队列管理（创建/调度/移除/查找任务）
├── workers.ts                           ← 3个 BullMQ Worker（并发消费任务）
├── utils.ts                             ← 任务执行逻辑、启动时任务初始化（240行）
└── logger.ts                            ← Pino 日志配置
```

## 11. Go 重写注意事项

- **BullMQ/Redis 替代**: Go 中可使用以下方案：
  - `github.com/hibiken/asynq` - 基于 Redis 的任务队列，原生支持 cron 调度，是最接近的替代方案
  - `github.com/robfig/cron/v3` - 纯 Go cron 调度器，不依赖 Redis，适合嵌入式场景
  - 如果已有 Redis 依赖，`asynq` 是更好的选择；否则可以使用内置 cron 库
- **Worker 模型**: BullMQ 的多 Worker + 高并发模型在 Go 中可通过 goroutine pool 实现，不需要显式创建多个 Worker 实例
- **队列清空与重建**: 启动时 `cleanQueue()` + `initializeJobs()` 的模式可以保留，确保任务状态一致性
- **服务合并可能性**: 与 API 服务类似，后台任务服务可以作为主服务的内部模块，使用 goroutine 和 cron 库实现，消除对独立 Redis 队列的依赖
- **Drizzle ORM 替代**: 数据库查询需迁移到 Go ORM（如 GORM、sqlc、Ent），`initializeJobs()` 中的关联查询（`with` 子句）需要相应调整
- **Zod Schema**: 使用 Go struct + `json` tag + 自定义验证逻辑替代
- **优雅关闭**: Go 的 `os/signal.Notify` + `context.WithCancel` 模式比 Node.js 的 process.on 更规范
- **错误隔离**: 当前所有任务类型共用一个队列，Go 版本可考虑按类型分离队列，提高故障隔离性
