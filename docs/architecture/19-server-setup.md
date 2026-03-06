# 服务器初始化设置

## 1. 模块概述

Server Setup 模块负责 Dokploy 系统启动时的基础设施初始化，以及远程服务器的配置部署。核心职责包括：

1. **目录结构创建** - 确保所有必要的文件系统路径存在（`/etc/dokploy/` 及其子目录）
2. **Docker Swarm/Network 初始化** - 初始化 Swarm 集群和 `dokploy-network` overlay 网络
3. **Traefik 配置生成** - 创建默认的 Traefik 主配置、动态路由配置和中间件配置
4. **远程服务器设置脚本生成** - 生成完整的 bash 脚本，通过 SSH 在远程服务器上安装 Docker、Swarm、Traefik、Nixpacks、Buildpacks 等依赖
5. **服务器验证与安全审计** - 远程检测服务器上各组件的安装状态和安全配置
6. **监控服务部署** - 在远程服务器上部署 `dokploy-monitoring` 容器
7. **内部服务初始化** - PostgreSQL、Redis 的 Swarm 服务创建

在系统架构中的位置：
```
应用启动入口 → Setup 模块 → Docker Engine / SSH → 远程服务器
```

## 2. 目录路径常量

### 2.1 constants/index.ts - 路径定义

```typescript
export const paths = (isServer = false) => {
    const BASE_PATH = isServer || process.env.NODE_ENV === "production"
        ? "/etc/dokploy"
        : path.join(process.cwd(), ".docker");

    return {
        BASE_PATH,                    // /etc/dokploy
        MAIN_TRAEFIK_PATH,           // /etc/dokploy/traefik
        DYNAMIC_TRAEFIK_PATH,        // /etc/dokploy/traefik/dynamic
        LOGS_PATH,                   // /etc/dokploy/logs
        APPLICATIONS_PATH,           // /etc/dokploy/applications
        COMPOSE_PATH,                // /etc/dokploy/compose
        SSH_PATH,                    // /etc/dokploy/ssh
        CERTIFICATES_PATH,           // /etc/dokploy/traefik/dynamic/certificates
        MONITORING_PATH,             // /etc/dokploy/monitoring
        REGISTRY_PATH,               // /etc/dokploy/registry
        SCHEDULES_PATH,              // /etc/dokploy/schedules
        VOLUME_BACKUPS_PATH,         // /etc/dokploy/volume-backups
        VOLUME_BACKUP_LOCK_PATH,     // /etc/dokploy/volume-backup-lock
        PATCH_REPOS_PATH,            // /etc/dokploy/patch-repos
    };
};
```

`isServer` 参数区分本地（主节点）和远程服务器场景；生产环境始终使用 `/etc/dokploy`，开发环境使用 `.docker` 相对目录。

其他关键常量：
- `CLEANUP_CRON_JOB = "50 23 * * *"` - 每日 23:50 执行 Docker 清理
- `docker` - 全局 Dockerode 实例，支持通过环境变量配置 `DOCKER_API_VERSION`、`DOCKER_HOST`、`DOCKER_PORT`

### 2.2 config-paths.ts - 目录创建

```typescript
export const setupDirectories = () => {
    const directories = [
        BASE_PATH, MAIN_TRAEFIK_PATH, DYNAMIC_TRAEFIK_PATH,
        LOGS_PATH, APPLICATIONS_PATH, SSH_PATH, CERTIFICATES_PATH,
        MONITORING_PATH, SCHEDULES_PATH, VOLUME_BACKUPS_PATH,
    ];
    for (const dir of directories) {
        createDirectoryIfNotExist(dir);
        if (dir === SSH_PATH) chmodSync(SSH_PATH, "700");
    }
};
```

SSH 目录设置 `700` 权限以保护密钥安全。

## 3. Swarm 与网络初始化

### 3.1 setup.ts

| 函数 | 功能 |
|------|------|
| `initializeSwarm()` | 检查 Swarm 状态，未初始化时调用 `docker.swarmInit({AdvertiseAddr: "127.0.0.1"})` |
| `dockerSwarmInitialized()` | 通过 `docker.swarmInspect()` 检测 Swarm 是否已初始化 |
| `initializeNetwork()` | 检查并创建 `dokploy-network` overlay 网络（`Attachable: true`） |
| `dockerNetworkInitialized()` | 通过 `docker.getNetwork("dokploy-network").inspect()` 检测网络是否存在 |

## 4. Traefik 配置管理

### 4.1 traefik-setup.ts - 配置生成

环境变量控制端口：
- `TRAEFIK_PORT` - HTTP 端口（默认 80）
- `TRAEFIK_SSL_PORT` - HTTPS 端口（默认 443）
- `TRAEFIK_HTTP3_PORT` - HTTP/3 UDP 端口（默认 443）
- `TRAEFIK_VERSION` - Traefik 版本（默认 3.6.7）

#### 主配置生成

| 函数 | 功能 |
|------|------|
| `getDefaultTraefikConfig()` | 生成主节点 Traefik 配置（开发/生产模式差异化） |
| `getDefaultServerTraefikConfig()` | 生成远程服务器 Traefik 配置（始终包含 Swarm + Docker + File provider） |
| `createDefaultTraefikConfig()` | 将主配置写入 `traefik.yml`，处理文件/目录冲突 |
| `createDefaultServerTraefikConfig()` | 生成动态路由配置 `dokploy.yml`（Dokploy 自身的 Traefik 路由） |
| `getDefaultMiddlewares()` | 生成 `redirect-to-https` 中间件配置 |
| `createDefaultMiddlewares()` | 将中间件写入 `middlewares.yml` |

生产环境主配置包含 Let's Encrypt ACME 证书解析器（HTTP Challenge）。

#### Traefik 实例创建

| 函数 | 模式 | 功能 |
|------|------|------|
| `initializeStandaloneTraefik(options)` | Standalone 容器 | 创建独立 Docker 容器运行 Traefik，支持自定义端口和 dashboard |
| `initializeTraefikService(options)` | Swarm Service | 创建 Swarm 服务运行 Traefik，约束在 manager 节点，使用 host 模式端口发布 |

两种模式都挂载 `traefik.yml`、动态配置目录和 Docker socket，并加入 `dokploy-network` 网络。

## 5. 远程服务器设置

### 5.1 server-setup.ts - SSH 远程部署

核心流程 `serverSetup(serverId, onData)`:

1. 创建日志目录和部署记录
2. 通过 SSH 执行 `defaultCommand()` 生成的安装脚本
3. 云环境下额外配置监控服务（生成 token、设置 callback URL、部署监控容器）
4. 更新部署状态

#### defaultCommand() 生成的脚本流程

**完整服务器（非 Build Server）** 包含 13 步：
1. 安装基础包（curl, wget, git, jq, openssl）
2. 验证端口（80, 443）
3. 安装 RClone
4. 安装 Docker（支持 20+ 种 Linux 发行版）
5. 设置 Docker Swarm（自动检测公网 IP）
6. 创建 dokploy-network
7. 创建目录结构
8. 生成 Traefik 配置
9. 创建默认中间件
10. 创建 Traefik 容器实例（先检查并迁移 Swarm Service → Standalone）
11. 安装 Nixpacks
12. 安装 Buildpacks
13. 安装 Railpack

**Build Server** 仅包含 6 步（跳过端口验证、RClone、Swarm/Network/Traefik）。

#### SSH 连接错误处理

脚本对两类 SSH 错误提供友好的诊断信息：
- `client-authentication` - SSH 密钥不匹配，提示检查 authorized_keys
- 其他连接错误 - 提示检查 IP、端口、防火墙

### 5.2 server-validate.ts - 远程验证

`serverValidate(serverId)` 通过 SSH 执行一组检测脚本，返回 JSON 格式的服务器状态：

| 检测项 | 函数 | 返回值 |
|--------|------|--------|
| Docker | `validateDocker()` | `{version, enabled}` |
| RClone | `validateRClone()` | `{version, enabled}` |
| Nixpacks | `validateNixpacks()` | `{version, enabled}` |
| Buildpacks | `validateBuildpacks()` | `{version, enabled}` |
| Railpack | `validateRailpack()` | `{version, enabled}` |
| Swarm | `validateSwarm()` | `boolean` |
| 主目录 | `validateMainDirectory()` | `boolean` |
| 网络 | `validateDokployNetwork()` | `boolean` |

### 5.3 server-audit.ts - 安全审计

`serverAudit(serverId)` 检测三个安全维度：

| 审计项 | 函数 | 检测内容 |
|--------|------|----------|
| UFW 防火墙 | `validateUfw()` | 安装状态、启用状态、默认入站策略 |
| SSH 配置 | `validateSsh()` | 密钥认证、Root 登录、密码认证、PAM |
| Fail2ban | `validateFail2ban()` | 安装/启用/活跃状态、SSH 保护模式 |

## 6. 监控服务部署

### 6.1 monitoring-setup.ts

| 函数 | 功能 |
|------|------|
| `setupMonitoring(serverId)` | 在远程服务器上部署 `dokploy-monitoring` 容器 |
| `setupWebMonitoring()` | 在主节点上部署监控容器 |

监控容器配置：
- 镜像：`dokploy/monitoring:latest`（或 `canary`）
- 环境变量：`METRICS_CONFIG` JSON 配置
- 挂载：Docker socket（只读）、`/sys`、`/proc`、`/etc/os-release`、SQLite 数据库文件
- 网络模式：`host`（远程服务器）

## 7. 内部服务初始化

### 7.1 postgres-setup.ts

`initializePostgres()` 创建 `dokploy-postgres` Swarm 服务：
- 镜像：`postgres:16`
- 数据卷：`dokploy-postgres` → `/var/lib/postgresql/data`
- 约束：`node.role==manager`
- 开发环境暴露 5432 端口

### 7.2 redis-setup.ts

`initializeRedis()` 创建 `dokploy-redis` Swarm 服务：
- 镜像：`redis:7`
- 数据卷：`dokploy-redis` → `/data`
- 约束：`node.role==manager`
- 开发环境暴露 6379 端口

两者都采用「先尝试更新已有服务，失败则创建新服务」的幂等策略，409 冲突错误静默处理。

## 8. 依赖关系

```
Setup 模块依赖：
├── dockerode (Docker SDK)
├── yaml (YAML 生成)
├── ssh2 (SSH 远程连接)
├── slugify (名称标准化)
├── constants/index.ts (路径常量、Docker 客户端)
├── services/server (服务器查询)
├── services/deployment (部署记录)
├── services/admin (获取 Dokploy URL)
├── services/settings (获取镜像标签)
├── utils/docker/utils (镜像拉取)
├── utils/process/execAsync (命令执行)
├── utils/servers/remote-docker (远程 Docker 客户端)
└── utils/filesystem/directory (目录操作)
```

被依赖：
```
├── 应用启动入口 (系统初始化时调用)
├── tRPC router (server.setup / server.validate / server.audit)
└── Web Server Settings (监控配置更新)
```

## 9. 源文件清单

```
packages/server/src/
├── constants/index.ts                    ← 路径常量、Docker 客户端、清理 cron 表达式
├── setup/
│   ├── config-paths.ts                  ← 目录结构创建
│   ├── setup.ts                         ← Swarm/Network 初始化
│   ├── traefik-setup.ts                 ← Traefik 配置生成与实例创建（434行）
│   ├── server-setup.ts                  ← 远程服务器 SSH 部署脚本生成（723行）
│   ├── server-validate.ts               ← 远程服务器组件验证
│   ├── server-audit.ts                  ← 远程服务器安全审计
│   ├── monitoring-setup.ts              ← 监控容器部署
│   ├── postgres-setup.ts                ← PostgreSQL Swarm 服务初始化
│   └── redis-setup.ts                   ← Redis Swarm 服务初始化
```

## 10. Go 重写注意事项

- **路径常量**: 直接在 Go 中定义为常量或 `func paths(isServer bool) Paths`，无需额外依赖
- **目录创建**: 使用 `os.MkdirAll()` 和 `os.Chmod()` 替代 Node.js 的 `mkdirSync/chmodSync`
- **Docker SDK**: 使用 `github.com/docker/docker/client` 替代 Dockerode，Swarm/Network API 有对应方法
- **SSH 远程执行**: 使用 `golang.org/x/crypto/ssh` 替代 `ssh2`，Go 的 SSH 库更成熟
- **Shell 脚本生成**: `defaultCommand()` 生成的大量 bash 脚本是语言无关的，可以直接作为字符串模板复用，仅需将 JS 模板字面量转为 Go 的 `fmt.Sprintf` 或 `text/template`
- **YAML 生成**: 使用 `gopkg.in/yaml.v3` 替代 `yaml` 库
- **Traefik 配置结构体**: TypeScript 的 `MainTraefikConfig` 和 `FileConfig` 类型可直接映射为 Go struct
- **幂等服务创建**: PostgreSQL/Redis 的「更新或创建」逻辑在 Go Docker SDK 中有等价的 `ServiceUpdate`/`ServiceCreate` 方法
- **安全审计脚本**: `validateUfw()`、`validateSsh()`、`validateFail2ban()` 生成的 shell 命令是语言无关的，可直接复用
- **Token 生成**: `crypto.getRandomValues` 可替换为 `crypto/rand.Read`
