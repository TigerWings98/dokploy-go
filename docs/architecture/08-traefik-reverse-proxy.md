# Traefik 反向代理与域名路由

## 1. 模块概述

Dokploy 使用 Traefik v3 作为反向代理，通过**文件提供者**（File Provider）动态管理路由配置。每个应用有一个独立的 YAML 配置文件，中间件（Basic Auth、重定向、路径重写）存放在共享的 `middlewares.yml` 中。

配置文件路径（生产环境）：
```
/etc/dokploy/traefik/
├── traefik.yml                    ← 主配置（静态配置）
└── dynamic/                       ← 动态配置目录（File Provider 监听）
    ├── acme.json                  ← Let's Encrypt 证书存储（chmod 600）
    ├── middlewares.yml             ← 全局中间件（redirect-to-https, basicAuth, redirectRegex）
    ├── dokploy.yml                ← Dokploy 面板自身的路由
    ├── {appName}.yml              ← 每个应用的路由配置
    └── access.log                 ← 访问日志（JSON 格式）
```

## 2. Traefik 主配置

### 2.1 默认主配置生成

```typescript
// setup/traefik-setup.ts
export const getDefaultTraefikConfig = () => MainTraefikConfig
```

生成的 `traefik.yml`（生产环境）：

```yaml
global:
  sendAnonymousUsage: false
providers:
  swarm:
    exposedByDefault: false
    watch: true
  docker:
    exposedByDefault: false
    watch: true
    network: dokploy-network
  file:
    directory: /etc/dokploy/traefik/dynamic
    watch: true
entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"
    http3:
      advertisedPort: 443
    http:
      tls:
        certResolver: letsencrypt
api:
  insecure: true
certificatesResolvers:
  letsencrypt:
    acme:
      email: test@localhost.com
      storage: /etc/dokploy/traefik/dynamic/acme.json
      httpChallenge:
        entryPoint: web
```

关键特性：
- **双提供者**：Docker/Swarm Provider（标签路由）+ File Provider（YAML 文件路由）
- **双入口点**：`web`（:80）和 `websecure`（:443，支持 HTTP/3）
- **Let's Encrypt**：自动 HTTPS 证书，HTTP Challenge

### 2.2 环境变量配置

| 变量 | 默认值 | 说明 |
|------|-------|------|
| `TRAEFIK_PORT` | 80 | HTTP 端口 |
| `TRAEFIK_SSL_PORT` | 443 | HTTPS 端口 |
| `TRAEFIK_HTTP3_PORT` | 443 | HTTP/3 端口（UDP） |
| `TRAEFIK_VERSION` | 3.6.7 | Traefik 版本 |

## 3. Traefik 容器部署

### 3.1 Standalone 模式（单机）

```typescript
export const initializeStandaloneTraefik = async (options: TraefikOptions) => void
```

使用 `docker.createContainer` 创建容器：
- 镜像：`traefik:v{TRAEFIK_VERSION}`
- 容器名：`dokploy-traefik`
- 网络：`dokploy-network`
- 挂载：`traefik.yml`、`dynamic/` 目录、`/var/run/docker.sock`
- 端口：80/tcp、443/tcp、443/udp
- 重启策略：always

### 3.2 Swarm 模式（集群）

```typescript
export const initializeTraefikService = async (options: TraefikOptions) => void
```

使用 `docker.createService` 创建 Swarm 服务：
- 约束：`node.role==manager`
- 端口模式：`host`（直接绑定宿主机端口）
- 副本数：1

## 4. 动态路由管理

### 4.1 应用配置文件操作（application.ts）

| 函数 | 功能 |
|------|------|
| `createTraefikConfig(appName)` | 创建应用初始配置（开发环境附默认路由） |
| `removeTraefikConfig(appName, serverId?)` | 删除应用配置文件 |
| `loadOrCreateConfig(appName)` | 加载或创建应用配置（本地） |
| `loadOrCreateConfigRemote(serverId, appName)` | 加载远程应用配置 |
| `readConfig(appName)` | 读取配置原始 YAML |
| `readRemoteConfig(serverId, appName)` | 读取远程配置 |
| `writeConfig(appName, traefikConfig)` | 写入原始 YAML 字符串 |
| `writeConfigRemote(serverId, appName, traefikConfig)` | 写入远程配置 |
| `writeTraefikConfig(config, appName)` | 写入 FileConfig 对象 |
| `writeTraefikConfigRemote(config, appName, serverId)` | 写入远程 FileConfig |
| `writeTraefikConfigInPath(path, config, serverId?)` | 写入指定路径 |
| `readConfigInPath(path, serverId?)` | 读取指定路径 |
| `readMonitoringConfig(readAll?)` | 读取访问日志（默认前 500 条，过滤 dokploy 自身请求） |
| `createServiceConfig(appName, domain)` | 创建 LoadBalancer 服务配置 |

**应用配置文件结构**：
```yaml
http:
  routers:
    {appName}-router-{uniqueConfigKey}:
      rule: "Host(`example.com`)"
      service: "{appName}-service-{uniqueConfigKey}"
      entryPoints: ["web"]
      middlewares: ["redirect-to-https", "auth-{appName}", "redirect-{appName}-1"]
    {appName}-router-websecure-{uniqueConfigKey}:
      rule: "Host(`example.com`)"
      service: "{appName}-service-{uniqueConfigKey}"
      entryPoints: ["websecure"]
      tls:
        certResolver: letsencrypt
  services:
    {appName}-service-{uniqueConfigKey}:
      loadBalancer:
        servers:
          - url: "http://{appName}:{port}"
        passHostHeader: true
```

### 4.2 域名管理（domain.ts）

```typescript
export const manageDomain = async (app: ApplicationNested, domain: Domain) => void
export const removeDomain = async (application: ApplicationNested, uniqueKey: number) => void
```

**manageDomain 流程**：
1. 加载应用的 Traefik 配置文件
2. 创建 HTTP 路由（web 入口点）
3. 如果启用 HTTPS，创建 HTTPS 路由（websecure 入口点）
4. 创建 LoadBalancer 服务指向 `http://{appName}:{port}`
5. 创建路径相关中间件（stripPrefix、addPrefix）
6. 写回配置文件

**createRouterConfig** 路由规则生成：
- 基础规则：`` Host(`{punycode_host}`) ``
- 路径前缀：`` && PathPrefix(`{path}`) ``（当 path 不为 `/` 时）
- 中间件链：`redirect-to-https`（HTTP 入口）→ `addprefix-*` → `stripprefix-*` → `redirect-*` → `auth-*`
- TLS 配置：`letsencrypt`、`custom`（自定义 certResolver）、`none`
- IDN 域名自动转换 Punycode

**removeDomain**：
- 删除对应的 router 和 service
- 清理路径中间件
- 如果是最后一个路由，删除整个配置文件

## 5. 中间件管理

### 5.1 全局中间件文件（middleware.ts）

| 函数 | 功能 |
|------|------|
| `loadMiddlewares()` | 加载 `middlewares.yml` |
| `loadRemoteMiddlewares(serverId)` | 加载远程 `middlewares.yml` |
| `writeMiddleware(config)` | 写入 `middlewares.yml` |
| `addMiddleware(config, name)` | 向所有路由添加中间件引用 |
| `deleteMiddleware(config, name)` | 从所有路由删除中间件引用 |
| `deleteAllMiddlewares(application)` | 删除应用的所有中间件（安全 + 重定向） |
| `createPathMiddlewares(app, domain)` | 创建 addPrefix/stripPrefix 中间件 |
| `removePathMiddlewares(app, uniqueConfigKey)` | 删除路径中间件 |

**默认中间件**（`middlewares.yml`）：
```yaml
http:
  middlewares:
    redirect-to-https:
      redirectScheme:
        scheme: https
        permanent: true
```

### 5.2 安全中间件（security.ts）

```typescript
export const createSecurityMiddleware = async (application, data: Security) => void
export const removeSecurityMiddleware = async (application, data: Security) => void
```

使用 Traefik BasicAuth 中间件：
```yaml
http:
  middlewares:
    auth-{appName}:
      basicAuth:
        removeHeader: true
        users:
          - "username:$2b$10$..."  # bcrypt 哈希
```

- 密码使用 `bcrypt` 哈希（saltRounds=10）
- 支持多用户（追加到 users 数组）
- 删除时按 username 过滤
- 当最后一个用户被删除时，移除整个中间件

### 5.3 重定向中间件（redirect.ts）

```typescript
export const createRedirectMiddleware = async (application, data: Redirect) => void
export const updateRedirectMiddleware = async (application, data: Redirect) => void
export const removeRedirectMiddleware = async (application, data: Redirect) => void
```

使用 Traefik redirectRegex 中间件：
```yaml
http:
  middlewares:
    redirect-{appName}-{uniqueConfigKey}:
      redirectRegex:
        regex: "^http://www\\.example\\.com/(.*)"
        replacement: "http://example.com/${1}"
        permanent: true
```

### 5.4 路径中间件

- **stripPrefix**：移除 URL 路径前缀后转发
- **addPrefix**：添加内部路径前缀

```yaml
http:
  middlewares:
    stripprefix-{appName}-{uniqueConfigKey}:
      stripPrefix:
        prefixes:
          - "/api"
    addprefix-{appName}-{uniqueConfigKey}:
      addPrefix:
        prefix: "/v2"
```

## 6. Dokploy 面板路由

### 6.1 初始配置

```typescript
// setup/traefik-setup.ts
export const createDefaultServerTraefikConfig = () => void
```

创建 `dokploy.yml`：
```yaml
http:
  routers:
    dokploy-router-app:
      rule: "Host(`dokploy.docker.localhost`) && PathPrefix(`/`)"
      service: dokploy-service-app
      entryPoints: ["web"]
  services:
    dokploy-service-app:
      loadBalancer:
        servers:
          - url: "http://dokploy:3000"
        passHostHeader: true
```

### 6.2 面板域名更新

```typescript
// traefik/web-server.ts
export const updateServerTraefik = (settings, newHost) => void
export const updateLetsEncryptEmail = (newEmail) => void
export const readMainConfig = () => string | null
export const writeMainConfig = (config) => void
```

`updateServerTraefik` 更新 Dokploy 面板的自定义域名和 HTTPS 配置。

## 7. Compose 服务的域名注入

对于 Docker Compose 服务，域名通过 Docker 标签注入（而非文件配置）：

```typescript
// docker/domain.ts → createDomainLabels()
```

生成的 Traefik 标签（docker-compose 模式放在 `labels`，stack 模式放在 `deploy.labels`）：
```yaml
labels:
  - "traefik.enable=true"
  - "traefik.docker.network=dokploy-network"
  - "traefik.http.routers.{routerName}.rule=Host(`example.com`)"
  - "traefik.http.routers.{routerName}.entrypoints=web"
  - "traefik.http.services.{routerName}.loadbalancer.server.port=3000"
  - "traefik.http.routers.{routerName}.service={routerName}"
  - "traefik.http.routers.{routerName}.middlewares=redirect-to-https@file"
  - "traefik.http.routers.{routerName}.tls.certresolver=letsencrypt"
```

## 8. 类型定义

### 8.1 FileConfig（动态配置类型）

```typescript
// file-types.ts
export interface FileConfig {
    http?: {
        routers?: { [k: string]: HttpRouter };
        services?: { [k: string]: HttpService };
        middlewares?: { [k: string]: HttpMiddleware };
    };
    tcp?: { routers, services };
    udp?: { routers, services };
    tls?: { options, certificates, stores };
}
```

### 8.2 MainTraefikConfig（静态配置类型）

```typescript
// types.ts
export interface MainTraefikConfig {
    accessLog?, api?, certificatesResolvers?, entryPoints?,
    experimental?, global?, log?, metrics?, providers?,
    serversTransport?, tracing?
}
```

## 9. 依赖关系

```
Traefik 工具层依赖：
├── yaml (YAML 解析/生成)
├── bcrypt (密码哈希)
├── dockerode (Traefik 容器/服务管理)
├── utils/process/execAsync (远程文件操作)
├── utils/servers/remote-docker (远程 Docker)
├── utils/docker/utils (encodeBase64)
└── constants (paths)
```

被依赖：
```
├── services/domain.ts (manageDomain 调用)
├── services/security.ts (安全中间件)
├── services/redirect.ts (重定向中间件)
├── services/application.ts (创建/删除时管理路由)
├── services/compose.ts (compose 域名注入)
├── setup/ (初始化)
└── server.ts (启动时创建默认配置)
```

## 10. 源文件清单

```
packages/server/src/utils/traefik/
├── application.ts        ← 应用配置文件 CRUD（核心）
├── domain.ts             ← 域名路由管理（manageDomain, removeDomain）
├── web-server.ts         ← Dokploy 面板路由 + Let's Encrypt 邮箱
├── security.ts           ← BasicAuth 中间件
├── redirect.ts           ← redirectRegex 中间件
├── middleware.ts          ← 中间件文件 CRUD + 路径中间件
├── file-types.ts          ← Traefik 动态配置 TypeScript 类型（大文件）
└── types.ts              ← Traefik 静态配置 TypeScript 类型

packages/server/src/setup/
└── traefik-setup.ts      ← Traefik 容器部署 + 默认配置生成
```

## 11. Go 重写注意事项

- **YAML 配置文件**: 所有 Traefik YAML 配置结构是语言无关的，可直接复用
- **类型定义**: `file-types.ts` 和 `types.ts` 的 Traefik 类型可使用 Go struct 重新定义，或使用 Traefik 官方 Go 类型
- **bcrypt**: 使用 `golang.org/x/crypto/bcrypt` 替代 Node.js bcrypt
- **Punycode**: Go 标准库 `golang.org/x/net/idna` 提供 IDN 支持
- **文件操作**: YAML 读写使用 `gopkg.in/yaml.v3`
- **远程文件操作**: 通过 SSH 执行 `cat`/`echo` 命令读写远程配置文件，命令模式语言无关
- **Traefik 版本**: 默认 `v3.6.7`，通过环境变量可配置
- **Docker 标签注入**: Compose 域名标签生成逻辑是字符串操作，直接移植
