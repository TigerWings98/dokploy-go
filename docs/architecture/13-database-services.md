# 13 - 数据库服务管理

## 1. 模块概述

数据库服务模块负责管理 Dokploy 中的五种数据库类型：MySQL、PostgreSQL、MariaDB、MongoDB、Redis。每种数据库都通过 Docker Swarm Service 进行部署和管理。

模块分为两层：
- **Service 层** (`services/`) — CRUD 操作、部署入口、状态管理
- **Builder 层** (`utils/databases/`) — Docker Swarm Service 配置生成与创建/更新

所有数据库共享统一的部署模式：创建记录 -> 拉取镜像 -> 构建 Docker Service -> 更新状态。

## 2. 设计详解

### 2.1 统一的部署流程

所有数据库的部署函数遵循相同模式：

```typescript
export const deployMySql = async (
    mysqlId: string,
    onData?: (data: any) => void,  // 实时日志回调
) => {
    const mysql = await findMySqlById(mysqlId);
    try {
        // 1. 设置状态为 running
        await updateMySqlById(mysqlId, { applicationStatus: "running" });
        onData?.("Starting mysql deployment...");

        // 2. 拉取 Docker 镜像（区分本地/远程）
        if (mysql.serverId) {
            await execAsyncRemote(mysql.serverId, `docker pull ${mysql.dockerImage}`, onData);
        } else {
            await pullImage(mysql.dockerImage, onData);
        }

        // 3. 创建/更新 Docker Swarm Service
        await buildMysql(mysql);

        // 4. 设置状态为 done
        await updateMySqlById(mysqlId, { applicationStatus: "done" });
        onData?.("Deployment completed successfully!");
    } catch (error) {
        onData?.(`Error: ${error}`);
        // 5. 出错设置状态为 error
        await updateMySqlById(mysqlId, { applicationStatus: "error" });
        throw new TRPCError({ code: "INTERNAL_SERVER_ERROR", message: `Error on deploy mysql${error}` });
    }
    return mysql;
};
```

**状态机：** `idle -> running -> done | error`

### 2.2 创建记录（Service 层）

所有数据库的创建函数共享以下逻辑：

```typescript
export const createMysql = async (input: z.infer<typeof apiCreateMySql>) => {
    // 1. 生成唯一应用名
    const appName = buildAppName("mysql", input.appName);

    // 2. 验证名称唯一性
    const valid = await validUniqueServerAppName(appName);
    if (!valid) {
        throw new TRPCError({ code: "CONFLICT", message: "Service with this 'AppName' already exists" });
    }

    // 3. 插入数据库，自动生成密码
    const newMysql = await db.insert(mysql).values({
        ...input,
        databasePassword: input.databasePassword ? input.databasePassword : generatePassword(),
        databaseRootPassword: input.databaseRootPassword ? input.databaseRootPassword : generatePassword(),
        appName,
    }).returning();

    return newMysql;
};
```

**密码处理：** 如果用户未提供密码，自动调用 `generatePassword()` 生成。

### 2.3 各数据库的环境变量差异

| 数据库 | 环境变量 | 备注 |
|--------|----------|------|
| MySQL | `MYSQL_USER`, `MYSQL_DATABASE`, `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD` | 当 user 为 root 时省略 `MYSQL_USER` 和 `MYSQL_PASSWORD` |
| PostgreSQL | `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | 无 root password |
| MariaDB | `MARIADB_DATABASE`, `MARIADB_USER`, `MARIADB_PASSWORD`, `MARIADB_ROOT_PASSWORD` | 与 MySQL 类似但前缀不同 |
| MongoDB | `MONGO_INITDB_ROOT_USERNAME`, `MONGO_INITDB_ROOT_PASSWORD`[, `MONGO_INITDB_DATABASE`] | replicaSets 模式额外加 `MONGO_INITDB_DATABASE=admin` |
| Redis | `REDIS_PASSWORD` | 仅密码，无用户名 |

#### MySQL 环境变量特殊处理

```typescript
const defaultMysqlEnv =
    databaseUser !== "root"
        ? `MYSQL_USER="${databaseUser}"\nMYSQL_DATABASE="${databaseName}"\nMYSQL_PASSWORD="${databasePassword}"\nMYSQL_ROOT_PASSWORD="${databaseRootPassword}"${env ? `\n${env}` : ""}`
        : `MYSQL_DATABASE="${databaseName}"\nMYSQL_ROOT_PASSWORD="${databaseRootPassword}"${env ? `\n${env}` : ""}`;
```

### 2.4 各数据库的默认端口

| 数据库 | TargetPort |
|--------|-----------|
| MySQL | 3306 |
| PostgreSQL | 5432 |
| MariaDB | 3306 |
| MongoDB | 27017 |
| Redis | 6379 |

### 2.5 Docker Swarm Service 构建

所有数据库共享相同的 Service 构建模式：

```typescript
export const buildMysql = async (mysql: MysqlNested) => {
    // 1. 准备环境变量
    const envVariables = prepareEnvironmentVariables(
        defaultMysqlEnv,            // 数据库专用变量
        mysql.environment.project.env,  // 项目级变量
        mysql.environment.env,          // 环境级变量
    );

    // 2. 生成通用配置
    const { HealthCheck, RestartPolicy, Placement, Labels, Mode,
            RollbackConfig, UpdateConfig, Networks, StopGracePeriod,
            EndpointSpec, Ulimits } = generateConfigContainer(mysql);

    // 3. 计算资源限制
    const resources = calculateResources({
        memoryLimit, memoryReservation, cpuLimit, cpuReservation,
    });

    // 4. 处理挂载
    const volumesMount = generateVolumeMounts(mounts);
    const bindsMount = generateBindMounts(mounts);
    const filesMount = generateFileMounts(appName, mysql);

    // 5. 获取 Docker 客户端（本地或远程）
    const docker = await getRemoteDocker(mysql.serverId);

    // 6. 组装 CreateServiceOptions
    const settings: CreateServiceOptions = {
        Name: appName,
        TaskTemplate: {
            ContainerSpec: {
                Image: dockerImage,
                Env: envVariables,
                Mounts: [...volumesMount, ...bindsMount, ...filesMount],
                // Command, Args, HealthCheck, Labels, Ulimits...
            },
            Networks, RestartPolicy, Placement, Resources: { ...resources },
        },
        Mode, RollbackConfig, UpdateConfig,
        EndpointSpec: EndpointSpec ? EndpointSpec : {
            Mode: "dnsrr",
            Ports: externalPort ? [{ Protocol: "tcp", TargetPort: 3306, PublishedPort: externalPort, PublishMode: "host" }] : [],
        },
    };

    // 7. 创建或更新 Service
    try {
        const service = docker.getService(appName);
        const inspect = await service.inspect();
        await service.update({
            version: parseInt(inspect.Version.Index),
            ...settings,
            TaskTemplate: {
                ...settings.TaskTemplate,
                ForceUpdate: inspect.Spec.TaskTemplate.ForceUpdate + 1,
            },
        });
    } catch {
        await docker.createService(settings);
    }
};
```

**更新策略：** 尝试 `service.inspect()`，成功则更新（`ForceUpdate + 1` 强制重新调度），失败（service 不存在）则创建新 Service。

### 2.6 PostgreSQL 特殊处理 — 版本相关挂载路径

```typescript
export function getMountPath(dockerImage: string): string {
    const versionMatch = dockerImage.match(/postgres:(\d+)/);
    if (versionMatch?.[1]) {
        const version = parseInt(versionMatch[1], 10);
        if (version >= 18) {
            return `/var/lib/postgresql/${version}/docker`;
        }
    }
    return "/var/lib/postgresql/data";
}
```

PostgreSQL 18+ 更改了默认的 `PGDATA` 路径。

### 2.7 MongoDB 特殊处理 — Replica Set 启动脚本

MongoDB 支持 Replica Set 模式，此时使用自定义启动脚本：

```typescript
const startupScript = `
#!/bin/bash
mongod --port 27017 --replSet rs0 --bind_ip_all &
MONGOD_PID=$!

# 等待 MongoDB 就绪
while ! mongosh --eval "db.adminCommand('ping')" > /dev/null 2>&1; do
    sleep 2
done

# 检查 replica set 是否已初始化
REPLICA_STATUS=$(mongosh --quiet --eval "rs.status().ok || 0")

if [ "$REPLICA_STATUS" != "1" ]; then
    echo "Initializing replica set..."
    mongosh --eval '
    rs.initiate({
        _id: "rs0",
        members: [{ _id: 0, host: "${appName}:27017", priority: 1 }]
    });
    while (!rs.isMaster().ismaster) { sleep(1000); }
    db.getSiblingDB("admin").createUser({
        user: "${databaseUser}",
        pwd: "${databasePassword}",
        roles: ["root"]
    });
    '
else
    echo "Replica set already initialized."
fi

wait $MONGOD_PID
`;
```

> **[可复用]** 此 Bash 脚本可直接在 Go 版本中使用，作为容器的 entrypoint。

当启用 Replica Set 时，Service 的 Command 被设为 `["/bin/bash"]`，Args 为 `["-c", startupScript]`，覆盖默认的 `command` 和 `args`。

### 2.8 Redis 特殊处理 — 默认命令

```typescript
// 如果没有自定义 command/args，使用默认的密码认证启动
...(command || args
    ? { /* 使用自定义 command/args */ }
    : {
        Command: ["/bin/sh"],
        Args: ["-c", `redis-server --requirepass ${databasePassword}`],
    }),
```

> **[可复用]** `redis-server --requirepass {password}` 是标准 Redis 启动命令。

### 2.9 查询模式

所有数据库的 `findById` 查询都加载关联数据：

```typescript
export const findMySqlById = async (mysqlId: string) => {
    const result = await db.query.mysql.findFirst({
        where: eq(mysql.mysqlId, mysqlId),
        with: {
            environment: { with: { project: true } },  // 环境变量 + 项目级变量
            mounts: true,                                // 挂载配置
            server: true,                                // 远程服务器信息
            backups: { with: { destination: true, deployments: true } },  // 备份配置
        },
    });
};
```

Redis 是例外——不加载 `backups`（Redis 不支持通过 Dokploy 备份）。

### 2.10 备份关联查询

MySQL、PostgreSQL、MariaDB、MongoDB 提供通过 backupId 反查数据库记录的函数：

```typescript
export const findMySqlByBackupId = async (backupId: string) => {
    return await db.select({ ...getTableColumns(mysql) })
        .from(mysql)
        .innerJoin(backups, eq(mysql.mysqlId, backups.mysqlId))
        .where(eq(backups.backupId, backupId))
        .limit(1);
};
```

## 3. 源文件清单

| 文件路径 | 说明 |
|----------|------|
| `packages/server/src/services/mysql.ts` | MySQL Service 层：CRUD、部署、备份查询 |
| `packages/server/src/services/postgres.ts` | PostgreSQL Service 层：CRUD、部署、备份查询、版本路径 |
| `packages/server/src/services/mariadb.ts` | MariaDB Service 层：CRUD、部署、备份查询 |
| `packages/server/src/services/mongo.ts` | MongoDB Service 层：CRUD、部署、备份查询、Compose 备份查询 |
| `packages/server/src/services/redis.ts` | Redis Service 层：CRUD、部署（无备份） |
| `packages/server/src/utils/databases/mysql.ts` | MySQL Docker Service 构建 |
| `packages/server/src/utils/databases/postgres.ts` | PostgreSQL Docker Service 构建 |
| `packages/server/src/utils/databases/mariadb.ts` | MariaDB Docker Service 构建 |
| `packages/server/src/utils/databases/mongo.ts` | MongoDB Docker Service 构建（含 Replica Set 脚本） |
| `packages/server/src/utils/databases/redis.ts` | Redis Docker Service 构建（含默认命令） |
| `packages/server/src/utils/databases/rebuild.ts` | 通用数据库重建函数（rebuildDatabase）：移除服务→清理卷→重新部署 |

## 4. 对外接口

### 各数据库 Service 层接口（以 MySQL 为例，其他类似）

```typescript
export type MySql = typeof mysql.$inferSelect

export const createMysql: (input: z.infer<typeof apiCreateMySql>) => Promise<MySql>
export const findMySqlById: (mysqlId: string) => Promise<MySqlNested>
export const updateMySqlById: (mysqlId: string, data: Partial<MySql>) => Promise<MySql>
export const removeMySqlById: (mysqlId: string) => Promise<MySql>
export const findMySqlByBackupId: (backupId: string) => Promise<MySql>
export const deployMySql: (mysqlId: string, onData?: (data: any) => void) => Promise<MySqlNested>
```

### PostgreSQL 特有

```typescript
export function getMountPath(dockerImage: string): string  // 版本相关的数据目录
```

### MongoDB 特有

```typescript
export const findComposeByBackupId: (backupId: string) => Promise<Compose>  // Compose 备份查询（定义在 mongo.ts）
```

### 通用重建接口

```typescript
// utils/databases/rebuild.ts
export const rebuildDatabase: (databaseId: string, type: "postgres" | "mysql" | "mariadb" | "mongo" | "redis") => Promise<void>
// 流程: removeService → 等待6s → 清理 volume mounts → 重新 deploy
```

### Builder 层接口

```typescript
export const buildMysql: (mysql: MysqlNested) => Promise<void>
export const buildPostgres: (postgres: PostgresNested) => Promise<void>
export const buildMariadb: (mariadb: MariadbNested) => Promise<void>
export const buildMongo: (mongo: MongoNested) => Promise<void>
export const buildRedis: (redis: RedisNested) => Promise<void>
```

### 嵌套类型定义

```typescript
// 所有数据库的嵌套类型都遵循相同模式
export type MysqlNested = InferResultType<
    "mysql",
    { mounts: true; environment: { with: { project: true } } }
>;
```

## 5. 依赖关系

### 上游依赖

| 依赖 | 用途 |
|------|------|
| `drizzle-orm` + 数据库 | CRUD 操作 |
| `dockerode` (CreateServiceOptions) | Docker Swarm Service 配置类型 |
| `@dokploy/server/utils/docker/utils` | calculateResources, generateConfigContainer, generateVolumeMounts, generateBindMounts, generateFileMounts, prepareEnvironmentVariables, pullImage |
| `@dokploy/server/utils/servers/remote-docker` | getRemoteDocker（获取远程 Docker 客户端） |
| `@dokploy/server/utils/process/execAsync` | execAsyncRemote（远程镜像拉取） |
| `@dokploy/server/templates` | generatePassword（密码生成） |
| `@dokploy/server/db/schema` | 数据库 Schema、buildAppName |
| `services/project` | validUniqueServerAppName（名称唯一性校验） |

### 下游消费者

- **tRPC API 路由** — 调用 CRUD 和 deploy 函数
- **备份模块** — 调用 `findXxxByBackupId` 获取数据库配置
- **监控/状态查询** — 读取 `applicationStatus` 字段

## 6. Go 重写注意事项

### Docker Service 构建

Go 中使用 `github.com/docker/docker/client` 包的 `ServiceCreate` / `ServiceUpdate` API。`CreateServiceOptions` 的结构需要映射到 Go 的 `swarm.ServiceSpec`。

核心模式保持一致：先尝试 `ServiceInspectWithRaw`，成功则 `ServiceUpdate`（带 `ForceUpdate + 1`），失败则 `ServiceCreate`。

### 可复用的配置值

以下环境变量模板可直接在 Go 中使用：

```bash
# MySQL
MYSQL_USER="{user}"
MYSQL_DATABASE="{dbname}"
MYSQL_PASSWORD="{password}"
MYSQL_ROOT_PASSWORD="{rootPassword}"

# PostgreSQL
POSTGRES_DB="{dbname}"
POSTGRES_USER="{user}"
POSTGRES_PASSWORD="{password}"

# MariaDB
MARIADB_DATABASE="{dbname}"
MARIADB_USER="{user}"
MARIADB_PASSWORD="{password}"
MARIADB_ROOT_PASSWORD="{rootPassword}"

# MongoDB
MONGO_INITDB_ROOT_USERNAME="{user}"
MONGO_INITDB_ROOT_PASSWORD="{password}"

# Redis
REDIS_PASSWORD="{password}"
```

默认端口映射（3306, 5432, 3306, 27017, 6379）也完全可复用。

### 可复用的 Shell 脚本

MongoDB Replica Set 启动脚本是纯 Bash，可以作为字符串模板在 Go 中生成。Redis 的 `redis-server --requirepass {password}` 也可直接复用。

### 通用 Builder 抽象

建议在 Go 中提取通用的数据库 Builder 接口：

```go
type DatabaseBuilder interface {
    DefaultEnvVars() map[string]string
    DefaultPort() int
    Build(ctx context.Context) error
}
```

五种数据库实现此接口，共享环境变量合并、资源计算、挂载生成、Service 创建/更新的通用逻辑。

### 密码生成

`generatePassword()` 在 Go 中可使用 `crypto/rand` 实现，确保密码满足各数据库的复杂度要求。

### onData 回调

TypeScript 中的 `onData?: (data: any) => void` 回调用于实时日志推送。Go 版本可使用 `io.Writer` 或 channel 替代：

```go
func DeployMySQL(ctx context.Context, mysqlID string, logWriter io.Writer) error
```
