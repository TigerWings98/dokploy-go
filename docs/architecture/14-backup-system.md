# 备份系统

## 1. 模块概述

备份系统负责 Dokploy 中所有数据的定时备份与恢复，支持三大类备份：
1. **数据库备份**（Database Backup）- 支持 PostgreSQL、MySQL、MariaDB、MongoDB 四种数据库
2. **Compose 备份**（Compose Backup）- 对 Compose 服务中的数据库进行备份
3. **卷备份**（Volume Backup）- 对 Docker Volume 进行备份
4. **Web Server 备份** - 对 Dokploy 自身的数据库和文件系统进行完整备份

所有备份通过 **rclone** 工具上传至 S3 兼容的存储目标（Destination），并通过 **node-schedule** 实现 cron 定时调度。

在系统架构中的位置：
```
Cron 调度器 → 备份执行器（utils/backups/） → Docker exec（导出数据） → rclone（上传 S3）
                                             ↓
                                       通知系统（成功/失败）
```

## 2. 数据模型

### 2.1 backups 表

```typescript
// db/schema/backups.ts
export const backups = pgTable("backup", {
    backupId: text("backupId").primaryKey(),
    appName: text("appName").unique(),           // 自动生成的应用名
    schedule: text("schedule").notNull(),         // cron 表达式
    enabled: boolean("enabled"),                  // 是否启用
    database: text("database").notNull(),         // 数据库名
    prefix: text("prefix").notNull(),             // S3 路径前缀
    serviceName: text("serviceName"),             // Compose 服务名
    destinationId: text("destinationId"),         // 关联存储目标
    keepLatestCount: integer("keepLatestCount"),  // 保留最新 N 份
    backupType: backupType("backupType"),         // "database" | "compose"
    databaseType: databaseType("databaseType"),   // "postgres" | "mariadb" | "mysql" | "mongo" | "web-server"
    // 外键: composeId, postgresId, mariadbId, mysqlId, mongoId, userId
    metadata: jsonb("metadata"),                  // Compose 备份时的数据库凭据
});
```

**关系**: 关联 `destinations`（存储目标）、`postgres`/`mysql`/`mariadb`/`mongo`（数据库）、`compose`（Compose 服务）、`deployments`（部署日志）。

### 2.2 destinations 表（存储目标）

```typescript
// db/schema/destination.ts
export const destinations = pgTable("destination", {
    destinationId: text("destinationId").primaryKey(),
    name: text("name").notNull(),
    provider: text("provider"),          // S3 提供商（如 AWS、Minio 等）
    accessKey: text("accessKey"),        // S3 Access Key
    secretAccessKey: text("secretAccessKey"),
    bucket: text("bucket"),              // S3 Bucket 名
    region: text("region"),              // S3 Region
    endpoint: text("endpoint"),          // S3 Endpoint URL
    organizationId: text("organizationId"),
});
```

### 2.3 volumeBackups 表（卷备份）

```typescript
// db/schema/volume-backups.ts
export const volumeBackups = pgTable("volume_backup", {
    volumeBackupId: text("volumeBackupId").primaryKey(),
    name: text("name").notNull(),
    volumeName: text("volumeName").notNull(),     // Docker Volume 名称
    prefix: text("prefix").notNull(),             // S3 路径前缀
    serviceType: serviceType("serviceType"),      // "application" | "postgres" | "mysql" | ...
    appName: text("appName"),                     // 自动生成
    serviceName: text("serviceName"),             // Compose 服务名
    turnOff: boolean("turnOff"),                  // 备份时是否停止服务
    cronExpression: text("cronExpression"),        // cron 表达式
    keepLatestCount: integer("keepLatestCount"),
    enabled: boolean("enabled"),
    destinationId: text("destinationId"),
    // 外键: applicationId, postgresId, mariadbId, mongoId, mysqlId, redisId, composeId
});
```

## 3. 备份执行流程

### 3.1 初始化与 Cron 调度

系统启动时 `initCronJobs()` 加载所有备份任务：

```
initCronJobs()
├── 1. Docker 清理定时任务（enableDockerCleanup）
│   ├── 本地 cleanupAll()
│   └── 远程服务器 cleanupAll(serverId)
├── 2. 加载所有 backups 记录
│   └── 对每个 enabled 备份调用 scheduleBackup()
└── 3. 日志清理任务（logCleanupCron）
```

`scheduleBackup()` 根据 `backupType` 和 `databaseType` 分发到对应执行器：

| backupType | databaseType | 执行函数 |
|------------|-------------|---------|
| database | postgres | `runPostgresBackup()` |
| database | mysql | `runMySqlBackup()` |
| database | mariadb | `runMariadbBackup()` |
| database | mongo | `runMongoBackup()` |
| database | web-server | `runWebServerBackup()` |
| compose | * | `runComposeBackup()` |

### 3.2 数据库备份命令

每种数据库使用对应的原生工具导出：

| 数据库 | 备份命令 | 输出格式 |
|--------|---------|---------|
| PostgreSQL | `pg_dump -Fc --no-acl --no-owner \| gzip` | .sql.gz |
| MySQL | `mysqldump --single-transaction --quick \| gzip` | .sql.gz |
| MariaDB | `mariadb-dump --single-transaction --quick \| gzip` | .sql.gz |
| MongoDB | `mongodump --archive --gzip` | .sql.gz |

所有命令通过 `docker exec -i $CONTAINER_ID bash -c "..."` 在容器内执行。

### 3.3 通用备份执行流程

以 PostgreSQL 为例：

```
runPostgresBackup(postgres, backup)
├── 1. 获取 environment 和 project 信息
├── 2. 创建 deployment 日志记录（createDeploymentBackup）
├── 3. 构建 rclone 上传命令
│   ├── getS3Credentials(destination) → rclone flags
│   └── rclone rcat :s3:{bucket}/{prefix}/{timestamp}.sql.gz
├── 4. 构建完整备份命令（getBackupCommand）
│   ├── 查找运行中容器（docker ps --filter）
│   ├── 执行 pg_dump | gzip（测试运行）
│   └── 执行 pg_dump | gzip | rclone rcat（实际上传）
├── 5. 执行命令
│   ├── 本地: execAsync(backupCommand)
│   └── 远程: execAsyncRemote(serverId, backupCommand)
├── 6. 发送通知（成功/失败）
└── 7. 更新 deployment 状态（done/error）
```

### 3.4 S3 凭据与 rclone

`getS3Credentials()` 将 Destination 配置转换为 rclone 命令行参数：

```bash
--s3-provider="AWS"
--s3-access-key-id="..."
--s3-secret-access-key="..."
--s3-region="us-east-1"
--s3-endpoint="https://s3.amazonaws.com"
--s3-no-check-bucket
--s3-force-path-style
```

上传使用 `rclone rcat`（流式上传），Web Server 备份使用 `rclone copyto`（文件上传）。

### 3.5 Web Server 备份

Web Server 备份是特殊类型，备份 Dokploy 自身：

```
runWebServerBackup()
├── 1. 创建临时目录
├── 2. 找到 dokploy-postgres 容器
├── 3. pg_dump 导出 Dokploy 数据库
├── 4. docker cp 复制 dump 到宿主机
├── 5. rsync 复制文件系统（BASE_PATH）
├── 6. zip 打包（database.sql + filesystem/）
├── 7. rclone copyto 上传到 S3
└── 8. 清理临时目录
```

输出文件格式为 `.zip`，包含数据库 dump 和文件系统。

### 3.6 容器查找

备份前需要找到目标数据库容器：

| 类型 | 查找方式 |
|------|---------|
| Swarm 服务 | `docker ps --filter "label=com.docker.swarm.service.name={appName}"` |
| Compose Stack | `docker ps --filter "label=com.docker.stack.namespace={appName}" --filter "label=com.docker.swarm.service.name={appName}_{serviceName}"` |
| Docker Compose | `docker ps --filter "label=com.docker.compose.project={appName}" --filter "label=com.docker.compose.service={serviceName}"` |

## 4. 备份保留策略

`keepLatestNBackups()` 在每次备份后执行，删除超出保留数量的旧备份：

```bash
# 列出 S3 中的备份文件
rclone lsf {rcloneFlags} --include "*.sql.gz" :s3:{bucket}/{prefix}/
# 按时间倒序排列，跳过最新 N 个，删除其余
| sort -r | tail -n +$((keepLatestCount+1)) | xargs -I{}
# 删除文件
rclone delete {rcloneFlags} :s3:{bucket}/{prefix}/{}
```

Web Server 备份使用 `--include "*.zip"` 过滤。

## 5. 恢复流程

恢复通过 `apiRestoreBackup` schema 定义输入：
- `databaseId` - 目标数据库 ID
- `databaseType` - 数据库类型
- `backupType` - "database" 或 "compose"
- `databaseName` - 数据库名
- `backupFile` - S3 中的备份文件路径
- `destinationId` - 存储目标 ID
- `metadata` - Compose 备份的数据库凭据

## 6. 服务层 CRUD

### 6.1 backup 服务

| 函数 | 功能 |
|------|------|
| `createBackup(input)` | 创建备份配置 |
| `findBackupById(backupId)` | 查找备份（关联 postgres/mysql/mariadb/mongo/destination/compose） |
| `updateBackupById(backupId, data)` | 更新备份配置 |
| `removeBackupById(backupId)` | 删除备份配置 |
| `findBackupsByDbId(id, type)` | 按数据库 ID 查找所有备份 |

### 6.2 destination 服务

| 函数 | 功能 |
|------|------|
| `createDestintation(input, orgId)` | 创建存储目标 |
| `findDestinationById(destinationId)` | 查找存储目标 |
| `updateDestinationById(destinationId, data)` | 更新存储目标 |
| `removeDestinationById(destinationId, orgId)` | 删除存储目标 |

### 6.3 volume-backups 服务

| 函数 | 功能 |
|------|------|
| `createVolumeBackup(data)` | 创建卷备份配置 |
| `findVolumeBackupById(id)` | 查找卷备份（关联 application/postgres/mysql/mariadb/mongo/redis/compose/destination） |
| `updateVolumeBackup(id, data)` | 更新卷备份配置 |
| `removeVolumeBackup(id)` | 删除卷备份配置 |

## 7. 依赖关系

```
备份系统依赖：
├── node-schedule (cron 调度)
├── rclone (S3 上传/管理，系统命令)
├── utils/process/execAsync (命令执行)
├── utils/process/execAsyncRemote (远程命令执行)
├── services/deployment (部署日志)
├── services/destination (存储目标)
├── utils/notifications/database-backup (备份通知)
├── utils/docker/utils (Docker 清理)
└── utils/access-log/handler (日志清理)
```

被依赖：
```
├── 初始化流程（initCronJobs）
├── tRPC Router（备份 CRUD API）
└── 通知系统（备份成功/失败通知）
```

## 8. 源文件清单

```
packages/server/src/
├── db/schema/
│   ├── backups.ts                    ← 备份表定义（backups, databaseType, backupType 枚举）
│   ├── destination.ts                ← 存储目标表定义
│   └── volume-backups.ts             ← 卷备份表定义
├── services/
│   ├── backup.ts                     ← 备份 CRUD 服务
│   ├── destination.ts                ← 存储目标 CRUD 服务
│   └── volume-backups.ts             ← 卷备份 CRUD 服务
└── utils/backups/
    ├── index.ts                      ← initCronJobs(), keepLatestNBackups()
    ├── utils.ts                      ← 通用工具：scheduleBackup, getS3Credentials, getBackupCommand, 各数据库备份命令生成
    ├── postgres.ts                   ← runPostgresBackup()
    ├── mysql.ts                      ← runMySqlBackup()
    ├── mariadb.ts                    ← runMariadbBackup()
    ├── mongo.ts                      ← runMongoBackup()
    ├── compose.ts                    ← runComposeBackup()
    └── web-server.ts                 ← runWebServerBackup()
└── utils/volume-backups/
    ├── index.ts                      ← 卷备份入口
    ├── utils.ts                      ← 卷备份通用工具
    ├── backup.ts                     ← 卷备份执行
    └── restore.ts                    ← 卷备份恢复
```

## 9. Go 重写注意事项

- **Cron 调度**: 使用 `github.com/robfig/cron/v3` 替代 `node-schedule`，两者都支持标准 cron 表达式
- **rclone 命令**: rclone 是系统级工具，所有命令（`rclone rcat`、`rclone lsf`、`rclone delete`、`rclone copyto`）是语言无关的，可直接复用
- **Docker exec 命令**: `pg_dump`、`mysqldump`、`mariadb-dump`、`mongodump` 等命令通过 shell 执行，语言无关
- **S3 SDK 替代**: 可考虑使用 `github.com/aws/aws-sdk-go-v2` 直接操作 S3，替代部分 rclone 调用（如列出文件、删除文件），但保留 rclone 用于流式上传可能更简单
- **临时文件处理**: Web Server 备份中的 `mkdtemp`、`rsync`、`zip` 操作可用 Go 标准库 `os.MkdirTemp()` + `archive/zip` 替代，或继续使用 shell 命令
- **并发安全**: Go 中需要注意 cron job 的并发执行问题，可使用 `sync.Mutex` 或 cron 库的 `WithChain(cron.SkipIfStillRunning())` 防止同一备份任务并发执行
- **日志流写入**: `createWriteStream` 写入部署日志的模式，在 Go 中用 `os.OpenFile` + `bufio.Writer` 替代
- **备份命令模板**: `getBackupCommand()` 生成的 shell 脚本是语言无关的，可直接作为字符串模板复用
