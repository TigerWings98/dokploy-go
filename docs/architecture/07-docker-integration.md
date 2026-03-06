# Docker 集成

## 1. 模块概述

Docker 集成是 Dokploy 的核心基础设施层，负责所有容器生命周期管理。系统通过两种方式与 Docker 交互：
1. **Dockerode SDK**（Node.js Docker 客户端）- 用于程序化 API 调用（如拉取镜像、创建服务、监听容器事件）
2. **Docker CLI** - 通过 `execAsync` 执行 shell 命令（如 `docker ps`、`docker service scale`、`docker stack deploy`）

所有 Docker 操作都支持**本地/远程**双模式：本地直接执行，远程通过 SSH 在目标服务器上执行。

在系统架构中的位置：
```
服务层 → Docker 工具层 → Docker Engine (本地) / SSH → Docker Engine (远程)
```

## 2. Docker 客户端初始化

### 2.1 本地 Docker 客户端

```typescript
// packages/server/src/constants/index.ts
import Docker from "dockerode";

export const DOCKER_API_VERSION = undefined; // 使用默认版本
export const docker = new Docker();  // 连接本地 Docker socket
```

默认通过 `/var/run/docker.sock` Unix Socket 连接。

### 2.2 远程 Docker 客户端

```typescript
// packages/server/src/utils/servers/remote-docker.ts
export const getRemoteDocker = async (serverId?: string | null) => {
    if (!serverId) return docker; // 无 serverId 返回本地客户端

    const server = await findServerById(serverId);
    // 通过 SSH 协议连接远程 Docker
    return new Docker({
        protocol: "ssh",
        host: server.ipAddress,
        port: server.port,
        username: server.username,
        sshOptions: { /* SSH key auth */ },
    });
};
```

## 3. 核心 Docker 操作工具

### 3.1 utils/docker/utils.ts - 通用 Docker 工具

| 函数 | 签名 | 功能 |
|------|------|------|
| `pullImage` | `(dockerImage: string, onData?, authConfig?) => Promise<void>` | 本地拉取镜像（支持私有仓库认证） |
| `pullRemoteImage` | `(dockerImage: string, serverId: string, onData?, authConfig?) => Promise<void>` | 远程拉取镜像（通过 Dockerode SSH） |
| `containerExists` | `(containerName: string) => Promise<boolean>` | 检查容器是否存在 |
| `stopService` | `(appName: string) => Promise<void>` | 停止 Swarm 服务（scale=0） |
| `stopServiceRemote` | `(serverId: string, appName: string) => Promise<void>` | 远程停止服务 |
| `startService` | `(appName: string) => Promise<void>` | 启动 Swarm 服务（scale=1） |
| `startServiceRemote` | `(serverId: string, appName: string) => Promise<void>` | 远程启动服务 |
| `removeService` | `(appName: string, serverId?, deleteVolumes?) => Promise<void>` | 删除 Docker 服务 |
| `getContainerByName` | `(name: string) => Promise<ContainerInfo>` | 按名称查找容器 |
| `getServiceContainer` | `(appName: string, serverId?) => Promise<ContainerInfo \| null>` | 获取 Swarm 服务的运行容器 |
| `getComposeContainer` | `(compose, serviceName) => Promise<ContainerInfo \| null>` | 获取 Compose 服务的运行容器 |
| `dockerSafeExec` | `(exec: string) => string` | 生成安全执行脚本（等待 Docker 空闲后执行） |

#### 环境变量处理

| 函数 | 功能 |
|------|------|
| `prepareEnvironmentVariables(serviceEnv, projectEnv?, environmentEnv?)` | 解析环境变量，支持 `${{project.XXX}}`、`${{environment.XXX}}` 和 `${{XXX}}` 三级变量引用 |
| `prepareEnvironmentVariablesForShell(...)` | 同上，但使用 `shell-quote` 库转义特殊字符 |
| `getEnviromentVariablesObject(...)` | 返回 `Record<string, string>` 格式的环境变量 |

#### 挂载点生成

| 函数 | 功能 |
|------|------|
| `generateVolumeMounts(mounts)` | 生成 Docker Volume 类型挂载 |
| `generateBindMounts(mounts)` | 生成 Docker Bind 类型挂载 |
| `generateFileMounts(appName, service)` | 生成文件挂载（从 `APPLICATIONS_PATH/{appName}/files/` 映射） |
| `createFile(outputPath, filePath, content)` | 在本地创建挂载文件 |
| `getCreateFileCommand(outputPath, filePath, content)` | 生成远程创建文件的 shell 命令（base64 编码传输） |

#### 资源限制与 Swarm 配置

```typescript
export const calculateResources = ({
    memoryLimit, memoryReservation, cpuLimit, cpuReservation
}: Resources): ResourceRequirements => ({
    Limits: { MemoryBytes, NanoCPUs },
    Reservations: { MemoryBytes, NanoCPUs },
});

export const generateConfigContainer = (application) => ({
    HealthCheck,        // healthCheckSwarm
    RestartPolicy,      // restartPolicySwarm
    Placement,          // placementSwarm（默认 manager 约束如有挂载）
    Labels,             // labelsSwarm
    Mode,               // modeSwarm 或 Replicated{Replicas}
    RollbackConfig,     // rollbackConfigSwarm
    UpdateConfig,       // updateConfigSwarm（默认 Parallelism:1, Order:start-first）
    StopGracePeriod,    // stopGracePeriodSwarm
    Networks,           // networkSwarm（默认 dokploy-network）
    EndpointSpec,       // endpointSpecSwarm
    Ulimits,            // ulimitsSwarm
});
```

#### 清理操作

| 函数 | 功能 |
|------|------|
| `cleanupContainers(serverId?)` | `docker container prune --force` |
| `cleanupImages(serverId?)` | `docker image prune --all --force` |
| `cleanupVolumes(serverId?)` | `docker volume prune --all --force` |
| `cleanupBuilders(serverId?)` | `docker builder prune --all --force` |
| `cleanupSystem(serverId?)` | `docker system prune --all --force` |
| `cleanupAll(serverId?)` | 顺序执行所有清理（**排除 volumes**，防止误删） |
| `cleanupAllBackground(serverId?)` | 并行异步执行清理 |

所有清理操作使用 `dockerSafeExec` 包装，等待 Docker 空闲后再执行。

### 3.2 services/docker.ts - Docker 查询服务

| 函数 | 签名 | 功能 |
|------|------|------|
| `getContainers` | `(serverId?) => Promise<Container[]>` | 列出所有容器（排除 dokploy 自身） |
| `getConfig` | `(containerId, serverId?) => Promise<object>` | 获取容器详细配置（`docker inspect`） |
| `getContainersByAppNameMatch` | `(appName, appType?, serverId?) => Promise<Container[]>` | 按应用名匹配容器（compose 用 label 过滤，其他用 grep） |
| `getStackContainersByAppName` | `(appName, serverId?) => Promise<Container[]>` | 获取 Swarm Stack 的所有任务 |
| `getServiceContainersByAppName` | `(appName, serverId?) => Promise<Container[]>` | 获取 Swarm 服务的所有任务 |
| `getContainersByAppLabel` | `(appName, type, serverId?) => Promise<Container[]>` | 按标签过滤容器（standalone/swarm/compose） |
| `containerRestart` | `(containerId) => Promise<void>` | 重启容器 |
| `getSwarmNodes` | `(serverId?) => Promise<Node[]>` | 列出 Swarm 节点 |
| `getNodeInfo` | `(nodeId, serverId?) => Promise<object>` | 获取节点详情 |
| `getNodeApplications` | `(serverId?) => Promise<Service[]>` | 列出所有服务（排除 dokploy-） |
| `getApplicationInfo` | `(appNames[], serverId?) => Promise<Task[]>` | 获取多个服务的任务信息 |

## 4. Docker Compose 操作

### 4.1 utils/docker/domain.ts - Compose 域名注入

核心功能是将 Traefik 路由标签注入到 Docker Compose 文件中。

| 函数 | 功能 |
|------|------|
| `cloneCompose(compose)` | 根据 sourceType（github/gitlab/bitbucket/git/gitea/raw）克隆代码生成命令 |
| `getComposePath(compose)` | 获取 compose 文件路径：`COMPOSE_PATH/{appName}/code/{composePath}` |
| `loadDockerCompose(compose)` | 本地读取并解析 compose YAML |
| `loadDockerComposeRemote(compose)` | 远程读取并解析 compose YAML |
| `readComposeFile(compose)` | 读取原始 compose 文件内容 |
| `writeDomainsToCompose(compose, domains)` | 注入域名标签后写回 compose 文件（生成 shell 命令） |
| `addDomainToCompose(compose, domains)` | 核心逻辑：解析 compose → 随机化 → 注入 Traefik 标签 → 添加 dokploy-network |
| `writeComposeFile(compose, composeSpec)` | 将 ComposeSpecification 写回 YAML 文件 |
| `createDomainLabels(appName, domain, entrypoint)` | 生成 Traefik 路由标签 |
| `addDokployNetworkToService(networks)` | 向服务添加 dokploy-network 和 default 网络 |
| `addDokployNetworkToRoot(networks)` | 向 compose 根部添加 dokploy-network（external: true） |

#### Traefik 标签生成逻辑

`createDomainLabels` 为每个域名生成以下 Traefik 标签：
```yaml
traefik.http.routers.{routerName}.rule=Host(`{host}`) && PathPrefix(`{path}`)
traefik.http.routers.{routerName}.entrypoints={web|websecure}
traefik.http.services.{routerName}.loadbalancer.server.port={port}
traefik.http.routers.{routerName}.service={routerName}
# 可选中间件
traefik.http.routers.{routerName}.middlewares=redirect-to-https@file,stripprefix-...,addprefix-...
# HTTPS TLS
traefik.http.routers.{routerName}.tls.certresolver=letsencrypt|{customCertResolver}
```

### 4.2 utils/docker/compose.ts - Compose 随机化

用于防止多个 Compose 部署间的命名冲突。

| 函数 | 功能 |
|------|------|
| `generateRandomHash()` | 生成 8 位随机十六进制后缀 |
| `randomizeComposeFile(composeId, suffix?)` | 读取 compose 文件并为所有属性添加后缀 |
| `randomizeSpecificationFile(composeSpec, suffix?)` | 对已解析的 spec 添加后缀 |
| `addSuffixToAllProperties(composeData, suffix)` | 依次调用 5 个子模块添加后缀 |

### 4.3 Compose 子模块（compose/ 目录）

| 文件 | 函数 | 功能 |
|------|------|------|
| `compose/service.ts` | `addSuffixToAllServiceNames` | 为所有服务名添加后缀，更新 depends_on、links 引用 |
| `compose/volume.ts` | `addSuffixToAllVolumes` | 为卷名添加后缀，更新服务中的卷引用 |
| `compose/network.ts` | `addSuffixToAllNetworks` | 为网络名添加后缀，更新服务网络引用 |
| `compose/configs.ts` | `addSuffixToAllConfigs` | 为 configs 添加后缀 |
| `compose/secrets.ts` | `addSuffixToAllSecrets` | 为 secrets 添加后缀 |

### 4.4 utils/docker/collision.ts - 隔离部署

用于 isolatedDeployment 模式（隔离部署），为 Compose 服务添加应用名前缀防止网络冲突。

| 函数 | 功能 |
|------|------|
| `addAppNameToPreventCollision(composeData, appName, isolatedDeploymentsVolume)` | 添加应用名到服务网络，可选是否隔离卷 |
| `randomizeIsolatedDeploymentComposeFile(composeId, suffix?)` | 克隆代码 → 加载 compose → 应用隔离 |
| `randomizeDeployableSpecificationFile(composeSpec, isolatedDeploymentsVolume, suffix?)` | 对已解析 spec 应用隔离 |

### 4.5 collision/root-network.ts

| 函数 | 功能 |
|------|------|
| `addAppNameToRootNetwork(composeData, appName)` | 在根 networks 中添加以 appName 命名的外部网络 |
| `addAppNameToServiceNetworks(services, appName)` | 为每个服务的 networks 添加 appName 网络 |
| `addAppNameToAllServiceNames(composeData, appName)` | 组合上述两个操作 |

## 5. Docker Compose 类型系统

`types.ts` 定义了完整的 Docker Compose Specification TypeScript 类型，基于 Compose Spec JSON Schema 生成：

- `ComposeSpecification` - 根类型（version, name, services, networks, volumes, secrets, configs）
- `DefinitionsService` - 服务定义（80+ 属性，包括 build, deploy, environment, volumes, networks 等）
- `DefinitionsDeployment` - 部署配置（mode, replicas, resources, restart_policy, placement, update_config）
- `DefinitionsNetwork` / `DefinitionsVolume` / `DefinitionsSecret` / `DefinitionsConfig` - 子资源定义
- `DefinitionsHealthcheck` - 健康检查定义

## 6. Docker Registry 集成

### 6.1 utils/cluster/upload.ts - 镜像推送

| 函数 | 功能 |
|------|------|
| `uploadImageRemoteCommand(application)` | 生成镜像推送命令序列 |
| `getRegistryTag(registry, imageName)` | 构建 registry 标签：`{registryUrl}/{prefix}/{repoName}` |

支持三种 Registry：
- **registry** - Swarm Registry（分发到集群节点）
- **buildRegistry** - 构建 Registry
- **rollbackRegistry** - 回滚 Registry（保存历史镜像）

推送流程：
```bash
docker login {registryUrl} -u '{username}' --password-stdin
docker tag {imageName} {registryTag}
docker push {registryTag}
```

## 7. Docker 安全执行

`dockerSafeExec` 生成一个 shell 脚本，在执行实际命令前检查 Docker 是否空闲：

```bash
CHECK_INTERVAL=10
while true; do
    PROCESSES=$(ps aux | grep -E "^.*docker [A-Za-z]" | grep -v grep)
    if [ -z "$PROCESSES" ]; then
        echo "Docker is idle. Starting execution..."
        break
    else
        echo "Docker is busy. Will check again in $CHECK_INTERVAL seconds..."
        sleep $CHECK_INTERVAL
    fi
done
{actual_command}
```

这防止了并发 Docker 操作导致的资源竞争。

## 8. 网络管理

所有 Dokploy 管理的服务都加入 `dokploy-network` 网络：

```typescript
// 初始化时创建
docker network create --driver overlay --attachable dokploy-network

// 服务创建时默认网络
Networks: [{ Target: "dokploy-network" }]

// Compose 部署时注入
networks:
  dokploy-network:
    external: true
```

## 9. 依赖关系

```
Docker 工具层依赖：
├── dockerode (Docker SDK)
├── yaml (YAML 解析/生成)
├── dotenv (环境变量解析)
├── shell-quote (Shell 转义)
├── lodash (对象操作)
├── utils/process/ (命令执行)
├── utils/servers/remote-docker (远程 Docker)
├── utils/providers/ (Git 提供商，用于 compose clone)
└── services/ (compose, deployment, registry, rollbacks)
```

被依赖：
```
├── services/application.ts (部署应用)
├── services/compose.ts (部署 compose)
├── services/mysql|postgres|redis|mariadb|mongo.ts (数据库部署)
├── utils/builders/ (构建系统)
├── wss/ (WebSocket 容器日志/终端)
└── setup/ (初始化)
```

## 10. 源文件清单

```
packages/server/src/
├── constants/index.ts                      ← Docker 客户端初始化
├── services/docker.ts                      ← Docker 查询服务
├── utils/docker/
│   ├── types.ts                           ← ComposeSpecification 类型定义（880行）
│   ├── utils.ts                           ← 核心 Docker 工具函数（744行）
│   ├── domain.ts                          ← Compose 域名注入
│   ├── compose.ts                         ← Compose 随机化
│   ├── collision.ts                       ← 隔离部署
│   ├── collision/root-network.ts          ← 网络隔离
│   └── compose/
│       ├── service.ts                     ← 服务名后缀
│       ├── volume.ts                      ← 卷名后缀
│       ├── network.ts                     ← 网络名后缀
│       ├── configs.ts                     ← 配置名后缀
│       └── secrets.ts                     ← 密钥名后缀
├── utils/cluster/upload.ts                ← Registry 镜像推送
└── utils/servers/remote-docker.ts         ← 远程 Docker 连接
```

## 11. Go 重写注意事项

- **Docker SDK**: 使用 `github.com/docker/docker/client` 替代 Dockerode
- **Docker CLI 命令**: 所有 `docker ps`、`docker service`、`docker stack` 等命令是语言无关的，可直接复用
- **YAML 处理**: 使用 `gopkg.in/yaml.v3` 处理 Compose 文件
- **Compose 类型**: `types.ts` 中的 ComposeSpecification 可使用 `github.com/compose-spec/compose-go` 官方 Go 库
- **环境变量模板**: `${{project.XXX}}` 变量替换逻辑需在 Go 中实现
- **Shell 脚本生成**: `dockerSafeExec`、`getCreateFileCommand` 等生成的 shell 脚本是语言无关的，可直接复用
- **Registry 推送命令**: `getRegistryCommands` 生成的 docker login/tag/push 命令链是语言无关的
- **Remote Docker**: Go 的 Docker SDK 原生支持 SSH 协议连接远程 Docker
- **网络操作**: `dokploy-network` 的创建和管理逻辑是 Docker CLI 命令，可直接复用
