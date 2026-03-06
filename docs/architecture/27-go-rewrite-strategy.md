# Go 重写策略

## 1. 模块概述

本文档提供将 Dokploy 从 Node.js/TypeScript 迁移到 Go 的完整技术策略。涵盖项目结构、技术选型、模块迁移顺序、前端兼容性方案、数据库迁移策略和测试方案。

### 1.1 重写范围

| 组件 | 当前技术栈 | 重写策略 |
|------|-----------|---------|
| `packages/server/` | TypeScript | 完全重写为 Go |
| `apps/dokploy/server/` | tRPC + WebSocket | 完全重写为 Go REST API + WebSocket |
| `apps/api/` | Hono + Inngest | 完全重写为 Go REST API |
| `apps/schedules/` | Hono + BullMQ | 完全重写为 Go 定时任务 |
| `apps/monitoring/` | Go + Fiber | **直接复用**，已是 Go 实现 |
| `apps/dokploy/pages/` 等前端 | Next.js + React | **保持不变**，仅修改 API 调用层 |

### 1.2 设计原则

1. **数据库兼容**: 使用同一个 PostgreSQL 实例，表结构完全兼容
2. **渐进迁移**: 模块可逐个替换，支持并行运行新旧版本
3. **前端最小改动**: 保持相同的 API 语义和数据格式
4. **CLI 复用**: Docker CLI、Nixpacks、RClone 等外部命令直接复用
5. **配置复用**: Traefik YAML、Docker Compose、`/etc/dokploy/` 目录结构不变

## 2. Go 项目结构

### 2.1 推荐目录布局

```
dokploy-go/
├── cmd/
│   ├── server/           # 主服务入口
│   │   └── main.go
│   ├── api/              # 外部 REST API 入口
│   │   └── main.go
│   ├── scheduler/        # 定时任务入口
│   │   └── main.go
│   └── monitoring/       # 监控服务入口（现有代码迁入）
│       └── main.go
├── internal/
│   ├── config/           # 配置加载（环境变量、常量）
│   ├── db/
│   │   ├── schema/       # 数据库模型定义（struct + tag）
│   │   ├── migration/    # 数据库迁移文件
│   │   └── repository/   # 数据访问层（按实体分文件）
│   ├── service/          # 业务逻辑层（43 个服务）
│   ├── handler/          # HTTP 处理器（路由 handler）
│   │   ├── application.go
│   │   ├── compose.go
│   │   └── ...
│   ├── middleware/        # HTTP 中间件（认证、权限、日志）
│   ├── auth/             # 认证系统
│   ├── ws/               # WebSocket 处理器
│   ├── queue/            # 任务队列
│   ├── notify/           # 通知系统（邮件 + 各渠道）
│   ├── docker/           # Docker 操作封装
│   ├── builder/          # 构建器（命令生成）
│   ├── traefik/          # Traefik 配置管理
│   ├── ssh/              # SSH 远程执行
│   ├── process/          # 本地/远程命令执行
│   ├── backup/           # 备份工具
│   ├── provider/         # Git 提供商集成
│   └── setup/            # 初始化逻辑
├── pkg/
│   ├── nanoid/           # nanoid 生成
│   ├── appname/          # appName 生成规则
│   └── validator/        # 输入验证工具
├── api/
│   └── openapi.yaml      # OpenAPI 3.0 规范文件
├── web/                  # Next.js 前端（保持不变，作为子模块或独立仓库）
├── go.mod
├── go.sum
├── Dockerfile
└── Makefile
```

### 2.2 分层架构

```
HTTP 请求
    ↓
middleware（认证 → 权限 → 日志）
    ↓
handler（路由处理器 — 输入验证 + 调用 service）
    ↓
service（业务逻辑 — 编排多个 repository 和外部调用）
    ↓
repository（数据访问 — SQL 查询）    docker/ssh/traefik/process（外部系统）
    ↓                                    ↓
PostgreSQL                          Docker / SSH / 文件系统
```

## 3. 技术栈推荐

### 3.1 Web 框架

| 框架 | 推荐度 | 理由 |
|------|--------|------|
| **Echo** | 推荐 | 轻量、高性能、中间件丰富、WebSocket 支持好、文档完善 |
| Fiber | 可选 | 监控服务已在使用，Express 风格 API，性能极高 |
| Chi | 可选 | 纯标准库风格，轻量极致，但 WebSocket 需额外库 |
| Gin | 可选 | 社区最大，但与 Echo 功能相当 |

**推荐 Echo**，理由：
- 内置 WebSocket 支持
- 中间件链设计与现有 tRPC middleware 概念对应
- 路由分组（Group）对应 tRPC 的 router 注册
- 支持 OpenAPI 代码生成

如果希望与现有监控服务保持技术统一，也可选择 **Fiber**。

### 3.2 数据库

| 组件 | 推荐 | 理由 |
|------|------|------|
| **ORM** | GORM | 生态最大，Preload/Association 对应 Drizzle 的 `with`，迁移工具完善 |
| 备选 ORM | Ent | Facebook 出品，代码生成，强类型，适合复杂 Schema |
| SQL 构建器 | sqlx + squirrel | 更接近原生 SQL，性能更好，但需要更多手动代码 |
| 迁移工具 | golang-migrate | 独立于 ORM，SQL 文件迁移 |

**推荐 GORM**，理由：
- 55 个表（42 个 schema 文件）的关系加载需要 ORM 的 Preload 能力
- JSON/JSONB 字段处理方便（struct tag）
- 社区插件丰富（软删除、分页、审计等）

### 3.3 认证

| 组件 | 推荐 | 理由 |
|------|------|------|
| Session 管理 | `gorilla/sessions` + Redis | 兼容现有 session 表 |
| 密码哈希 | `golang.org/x/crypto/bcrypt` | 与 Better Auth 的 bcrypt 兼容 |
| OAuth | `golang.org/x/oauth2` | 标准库，GitHub/Google OAuth |
| JWT（可选） | `golang-jwt/jwt` | 如果改用 JWT 方案 |
| 2FA/TOTP | `pquerna/otp` | TOTP 生成和验证 |

### 3.4 任务队列

| 组件 | 推荐 | 理由 |
|------|------|------|
| **异步任务** | asynq | Go 生态最流行的 Redis 任务队列，API 类似 BullMQ |
| 备选 | machinery | 功能更全，支持任务链 |
| 定时任务 | robfig/cron | Go 标准 cron 库 |

### 3.5 其他

| 功能 | 推荐库 |
|------|--------|
| SSH | `golang.org/x/crypto/ssh` |
| Docker API | `github.com/docker/docker/client` (官方 SDK) |
| WebSocket | `nhooyr.io/websocket` 或 `gorilla/websocket` |
| 邮件 SMTP | `github.com/wneessen/go-mail` |
| Resend | `github.com/resend/resend-go/v2` |
| HTTP 客户端 | 标准库 `net/http` |
| 日志 | `zerolog` 或 `zap`（对应 Pino） |
| 配置 | `github.com/caarlos0/env` + `godotenv` |
| 验证 | `github.com/go-playground/validator/v10` |
| nanoid | `github.com/matoous/go-nanoid/v2` |
| YAML 处理 | `gopkg.in/yaml.v3` |
| 测试 | `testify` + `testcontainers-go` |

## 4. 模块迁移顺序

### 4.1 推荐顺序（由底向上、由简到繁）

```
Phase 1: 基础设施层
├── Step 1.1: 配置与常量           ← config/, constants
├── Step 1.2: 数据库连接与模型       ← db/schema, db/repository
├── Step 1.3: 进程执行（本地+SSH）  ← process/, ssh/
└── Step 1.4: 认证系统             ← auth/, middleware/

Phase 2: 核心服务层
├── Step 2.1: Docker 操作          ← docker/
├── Step 2.2: Traefik 配置管理     ← traefik/
├── Step 2.3: 构建系统             ← builder/
├── Step 2.4: 应用/Compose 服务    ← service/application, service/compose
└── Step 2.5: 数据库服务           ← service/mysql, postgres, redis, ...

Phase 3: API 层
├── Step 3.1: REST API handler    ← handler/
├── Step 3.2: WebSocket 处理器     ← ws/
├── Step 3.3: 部署队列            ← queue/
└── Step 3.4: 通知系统            ← notify/

Phase 4: 辅助服务
├── Step 4.1: Git 提供商集成       ← provider/
├── Step 4.2: 备份系统            ← backup/
├── Step 4.3: 定时任务服务         ← scheduler/
├── Step 4.4: 外部 REST API       ← api/
└── Step 4.5: 监控服务整合         ← 现有 Go 代码迁入
```

### 4.2 各阶段详细说明

#### Phase 1: 基础设施层

**Step 1.1 配置与常量**

对应源文件：
- `packages/server/src/constants/index.ts` → `internal/config/`
- 环境变量加载、文件路径定义、Docker API 版本等

关键工作：
- 定义 `Config` struct，使用 `env` tag 自动绑定环境变量
- 复用 `/etc/dokploy/` 路径体系
- 复用开发环境 `.docker/` 路径

**Step 1.2 数据库连接与模型**

对应源文件：
- `packages/server/src/db/schema/*.ts`（55 个表（42 个 schema 文件））→ `internal/db/schema/`
- `packages/server/src/db/index.ts` → `internal/db/`

关键工作：
- 定义 42 个 GORM model struct
- PostgreSQL pgEnum 用 Go 自定义 string 类型 + 常量
- JSON/JSONB 字段（Swarm 配置）用嵌套 struct + `gorm:"type:jsonb"`
- nanoid 主键生成：`BeforeCreate` hook
- 定义 Repository 接口和实现

**Step 1.3 进程执行**

对应源文件：
- `packages/server/src/utils/process/` → `internal/process/`

关键工作：
- `ExecAsync` → `os/exec.Command` + `CombinedOutput`
- `ExecAsyncStream` → `os/exec.Command` + `StdoutPipe` + goroutine 读取
- `SpawnAsync` → `os/exec.Command` + 实时输出回调
- SSH 远程执行 → `golang.org/x/crypto/ssh`
- `ExecError` 自定义错误类型

**Step 1.4 认证系统**

对应源文件：
- `packages/server/src/lib/auth.ts` → `internal/auth/`

关键工作：
- Session 管理（兼容现有 session 表）
- 密码 bcrypt 验证（与 Better Auth 10 轮兼容）
- OAuth 流程（GitHub、Google）
- API Key 验证
- 2FA TOTP 验证
- Cookie 配置（自托管 vs 云模式）

#### Phase 2: 核心服务层

**Step 2.1 Docker 操作**

对应源文件：
- `packages/server/src/utils/docker/` → `internal/docker/`
- `packages/server/src/constants/index.ts`（Docker 客户端初始化）

关键工作：
- 使用官方 `github.com/docker/docker/client` 替代 Dockerode
- Docker Service CRUD（创建、更新、删除、Scale）
- 容器日志流
- Docker Network 管理
- 远程 Docker 连接（通过 SSH）

**Step 2.2 Traefik 配置管理**

对应源文件：
- `packages/server/src/utils/traefik/` → `internal/traefik/`

关键工作：
- YAML 配置文件生成（使用 `gopkg.in/yaml.v3`）
- 域名路由配置
- 中间件配置（Basic Auth、重定向）
- SSL 证书管理
- **Traefik YAML 格式完全不变，直接复用**

**Step 2.3 构建系统**

对应源文件：
- `packages/server/src/utils/builders/` → `internal/builder/`

关键工作：
- 6 种构建器的命令生成（本质是字符串拼接）
- Nixpacks/Railpack/Pack/Docker CLI 命令参数组装
- 构建命令通过 `process/` 模块执行
- **外部 CLI 工具命令格式完全不变**

**Step 2.4-2.5 应用/数据库服务**

对应源文件：
- `packages/server/src/services/` → `internal/service/`

关键工作：
- 43 个服务文件逐一迁移
- CRUD 操作通过 GORM Repository
- 部署流程编排（克隆→构建→创建服务→通知）
- 服务启停（Docker Service Scale）

#### Phase 3: API 层

**Step 3.1 REST API handler**

对应源文件：
- `apps/dokploy/server/api/routers/*.ts` → `internal/handler/`

关键工作：
- 43 个 tRPC router → REST API handler
- 路径映射：`application.create` → `POST /api/v1/application`
- 输入验证：Zod → `validator` struct tag
- 权限中间件：protectedProcedure → auth middleware
- 错误格式统一

**Step 3.2 WebSocket 处理器**

对应源文件：
- `apps/dokploy/server/wss/` → `internal/ws/`

关键工作：
- 6 个 WebSocket handler 用 Go WebSocket 库重写
- 部署日志流、容器日志、终端代理
- 认证：WebSocket 握手时验证 session/token

**Step 3.3 部署队列**

对应源文件：
- `apps/dokploy/server/queues/` → `internal/queue/`

关键工作：
- BullMQ → asynq（基于 Redis）
- 部署任务序列化/反序列化
- Worker 并发控制
- 任务取消、超时处理

**Step 3.4 通知系统**

对应源文件：
- `packages/server/src/utils/notifications/` → `internal/notify/`
- `packages/server/src/emails/` → `internal/notify/template/`

关键工作：
- 11 个通知渠道发送函数
- React Email → Go `html/template` 模板
- 通知事件编排逻辑

#### Phase 4: 辅助服务

与前三个阶段类似，按对应源文件逐一迁移。

## 5. 可直接复用的部分

### 5.1 现有 Go 代码

`apps/monitoring/` 的全部代码可以直接迁入 Go 项目：

| 包 | 说明 |
|------|------|
| `monitoring/monitor.go` | 系统指标采集（gopsutil） |
| `database/` | SQLite 指标存储 |
| `containers/` | Docker 容器监控 |
| `middleware/auth.go` | Token 认证 |
| `config/metrics.go` | 配置管理 |

### 5.2 Shell 命令（语言无关）

以下命令在 Go 中通过 `os/exec` 执行，格式完全不变：

| 类别 | 命令示例 |
|------|---------|
| Docker CLI | `docker build`, `docker push`, `docker service create/update/rm`, `docker stack deploy` |
| Nixpacks | `nixpacks build --name xxx .` |
| Railpack | `railpack prepare && docker buildx build` |
| Pack | `pack build --builder heroku/builder:24` |
| RClone | `rclone copy /backup s3:bucket/path` |
| Git | `git clone`, `git checkout`, `git pull` |
| 系统 | `curl`, `unzip`, `rsync` |

### 5.3 配置文件格式

| 类别 | 说明 |
|------|------|
| Traefik YAML | 路由、中间件、证书配置文件格式不变 |
| Docker Compose | `docker-compose.yml` 格式不变 |
| Dockerfile | 应用构建的 Dockerfile 不变 |
| `/etc/dokploy/` | 目录结构不变 |
| `.env` 文件 | 环境变量文件不变 |

## 6. 需要完全重写的部分

### 6.1 tRPC → REST API

| 原始 | Go 替代 | 说明 |
|------|---------|------|
| tRPC Router | Echo/Fiber Route Group | 路由注册 |
| tRPC Procedure | HTTP Handler | 请求处理 |
| tRPC Middleware | Echo/Fiber Middleware | 认证、权限 |
| Zod Schema | validator struct tag | 输入验证 |
| superjson | encoding/json | 序列化（需处理 Date 格式） |
| tRPC subscription | gorilla/websocket | WebSocket 订阅 |
| tRPC batch | 不需要 | Go 单请求性能已足够 |

### 6.2 Drizzle ORM → GORM

| 原始 | Go 替代 | 说明 |
|------|---------|------|
| Drizzle Schema | GORM Model struct | 表定义 |
| drizzle-zod | validator tag | 验证生成 |
| `db.query.xxx.findMany({ with })` | `db.Preload("Relation").Find()` | 关系加载 |
| `db.insert().values()` | `db.Create()` | 插入 |
| `db.update().set().where()` | `db.Model().Where().Updates()` | 更新 |
| `db.delete().where()` | `db.Where().Delete()` | 删除 |
| `db.transaction()` | `db.Transaction()` | 事务 |
| drizzle-kit migrate | golang-migrate | 数据库迁移 |

### 6.3 BullMQ → asynq

| 原始 | Go 替代 | 说明 |
|------|---------|------|
| `new Queue("deployments")` | `asynq.NewClient()` | 队列客户端 |
| `new Worker("deployments", handler)` | `asynq.NewServer().Start(mux)` | 任务处理器 |
| `queue.add("deploy", payload)` | `client.Enqueue(task)` | 任务入队 |
| `job.progress` | 自定义 Redis key | 进度追踪 |
| `queue.clean()` | `asynq.Inspector` | 队列管理 |

### 6.4 WebSocket

| 原始 | Go 替代 | 说明 |
|------|---------|------|
| `ws` (Node.js) | `nhooyr.io/websocket` | WebSocket 库 |
| `node-pty` | `github.com/creack/pty` | 伪终端 |
| tRPC WS transport | 标准 WebSocket | 消息协议 |

### 6.5 React Email → Go 模板

| 原始 | Go 替代 | 说明 |
|------|---------|------|
| React Email (JSX) | `html/template` | 模板引擎 |
| `renderAsync()` | `template.Execute()` | 渲染 |
| Tailwind (内联) | 预编译 CSS 内联 | 样式 |

### 6.6 Better Auth → 自定义认证

| 原始 | Go 替代 | 说明 |
|------|---------|------|
| Better Auth | 自定义实现 | Session + Cookie |
| bcrypt（Better Auth 内置） | `golang.org/x/crypto/bcrypt` | 密码哈希（兼容） |
| OAuth 插件 | `golang.org/x/oauth2` | OAuth 流程 |
| API Key 插件 | 自定义中间件 | API Key 验证 |
| 2FA 插件 | `pquerna/otp` | TOTP |
| SSO 插件 | `github.com/crewjam/saml` | SAML/OIDC |

## 7. 前端兼容性方案

### 7.1 API 迁移策略

**方案一：OpenAPI 规范驱动（推荐）**

1. 定义 `api/openapi.yaml` 规范文件
2. Go 后端：使用 `oapi-codegen` 生成 handler 接口
3. 前端：使用 `openapi-fetch` 或 `orval` 生成类型安全客户端
4. 前端调用从 `api.xxx.useQuery()` 改为 `useQuery(() => client.GET("/api/v1/xxx"))`

优势：
- 前后端共享同一份 API 契约
- 自动生成文档
- 迁移可逐个 API 进行

**方案二：保持路径兼容**

1. Go 后端暴露与 tRPC 相同路径格式的 REST API
2. 例如 `POST /api/trpc/application.create` 返回 tRPC 格式的 JSON
3. 前端完全不改动

优势：前端零改动
劣势：路径格式不自然，长期维护成本高

### 7.2 数据格式兼容

需要处理的 superjson 特殊类型：

| superjson 类型 | Go JSON 处理 |
|---------------|-------------|
| `Date` → `"2024-01-01T00:00:00.000Z"` | `time.Time` + `json:"xxx"` |
| `BigInt` → `"123456789"` | `int64` 或 `string` |
| `undefined` → 省略 | `omitempty` tag |
| `null` → `null` | `*type`（指针） |

### 7.3 WebSocket 兼容

| 现有端点 | Go 替代 | 前端改动 |
|---------|---------|---------|
| `/drawer-logs` (tRPC WS) | 标准 WebSocket | 需要修改 subscription 调用 |
| `/deployment-logs` | 标准 WebSocket | 消息格式保持不变即可 |
| `/container-logs` | 标准 WebSocket | 同上 |
| `/container-terminal` | WebSocket + PTY | 同上 |
| `/terminal` | WebSocket + PTY | 同上 |
| `/docker-stats` | 标准 WebSocket | 同上 |

独立 WebSocket 端点的消息格式保持不变（纯文本日志/终端数据），前端改动极小。
tRPC subscription 需要改为标准 WebSocket 调用，前端改动较大。

## 8. 数据库迁移策略

### 8.1 核心原则

- Go 版本使用与 Node.js 版本**完全相同**的 PostgreSQL 数据库
- 不修改任何现有表结构
- 新的迁移文件与 Drizzle 迁移兼容
- 迁移工具从 drizzle-kit 切换到 golang-migrate

### 8.2 迁移步骤

```
1. 导出当前数据库 Schema（pg_dump --schema-only）
2. 根据导出的 SQL 创建 Go GORM model
3. 使用 golang-migrate 创建基准迁移文件
4. 后续增量迁移使用 golang-migrate SQL 文件
5. 迁移版本号续接 Drizzle 的最后版本
```

### 8.3 Model 定义示例

```go
type Application struct {
    ApplicationID string          `gorm:"primaryKey;type:text" json:"applicationId"`
    Name          string          `gorm:"type:text;not null" json:"name"`
    AppName       string          `gorm:"type:text;uniqueIndex;not null" json:"appName"`
    Description   *string         `gorm:"type:text" json:"description"`
    Env           *string         `gorm:"type:text" json:"env"`
    SourceType    SourceType      `gorm:"type:text;not null;default:github" json:"sourceType"`
    BuildType     BuildType       `gorm:"type:text;not null;default:nixpacks" json:"buildType"`
    Replicas      int             `gorm:"default:1" json:"replicas"`
    EnvironmentID string          `gorm:"type:text;not null" json:"environmentId"`
    ServerID      *string         `gorm:"type:text" json:"serverId"`
    CreatedAt     string          `gorm:"type:text" json:"createdAt"`

    // Relations
    Environment   Environment     `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
    Deployments   []Deployment    `gorm:"foreignKey:ApplicationID" json:"deployments,omitempty"`
    Domains       []Domain        `gorm:"foreignKey:ApplicationID" json:"domains,omitempty"`
    Mounts        []Mount         `gorm:"foreignKey:ApplicationID" json:"mounts,omitempty"`

    // Swarm JSON fields
    HealthCheckSwarm    *HealthCheckSwarm    `gorm:"type:jsonb" json:"healthCheckSwarm"`
    RestartPolicySwarm  *RestartPolicySwarm  `gorm:"type:jsonb" json:"restartPolicySwarm"`
}

func (a *Application) BeforeCreate(tx *gorm.DB) error {
    if a.ApplicationID == "" {
        a.ApplicationID = nanoid.New()
    }
    return nil
}
```

### 8.4 枚举处理

```go
type SourceType string

const (
    SourceTypeDocker    SourceType = "docker"
    SourceTypeGit       SourceType = "git"
    SourceTypeGithub    SourceType = "github"
    SourceTypeGitlab    SourceType = "gitlab"
    SourceTypeBitbucket SourceType = "bitbucket"
    SourceTypeGitea     SourceType = "gitea"
    SourceTypeDrop      SourceType = "drop"
)
```

## 9. 测试策略

### 9.1 测试层次

| 层次 | 工具 | 范围 |
|------|------|------|
| 单元测试 | `testing` + `testify` | service、builder、traefik 配置生成 |
| 集成测试 | `testcontainers-go` | 数据库操作、Docker 操作 |
| API 测试 | `httptest` + `testify` | handler 层端到端 |
| E2E 测试 | Playwright（前端已有） | 全栈集成 |

### 9.2 测试基础设施

```go
// 使用 testcontainers-go 启动测试数据库
func setupTestDB(t *testing.T) *gorm.DB {
    ctx := context.Background()
    pg, _ := postgres.RunContainer(ctx,
        testcontainers.WithImage("postgres:16"),
        postgres.WithDatabase("dokploy_test"),
    )
    t.Cleanup(func() { pg.Terminate(ctx) })
    dsn, _ := pg.ConnectionString(ctx)
    db, _ := gorm.Open(gormPg.Open(dsn))
    db.AutoMigrate(&Application{}, &Deployment{}, ...)
    return db
}
```

### 9.3 Mock 策略

- Docker API：使用 `github.com/docker/docker/client` 的 mock
- SSH：使用内存 SSH 服务器或 mock 接口
- 外部 HTTP（Git API、通知 Webhook）：使用 `httptest.NewServer`

### 9.4 兼容性测试

确保 Go 版本与 Node.js 版本的兼容性：

1. **数据库兼容**: 使用同一个测试数据库，Node.js 写入 → Go 读取 → 验证一致性
2. **命令兼容**: 对比 Go 和 Node.js 生成的 Docker/Nixpacks/Traefik 命令是否完全一致
3. **配置文件兼容**: Go 生成的 Traefik YAML 与 Node.js 生成的做 diff 验证
4. **API 响应兼容**: 对比相同请求在两个版本下的 JSON 响应

## 10. 部署与过渡方案

### 10.1 并行运行

过渡期可以 Go 和 Node.js 版本并行运行：

```
Traefik
├── /api/v2/*  → Go 服务（新 API）
├── /api/trpc/* → Node.js 服务（旧 API）
└── /*         → Next.js 前端
```

前端根据 feature flag 选择调用新旧 API。

### 10.2 Docker 镜像

```dockerfile
# Go 版本 Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

FROM alpine:3.20
# 安装与现有镜像相同的运行时依赖
RUN apk add --no-cache docker-cli curl git openssh-client rsync
COPY --from=builder /server /server
# 安装 Nixpacks、Railpack、Pack 等 CLI 工具（与现有 Dockerfile 相同）
EXPOSE 3000
CMD ["/server"]
```

### 10.3 切换清单

完成迁移后的验证清单：

- [ ] 所有 55 个表（42 个 schema 文件）的 CRUD 操作正常
- [ ] 认证流程（注册、登录、OAuth、2FA、API Key）正常
- [ ] 6 种构建类型都能正确执行
- [ ] Docker Service 创建/更新/删除正常
- [ ] Traefik 配置正确生成
- [ ] 域名路由和 SSL 证书正常
- [ ] 部署队列正常工作
- [ ] 备份和恢复正常
- [ ] 所有通知渠道正常发送
- [ ] WebSocket 日志/终端正常
- [ ] 定时任务正常执行
- [ ] 远程服务器 SSH 操作正常
- [ ] 监控指标采集和告警正常
- [ ] 前端所有页面功能正常

## 11. 源文件参考

| 现有文件/目录 | Go 对应 | 说明 |
|-------------|---------|------|
| `packages/server/src/services/` | `internal/service/` | 43 个业务服务 |
| `packages/server/src/db/schema/` | `internal/db/schema/` | 55 个表（42 个 schema 文件）模型 |
| `packages/server/src/utils/builders/` | `internal/builder/` | 6 个构建器 |
| `packages/server/src/utils/docker/` | `internal/docker/` | Docker 操作 |
| `packages/server/src/utils/traefik/` | `internal/traefik/` | Traefik 配置 |
| `packages/server/src/utils/process/` | `internal/process/` | 命令执行 |
| `packages/server/src/utils/notifications/` | `internal/notify/` | 通知系统 |
| `packages/server/src/utils/providers/` | `internal/provider/` | Git 集成 |
| `packages/server/src/utils/backups/` | `internal/backup/` | 备份工具 |
| `packages/server/src/lib/auth.ts` | `internal/auth/` | 认证系统 |
| `packages/server/src/setup/` | `internal/setup/` | 初始化逻辑 |
| `apps/dokploy/server/api/routers/` | `internal/handler/` | 43 个 API handler |
| `apps/dokploy/server/wss/` | `internal/ws/` | 6 个 WebSocket |
| `apps/dokploy/server/queues/` | `internal/queue/` | 部署队列 |
| `apps/monitoring/` | `cmd/monitoring/` + `internal/` | 已有 Go 代码 |
| `apps/api/` | `cmd/api/` | 外部 REST API |
| `apps/schedules/` | `cmd/scheduler/` | 定时任务 |
