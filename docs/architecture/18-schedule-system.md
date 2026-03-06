# 18. 定时任务系统

## 1. 模块概述

定时任务系统由两部分组成：一个独立的微服务（`apps/schedules`）和服务端的 Schedule 服务（`packages/server`）。微服务负责管理和执行所有基于 Cron 表达式的定时任务，使用 Redis + BullMQ 作为任务队列后端；服务端 Schedule 服务负责定时任务的 CRUD 和脚本管理。系统支持四种任务类型：数据库备份（backup）、Docker 清理（server）、自定义定时命令（schedule）和卷备份（volume-backup）。

**在系统中的角色：** 定时任务系统是平台的任务调度核心，作为独立进程运行，通过 HTTP API 接收主服务的任务管理请求，通过 BullMQ Worker 异步执行任务。它连接了备份系统、Docker 清理系统和自定义脚本执行功能。

## 2. 设计详解

### 2.1 系统架构

```
                    HTTP API (Hono)
                    /create-backup
                    /update-backup
主服务 (Dokploy) ---/remove-job------> Schedules 微服务
                    /health              |
                                         |
                    X-API-Key 认证       |
                                         v
                                    BullMQ Queue (Redis)
                                    "backupQueue"
                                         |
                                    +----+----+
                                    |    |    |
                                Worker1 Worker2 Worker3
                                (concurrency: 100 each)
                                         |
                                         v
                                    runJobs(job.data)
                                    |-- backup: 数据库备份
                                    |-- server: Docker 清理
                                    |-- schedule: 自定义命令
                                    |-- volume-backup: 卷备份
```

### 2.2 数据结构

#### 任务队列 Schema（apps/schedules 内部使用）

```typescript
export const jobQueueSchema = z.discriminatedUnion("type", [
  z.object({
    cronSchedule: z.string(),
    type: z.literal("backup"),
    backupId: z.string(),
  }),
  z.object({
    cronSchedule: z.string(),
    type: z.literal("server"),
    serverId: z.string(),
  }),
  z.object({
    cronSchedule: z.string(),
    type: z.literal("schedule"),
    scheduleId: z.string(),
    timezone: z.string().optional(),
  }),
  z.object({
    cronSchedule: z.string(),
    type: z.literal("volume-backup"),
    volumeBackupId: z.string(),
  }),
]);
```

#### 定时任务数据库表（packages/server 中定义）

```typescript
export const shellTypes = pgEnum("shellType", ["bash", "sh"]);
export const scheduleType = pgEnum("scheduleType", [
  "application",      // 在应用容器内执行命令
  "compose",          // 在 Compose 服务容器内执行命令
  "server",           // 在远程服务器上执行脚本
  "dokploy-server",   // 在 Dokploy 主服务器上执行脚本
]);

export const schedules = pgTable("schedule", {
  scheduleId: text("scheduleId").notNull().primaryKey(),
  name: text("name").notNull(),
  cronExpression: text("cronExpression").notNull(),
  appName: text("appName").notNull(),        // 自动生成，如 "schedule-xxx"
  serviceName: text("serviceName"),           // Docker 服务名（application/compose 类型使用）
  shellType: shellTypes("shellType").notNull().default("bash"),
  scheduleType: scheduleType("scheduleType").notNull().default("application"),
  command: text("command").notNull(),         // 在容器内执行的命令
  script: text("script"),                     // 在宿主机执行的脚本（server/dokploy-server 类型）
  applicationId: text("applicationId"),       // 关联应用（可选，application 类型）
  composeId: text("composeId"),              // 关联 Compose（可选，compose 类型）
  serverId: text("serverId"),                // 关联服务器（可选，server 类型）
  userId: text("userId"),                    // 创建者
  enabled: boolean("enabled").notNull().default(true),
  timezone: text("timezone"),                // 时区设置
  createdAt: text("createdAt").notNull(),
});
```

**关系映射：**
- `schedule -> application`（可选一对一）
- `schedule -> compose`（可选一对一）
- `schedule -> server`（可选一对一）
- `schedule -> user`（多对一）
- `schedule -> deployments`（一对多，记录执行历史）

### 2.3 Schedules 微服务 HTTP API

使用 Hono 框架提供 REST API，通过 `X-API-Key` Header 进行认证：

```typescript
// 中间件：API Key 认证（/health 路径跳过）
app.use(async (c, next) => {
  if (c.req.path === "/health") return next();
  const authHeader = c.req.header("X-API-Key");
  if (process.env.API_KEY !== authHeader) {
    return c.json({ message: "Invalid API Key" }, 403);
  }
  return next();
});
```

| 端点 | 方法 | 请求 Body | 功能 |
|------|------|----------|------|
| `POST /create-backup` | POST | `QueueJob` | 创建定时任务 |
| `POST /update-backup` | POST | `QueueJob` | 更新定时任务（先删旧再创新） |
| `POST /remove-job` | POST | `QueueJob` | 删除定时任务 |
| `GET /health` | GET | 无 | 健康检查，返回 `{ status: "ok" }` |

#### 更新任务流程

更新操作采用"先查-后删-再创建"的模式：

```typescript
app.post("/update-backup", async (c) => {
  const data = c.req.valid("json");
  // 1. 查找现有的 repeatable job（通过 name 匹配）
  const job = await getJobRepeatable(data);
  if (job) {
    // 2. 删除旧的 repeatable job（需要知道旧的 cron pattern）
    await removeJob({ ...识别字段, cronSchedule: job.pattern || "" });
  }
  // 3. 用新参数（可能包含新的 cronSchedule）创建任务
  scheduleJob(data);
});
```

### 2.4 BullMQ 队列管理

#### 队列配置

```typescript
export const jobQueue = new Queue("backupQueue", {
  connection: { url: process.env.REDIS_URL },
  defaultJobOptions: {
    removeOnComplete: true,  // 完成后自动清理 job 数据
    removeOnFail: true,      // 失败后自动清理 job 数据
  },
});
```

#### 启动清理

系统启动时先清空队列，然后重新从数据库加载所有任务：

```typescript
cleanQueue();       // 调用 jobQueue.obliterate({ force: true })
initializeJobs();   // 从 DB 重新加载所有任务
```

#### 任务调度

```typescript
export const scheduleJob = (job: QueueJob) => {
  if (job.type === "backup") {
    jobQueue.add(job.backupId, job, {
      repeat: { pattern: job.cronSchedule },
    });
  } else if (job.type === "server") {
    jobQueue.add(`${job.serverId}-cleanup`, job, {
      repeat: { pattern: job.cronSchedule },
    });
  } else if (job.type === "schedule") {
    jobQueue.add(job.scheduleId, job, {
      repeat: {
        pattern: job.cronSchedule,
        tz: job.timezone || "UTC",  // 仅 schedule 类型支持时区
      },
    });
  } else if (job.type === "volume-backup") {
    jobQueue.add(job.volumeBackupId, job, {
      repeat: { pattern: job.cronSchedule },
    });
  }
};
```

任务命名规则（用于查找和删除）：
- backup: `{backupId}`
- server: `{serverId}-cleanup`
- schedule: `{scheduleId}`
- volume-backup: `{volumeBackupId}`

#### 删除和查找任务

```typescript
// 删除：通过 name + cron pattern 定位 repeatable job
export const removeJob = async (data: QueueJob) => {
  return await jobQueue.removeRepeatable(jobName, { pattern: data.cronSchedule });
};

// 查找：遍历所有 repeatable jobs，按 name 匹配
export const getJobRepeatable = async (data: QueueJob): Promise<RepeatableJob | null> => {
  const repeatableJobs = await jobQueue.getRepeatableJobs();
  return repeatableJobs.find((j) => j.name === jobName) || null;
};
```

#### Worker 配置

系统启动 3 个并行 Worker 实例，共享同一个队列：

```typescript
export const firstWorker = new Worker(
  "backupQueue",
  async (job: Job<QueueJob>) => { await runJobs(job.data); },
  { concurrency: 100, connection: { url: process.env.REDIS_URL } },
);
// secondWorker 和 thirdWorker 配置完全相同
```

总并发能力：3 Workers x 100 concurrency = 300 并发任务。

### 2.5 任务执行逻辑

`runJobs` 函数根据任务类型分发执行：

```typescript
export const runJobs = async (job: QueueJob) => {
  if (job.type === "backup") {
    const backup = await findBackupById(job.backupId);
    const server = await findServerById(serverId);
    if (server.serverStatus === "inactive") return;  // 跳过不活跃服务器
    // 根据数据库类型执行对应备份
    if (databaseType === "postgres") await runPostgresBackup(postgres, backup);
    else if (databaseType === "mysql") await runMySqlBackup(mysql, backup);
    else if (databaseType === "mongo") await runMongoBackup(mongo, backup);
    else if (databaseType === "mariadb") await runMariadbBackup(mariadb, backup);
    // Compose 备份
    if (backupType === "compose") await runComposeBackup(compose, backup);
    // 清理旧备份
    await keepLatestNBackups(backup, server.serverId);
  } else if (job.type === "server") {
    if (server.serverStatus === "inactive") return;
    await cleanupAll(job.serverId);  // Docker 资源清理
  } else if (job.type === "schedule") {
    const schedule = await findScheduleById(job.scheduleId);
    if (schedule.enabled) await runCommand(schedule.scheduleId);  // 执行自定义命令
  } else if (job.type === "volume-backup") {
    const volumeBackup = await findVolumeBackupById(job.volumeBackupId);
    if (volumeBackup.enabled) await runVolumeBackup(job.volumeBackupId);
  }
};
```

### 2.6 启动初始化

系统启动时从数据库加载所有已启用的任务并注册到队列：

```typescript
export const initializeJobs = async () => {
  // 1. Docker 清理任务：查找所有 enableDockerCleanup=true 且 serverStatus=active 的服务器
  const servers = await db.query.server.findMany({
    where: and(eq(server.enableDockerCleanup, true), eq(server.serverStatus, "active")),
  });
  for (const srv of servers)
    scheduleJob({ serverId: srv.serverId, type: "server", cronSchedule: CLEANUP_CRON_JOB });

  // 2. 数据库备份任务：查找所有 enabled=true 的备份配置
  const backupsResult = await db.query.backups.findMany({ where: eq(backups.enabled, true) });
  for (const backup of backupsResult)
    scheduleJob({ backupId: backup.backupId, type: "backup", cronSchedule: backup.schedule });

  // 3. 自定义定时任务：查找所有 enabled=true 的 schedule，过滤掉关联服务器状态为 inactive 的
  const schedulesResult = await db.query.schedules.findMany({
    where: eq(schedules.enabled, true),
    with: { application: { with: { server: true } }, compose: { with: { server: true } }, server: true },
  });
  const filtered = schedulesResult.filter(s => {
    if (s.server) return s.server.serverStatus === "active";
    if (s.application) return s.application.server?.serverStatus === "active";
    if (s.compose) return s.compose.server?.serverStatus === "active";
  });
  for (const schedule of filtered)
    scheduleJob({ scheduleId: schedule.scheduleId, type: "schedule", cronSchedule: schedule.cronExpression });

  // 4. 卷备份任务：查找所有 enabled=true 的卷备份，同样过滤 inactive 服务器
  // ...
};
```

### 2.7 Schedule 服务端管理（services/schedule.ts）

#### 脚本文件管理

对于 `server` 和 `dokploy-server` 类型的定时任务，系统将脚本写入文件系统：

```typescript
const handleScript = async (schedule: Schedule) => {
  const { SCHEDULES_PATH } = paths(!!schedule?.serverId);
  const fullPath = path.join(SCHEDULES_PATH, schedule?.appName || "");

  // 脚本头部自动注入 PID 和 Schedule ID 信息
  const scriptWithPid = `echo "PID: $$ | Schedule ID: ${schedule.scheduleId}"
${schedule?.script || ""}`;

  const encodedContent = encodeBase64(scriptWithPid);
  const script = `
    mkdir -p ${fullPath}
    rm -f ${fullPath}/script.sh
    touch ${fullPath}/script.sh
    chmod +x ${fullPath}/script.sh
    echo "${encodedContent}" | base64 -d > ${fullPath}/script.sh
  `;

  if (schedule?.scheduleType === "dokploy-server") {
    await execAsync(script);           // 本地执行
  } else if (schedule?.scheduleType === "server") {
    await execAsyncRemote(schedule?.serverId || "", script);  // SSH 远程执行
  }
};
```

#### CRUD 操作

- **创建**：插入 DB 后，如果是 `server`/`dokploy-server` 类型，调用 `handleScript` 写入脚本文件
- **更新**：更新 DB 后，同样条件下调用 `handleScript` 更新脚本文件
- **删除**：先 `rm -rf` 删除脚本目录（本地或远程），再从 DB 删除记录

```typescript
export const deleteSchedule = async (scheduleId: string) => {
  const schedule = await findScheduleById(scheduleId);
  const serverId = schedule?.serverId || schedule?.application?.serverId || schedule?.compose?.serverId;
  const { SCHEDULES_PATH } = paths(!!serverId);
  const fullPath = path.join(SCHEDULES_PATH, schedule?.appName || "");
  const command = `rm -rf ${fullPath}`;
  if (serverId) await execAsyncRemote(serverId, command);
  else await execAsync(command);
  await db.delete(schedules).where(eq(schedules.scheduleId, scheduleId));
};
```

### 2.8 优雅关闭

```typescript
export const gracefulShutdown = async (signal: string) => {
  logger.warn(`Received ${signal}, closing server...`);
  await firstWorker.close();
  await secondWorker.close();
  await thirdWorker.close();
  process.exit(0);
};
process.on("SIGINT", () => gracefulShutdown("SIGINT"));
process.on("SIGTERM", () => gracefulShutdown("SIGTERM"));
process.on("uncaughtException", (err) => { logger.error(err, "Uncaught exception"); });
process.on("unhandledRejection", (reason, promise) => { logger.error({promise, reason}, "Unhandled Rejection"); });
```

## 3. 源文件清单

### Schedules 微服务（独立进程）
- `dokploy/apps/schedules/src/index.ts` — HTTP 服务入口（Hono 路由、API Key 中间件、启动初始化、优雅关闭信号处理）
- `dokploy/apps/schedules/src/schema.ts` — 任务队列 Schema（`jobQueueSchema`、`QueueJob` 类型）
- `dokploy/apps/schedules/src/queue.ts` — BullMQ 队列管理（`jobQueue` 实例、`scheduleJob`、`removeJob`、`getJobRepeatable`、`cleanQueue`）
- `dokploy/apps/schedules/src/workers.ts` — 3 个 Worker 实例（`firstWorker`、`secondWorker`、`thirdWorker`）
- `dokploy/apps/schedules/src/utils.ts` — 任务执行逻辑（`runJobs`）、启动初始化（`initializeJobs`）
- `dokploy/apps/schedules/src/logger.ts` — Pino 日志配置（pino-pretty 彩色输出）

### 服务端定时任务管理
- `dokploy/packages/server/src/services/schedule.ts` — Schedule CRUD（`createSchedule`、`findScheduleById`、`findScheduleOrganizationId`、`deleteSchedule`、`updateSchedule`）、脚本管理（`handleScript`）
- `dokploy/packages/server/src/db/schema/schedule.ts` — 数据库表结构（`schedules`）、枚举（`shellTypes`、`scheduleType`）、关系映射、Schema（`createScheduleSchema`、`updateScheduleSchema`）

## 4. 对外接口

### HTTP API（Schedules 微服务）

```
POST /create-backup    Body: QueueJob    Auth: X-API-Key    -> { message: string }
POST /update-backup    Body: QueueJob    Auth: X-API-Key    -> { message: string }
POST /remove-job       Body: QueueJob    Auth: X-API-Key    -> { message: string, result: boolean }
GET  /health           无需认证                              -> { status: "ok" }
```

`QueueJob` 类型为 discriminated union（按 `type` 字段区分），公共字段 `cronSchedule: string`。

### 队列管理函数（queue.ts）

```typescript
scheduleJob(job: QueueJob): void
removeJob(data: QueueJob): Promise<boolean>
getJobRepeatable(data: QueueJob): Promise<RepeatableJob | null>
cleanQueue(): Promise<void>
```

### 任务执行函数（utils.ts）

```typescript
runJobs(job: QueueJob): Promise<boolean>
initializeJobs(): Promise<void>
```

### Schedule 服务（schedule.ts）

```typescript
createSchedule(input: CreateScheduleSchema): Promise<Schedule>
findScheduleById(scheduleId: string): Promise<ScheduleExtended>
  // with: application -> environment -> project, compose -> environment -> project, server -> organization
findScheduleOrganizationId(scheduleId: string): Promise<string | null>
deleteSchedule(scheduleId: string): Promise<boolean>
updateSchedule(input: UpdateScheduleSchema): Promise<Schedule>
```

## 5. 依赖关系

### Schedules 微服务上游依赖
- `bullmq` — Redis 任务队列 + Worker
- `hono` + `@hono/node-server` — HTTP 框架
- `@hono/zod-validator` — 请求 Body 验证
- `pino` + `pino-pretty` — 结构化日志
- Redis（通过 `REDIS_URL` 环境变量）— 队列后端存储
- `@dokploy/server` — 服务端核心库（`findBackupById`、`findScheduleById`、`findServerById`、`findVolumeBackupById`、`runPostgresBackup`、`runMySqlBackup`、`runMongoBackup`、`runMariadbBackup`、`runComposeBackup`、`runVolumeBackup`、`cleanupAll`、`runCommand`、`keepLatestNBackups`、`CLEANUP_CRON_JOB`）
- `drizzle-orm` — 数据库 ORM（`initializeJobs` 查询）
- 环境变量: `REDIS_URL`、`API_KEY`、`PORT`（默认 3000）

### Schedule 服务上游依赖
- `drizzle-orm` — 数据库操作
- `@trpc/server`（`TRPCError`）— 错误类型
- `execAsync` / `execAsyncRemote` — 命令执行
- `encodeBase64` — 脚本内容编码
- `paths` — 获取 `SCHEDULES_PATH` 常量

### 下游被依赖
- 主服务通过 HTTP API 调用 Schedules 微服务管理任务生命周期
- Backup 创建/更新/删除时同步调用微服务 API
- Schedule 创建/更新/删除时同步调用微服务 API
- 服务器 Docker 清理设置变更时调用微服务 API
- VolumeBackup 创建/更新/删除时同步调用微服务 API

## 6. Go 重写注意事项

### 可直接复用的部分

1. **Cron 表达式格式**：标准 5/6 字段 Cron 格式，Go 可使用 `robfig/cron/v3` 库
2. **Shell 脚本模板**：`handleScript` 中的脚本文件创建命令（`mkdir -p`、`chmod +x`、`base64 -d`）可直接复用
3. **备份执行命令**：`runPostgresBackup`、`runMySqlBackup` 等底层使用的 `pg_dump`、`mysqldump`、`mongodump` 等 shell 命令
4. **Docker 清理命令**：`cleanupAll` 底层的 `docker system prune` 等命令
5. **Redis 队列模型**：任务数据结构（type + id + cron）和 repeatable job 模式
6. **API 认证模式**：`X-API-Key` Header 认证
7. **任务命名规则**：`{backupId}`、`{serverId}-cleanup`、`{scheduleId}`、`{volumeBackupId}`

### 需要重新实现的部分

1. **队列系统**：BullMQ 是 Node.js 专属库，Go 可选方案：
   - `hibiken/asynq` — 基于 Redis 的 Go 任务队列（最接近 BullMQ 的 repeatable job 语义）
   - `robfig/cron/v3` — 进程内 Cron 调度器（如果不需要分布式特性）
   - 内嵌调度 + Redis 持久化 — 自行管理 cron schedule
2. **HTTP 框架**：Hono -> Go 标准 `net/http` 或 `gin`/`echo`/`fiber`
3. **Worker 模型**：3 个 BullMQ Worker -> goroutine pool（如 `workerpool`）
4. **日志**：Pino -> `zerolog` 或 `zap`

### 架构优化建议

1. **合并为单进程**：Go 版本可考虑将 Schedules 微服务合并到主进程中，使用 goroutine 管理定时任务。优势：
   - 消除 HTTP API 通信开销和网络故障点
   - 共享数据库连接池
   - 简化部署架构（不需要额外的 Redis）
   - 使用 `robfig/cron/v3` 可在进程内高效管理数千个定时任务

2. **持久化策略**：如果合并为单进程，定时任务的注册状态完全来自数据库，进程重启时重新加载（与当前 `initializeJobs` 逻辑一致）

3. **服务器状态检查优化**：当前 `runJobs` 每次执行都查询服务器状态，可以考虑缓存或在注册时过滤

4. **健康检查增强**：当前 `/health` 只返回 OK，建议增加 Redis 连接状态和 Worker 活跃度检查

```go
// 建议的 Go 调度器设计（合并到主进程）
type Scheduler struct {
    cron     *cron.Cron
    db       *gorm.DB
    entryMap map[string]cron.EntryID  // jobKey -> cron entry
    mu       sync.RWMutex
    logger   *zap.Logger
}

func (s *Scheduler) Start(ctx context.Context) error {
    // 从 DB 加载所有任务
    if err := s.initializeJobs(ctx); err != nil {
        return err
    }
    s.cron.Start()
    return nil
}

func (s *Scheduler) AddJob(key string, cronExpr string, timezone string, fn func()) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    // 如果已存在则先删除
    if entryID, ok := s.entryMap[key]; ok {
        s.cron.Remove(entryID)
    }
    spec := cronExpr
    if timezone != "" {
        spec = fmt.Sprintf("CRON_TZ=%s %s", timezone, cronExpr)
    }
    entryID, err := s.cron.AddFunc(spec, fn)
    if err != nil {
        return err
    }
    s.entryMap[key] = entryID
    return nil
}

func (s *Scheduler) RemoveJob(key string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if entryID, ok := s.entryMap[key]; ok {
        s.cron.Remove(entryID)
        delete(s.entryMap, key)
    }
}

func (s *Scheduler) Stop() {
    s.cron.Stop()
}
```
