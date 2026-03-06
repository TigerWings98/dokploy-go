# 多服务器与集群管理

## 1. 模块概述

多服务器与集群管理模块负责 Dokploy 的分布式基础设施能力，包括远程服务器注册、SSH 连接验证、远程 Docker 引擎交互、Docker Swarm 集群管理、构建服务器功能、以及远程服务器上的 Traefik 反向代理配置。

系统支持两种服务器类型：
1. **部署服务器**（`deploy`）- 运行应用服务，安装完整的 Traefik、Swarm、网络等基础设施
2. **构建服务器**（`build`）- 仅用于构建镜像，安装精简的依赖（Docker、Nixpacks、Buildpacks、Railpack）

在系统架构中的位置：
```
tRPC Router → 服务层(server.ts) → SSH 连接 → 远程服务器
                                  → 远程 Docker(remote-docker.ts)
                                  → 服务器初始化(server-setup.ts)
                                  → 镜像分发(cluster/upload.ts)
```

## 2. 数据模型

### 2.1 server 表

```typescript
// db/schema/server.ts
export const server = pgTable("server", {
    serverId: text("serverId").primaryKey(),       // nanoid 生成
    name: text("name").notNull(),                  // 服务器名称
    description: text("description"),              // 描述
    ipAddress: text("ipAddress").notNull(),         // IP 地址
    port: integer("port").notNull(),               // SSH 端口
    username: text("username").default("root"),     // SSH 用户名
    appName: text("appName"),                      // 自动生成的应用名
    enableDockerCleanup: boolean("enableDockerCleanup").default(false),
    serverStatus: serverStatus("serverStatus").default("active"),  // "active" | "inactive"
    serverType: serverType("serverType").default("deploy"),        // "deploy" | "build"
    command: text("command").default(""),           // 自定义安装命令（覆盖默认脚本）
    sshKeyId: text("sshKeyId"),                    // 关联的 SSH 密钥
    organizationId: text("organizationId"),        // 所属组织
    metricsConfig: jsonb("metricsConfig"),         // 监控配置（端口、Token、刷新率、阈值等）
});
```

### 2.2 关联关系

服务器与以下实体存在关联：
- `sshKey` - SSH 密钥（一对一）
- `applications` / `buildApplications` - 部署应用 / 构建应用（一对多）
- `compose` - Compose 服务
- `redis` / `mariadb` / `mongo` / `mysql` / `postgres` - 数据库服务
- `certificates` - TLS 证书
- `deployments` / `buildDeployments` - 部署记录
- `schedules` - 定时任务
- `organization` - 所属组织

## 3. 服务器注册与管理

### 3.1 CRUD 操作

```typescript
// services/server.ts
export const createServer = async (input, organizationId) => {
    // 插入服务器记录，自动生成 serverId (nanoid)
    return db.insert(server).values({ ...input, organizationId });
};

export const findServerById = async (serverId) => {
    // 查询服务器，with: { deployments, sshKey }
};

export const haveActiveServices = async (serverId) => {
    // 检查服务器是否有活跃服务（applications + compose + 5种数据库）
    // 用于删除前的安全检查
};

export const deleteServer = async (serverId) => { ... };
export const updateServerById = async (serverId, data) => { ... };
export const getAllServers = async () => { ... };
```

### 3.2 API Schema

| Schema | 字段 | 用途 |
|--------|------|------|
| `apiCreateServer` | name, description, ipAddress, port, username, sshKeyId, serverType | 创建服务器 |
| `apiUpdateServer` | 同上 + serverId + command | 更新服务器 |
| `apiUpdateServerMonitoring` | serverId, metricsConfig | 更新监控配置 |

## 4. SSH 连接与远程 Docker

### 4.1 远程 Docker 客户端

```typescript
// utils/servers/remote-docker.ts
export const getRemoteDocker = async (serverId?: string | null) => {
    if (!serverId) return docker;           // 无 serverId 返回本地客户端
    const server = await findServerById(serverId);
    if (!server.sshKeyId) return docker;    // 无 SSH 密钥返回本地客户端
    return new Dockerode({
        host: server.ipAddress,
        port: server.port,
        username: server.username,
        protocol: "ssh",
        sshOptions: { privateKey: server.sshKey?.privateKey },
    });
};
```

该函数是整个远程操作的基础，被数据库部署（`buildMysql`、`buildPostgres` 等）、Docker 工具函数、服务管理等模块广泛调用。

### 4.2 SSH 连接验证

服务器初始化时通过 `ssh2` 库的 `Client` 建立 SSH 连接：

```typescript
// setup/server-setup.ts - installRequirements()
const client = new Client();
client
    .once("ready", () => {
        client.exec(command, (err, stream) => { /* 执行安装脚本 */ });
    })
    .on("error", (err) => {
        if (err.level === "client-authentication") {
            // SSH 密钥认证失败 - 提供友好的错误提示
        } else {
            // 连接失败 - IP/端口/防火墙等问题
        }
    })
    .connect({
        host: server.ipAddress,
        port: server.port,
        username: server.username,
        privateKey: server.sshKey?.privateKey,
    });
```

## 5. 服务器初始化流程

### 5.1 总体流程

```typescript
// setup/server-setup.ts
export const serverSetup = async (serverId, onData?) => {
    // 1. 创建日志目录
    // 2. 创建 deployment 记录
    // 3. 通过 SSH 执行安装脚本 (installRequirements)
    // 4. 如果是云版本，配置监控 (setupMonitoring)
    // 5. 更新 deployment 状态
};
```

### 5.2 部署服务器初始化步骤

`defaultCommand(isBuildServer = false)` 生成的安装脚本包含以下步骤：

| 步骤 | 内容 | 说明 |
|------|------|------|
| 1 | 安装基础工具 | curl, wget, git, git-lfs, jq, openssl（适配多种 Linux 发行版） |
| 2 | 端口验证 | 检查 80/443 端口是否被占用 |
| 3 | 安装 RClone | 用于备份 |
| 4 | 安装 Docker | 适配 20+ 种 Linux 发行版，支持 Ubuntu/Debian/CentOS/Fedora/Arch/Alpine/Amazon Linux 等 |
| 5 | 初始化 Docker Swarm | 自动检测公网 IP（支持 IPv4/IPv6），执行 `docker swarm init` |
| 6 | 创建 overlay 网络 | `docker network create --driver overlay --attachable dokploy-network` |
| 7 | 创建目录结构 | `/etc/dokploy/` 及其子目录 |
| 8 | 配置 Traefik | 生成 `traefik.yml` 配置文件 |
| 9 | 配置中间件 | 生成 `middlewares.yml`（默认中间件） |
| 10 | 启动 Traefik 容器 | 以 `docker run` 方式运行 Traefik（非 Swarm 服务） |
| 11 | 安装 Nixpacks | 构建工具 |
| 12 | 安装 Buildpacks | 构建工具 |
| 13 | 安装 Railpack | 构建工具 |

### 5.3 构建服务器初始化步骤（精简）

构建服务器仅执行步骤 1、4、7、11、12、13（不需要 Traefik、Swarm、网络等）。

### 5.4 自定义安装命令

服务器的 `command` 字段允许用户覆盖默认安装脚本，在 `installRequirements` 中：
```typescript
const command = server.command || defaultCommand(isBuildServer);
```

## 6. Docker Swarm 集群管理

### 6.1 Swarm 初始化

```bash
# setup/server-setup.ts - setupSwarm()
docker swarm init --advertise-addr $advertise_addr
```

自动检测服务器公网 IP 的优先级：
1. IPv4: ifconfig.io -> icanhazip.com -> ipecho.net
2. IPv6: 同上三个服务的 IPv6 端点

### 6.2 网络创建

```bash
docker network create --driver overlay --attachable dokploy-network
```

所有 Dokploy 管理的服务都加入 `dokploy-network`，确保服务间可互通。

### 6.3 Traefik 远程部署

远程服务器上的 Traefik 以独立容器方式运行：

```bash
docker run -d \
    --name dokploy-traefik \
    --restart always \
    -v /etc/dokploy/traefik/traefik.yml:/etc/traefik/traefik.yml \
    -v /etc/dokploy/traefik/dynamic:/etc/dokploy/traefik/dynamic \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -p 443:443 -p 80:80 -p 443:443/udp \
    traefik:v{TRAEFIK_VERSION}

docker network connect dokploy-network dokploy-traefik
```

## 7. 镜像分发（Registry 集成）

### 7.1 镜像推送流程

```typescript
// utils/cluster/upload.ts
export const uploadImageRemoteCommand = async (application) => {
    // 支持三种 Registry：
    // 1. registry       - Swarm 分发 Registry
    // 2. buildRegistry  - 构建 Registry
    // 3. rollbackRegistry - 回滚 Registry（保存历史镜像）
};
```

### 7.2 Registry 标签构建

```typescript
export const getRegistryTag = (registry, imageName) => {
    const repositoryName = extractRepositoryName(imageName);
    const targetPrefix = imagePrefix || username;
    return `${registryUrl}/${targetPrefix}/${repositoryName}`;
};
```

### 7.3 推送命令序列

```bash
# 1. 登录 Registry
echo "{password}" | docker login {registryUrl} -u '{username}' --password-stdin
# 2. 标记镜像
docker tag {imageName} {registryTag}
# 3. 推送镜像
docker push {registryTag}
```

回滚 Registry 还会创建回滚记录（`createRollback`），将当前部署的镜像保存为回滚快照。

## 8. 监控配置

服务器的 `metricsConfig` 字段包含完整的监控配置：

```typescript
metricsConfig: {
    server: {
        type: "Dokploy" | "Remote",     // 监控类型
        refreshRate: 60,                 // 刷新间隔（秒）
        port: 4500,                      // 监控端口
        token: "",                       // 认证 Token
        urlCallback: "",                 // 通知回调 URL
        retentionDays: 2,                // 数据保留天数
        cronJob: "",                     // 定时任务表达式
        thresholds: { cpu: 0, memory: 0 }, // 报警阈值
    },
    containers: {
        refreshRate: 60,
        services: { include: [], exclude: [] }, // 服务过滤
    },
};
```

## 9. 依赖关系

```
多服务器模块依赖：
├── ssh2 (SSH 连接)
├── dockerode (远程 Docker SDK)
├── drizzle-orm (数据库操作)
├── slugify (名称转换)
├── setup/traefik-setup (Traefik 配置生成)
├── setup/monitoring-setup (监控配置)
├── services/deployment (部署记录)
├── utils/filesystem/directory (目录操作)
└── utils/process/execAsync (命令执行)
```

被依赖：
```
├── utils/databases/*.ts (所有数据库部署通过 getRemoteDocker)
├── utils/docker/utils.ts (远程 Docker 操作)
├── utils/builders/*.ts (构建系统)
├── services/application.ts (应用部署)
├── services/compose.ts (Compose 部署)
└── utils/cluster/upload.ts (镜像分发)
```

## 10. 源文件清单

```
packages/server/src/
├── db/schema/server.ts                          ← 服务器数据模型与 API Schema
├── services/server.ts                           ← 服务器 CRUD 服务层
├── setup/server-setup.ts                        ← 服务器初始化脚本（680行）
├── utils/servers/remote-docker.ts               ← 远程 Docker 客户端
└── utils/cluster/upload.ts                      ← Registry 镜像推送
```

## 11. Go 重写注意事项

- **SSH 连接**: 使用 `golang.org/x/crypto/ssh` 替代 `ssh2` 库，Go 生态中 SSH 支持成熟
- **远程 Docker**: Go 的 `github.com/docker/docker/client` 原生支持 SSH 协议（`ssh://user@host:port`），无需额外适配
- **安装脚本**: `defaultCommand()` 生成的 shell 脚本是纯 bash，语言无关，可直接复用
- **Swarm 初始化命令**: `docker swarm init`、`docker network create` 等均为 CLI 命令，直接复用
- **Traefik 配置**: YAML 配置生成使用 `gopkg.in/yaml.v3`
- **服务器类型枚举**: `serverType` ("deploy" | "build") 和 `serverStatus` ("active" | "inactive") 在 Go 中用 `const + iota` 或字符串常量实现
- **监控配置 JSON**: `metricsConfig` 使用 `jsonb` 存储，Go 中用结构体 + `json` tag + `pgtype.JSONB` 映射
- **多发行版适配**: 安装脚本中的 OS 检测和包管理器适配逻辑是 shell 脚本，无需在 Go 中重新实现
- **Registry 命令链**: `docker login/tag/push` 命令链是语言无关的，可直接复用
