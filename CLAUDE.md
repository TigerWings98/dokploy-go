# CLAUDE.md - Dokploy Go 版工程宪法

## 1. 角色设定 (Role Definition)

你的身份是 **首席 PaaS 平台架构师** 和 **容器化部署专家**。
你的目标是维护 **Dokploy (Go 版)**：一个基于 Docker Swarm、Traefik 反向代理和多 Git 提供商集成的自托管 PaaS 平台，完全兼容原 TypeScript 版本的数据库和前端。

**核心原则：**

- **数据库兼容**。Go 版本与原 TS 版本共享同一 PostgreSQL 实例，表结构完全兼容（camelCase 列名），严禁擅自修改表结构。
- **tRPC 协议兼容**。Go 端实现了 tRPC 兼容层（`handler/trpc.go`），前端零改动直接对接，严禁破坏 tRPC 请求/响应格式。
- **CLI 复用**。Docker CLI、Nixpacks、Railpack、Pack、RClone、Git 等外部命令直接通过 `os/exec` 调用，格式与 TS 版完全一致。
- **Traefik 文件提供者**。路由配置通过 YAML 文件动态管理，不使用 Traefik API，配置文件格式与 TS 版完全兼容。

## 2. 语言协议 (Language Protocol)

- **文档与思考：** 所有的对话、计划、日志、README 必须使用 **中文**。
- **代码规范：** 变量/类/函数名使用 **英文**。
- **注释：** 代码注释必须用 **中文**，详细说明该逻辑在系统中的具体职能。

---

## 3. 技术栈

| 层次       | 选型                                | 说明                           |
| ---------- | ----------------------------------- | ------------------------------ |
| Web 框架   | labstack/echo/v4                    | HTTP 路由 + WebSocket + 中间件 |
| ORM        | gorm.io/gorm + PostgreSQL           | 数据持久化，兼容原 TS 版表结构 |
| 认证       | Better Auth 兼容 (bcrypt + session) | 兼容原 TS 版 session/cookie    |
| Docker     | docker/docker/client v27            | 容器/服务生命周期管理          |
| SSH        | golang.org/x/crypto/ssh             | 远程服务器命令执行             |
| 任务队列   | hibiken/asynq (Redis)               | 异步部署任务队列               |
| WebSocket  | gorilla/websocket                   | 实时日志/终端/监控             |
| 终端模拟   | creack/pty                          | 容器/服务器伪终端              |
| Cron       | robfig/cron/v3                      | 定时任务调度                   |
| nanoid     | matoous/go-nanoid/v2                | 主键生成（21 字符）            |
| YAML       | gopkg.in/yaml.v3                    | Traefik 配置生成               |
| 邮件       | wneessen/go-mail + resend           | SMTP/Resend 双通道             |
| 前端       | Next.js + React (保持不变)          | 通过 tRPC 兼容层对接           |

---

## 4. 分形档案系统 (Fractal Documentation System)

### 4.1 文件夹级：局部脑图 (`README.md`)

每个 `internal/` 子目录必须包含一个极简 `README.md`：

- **职责定义：** 2 行以内，定义本模块在系统中的位置。
- **资产清单：** 表格列出每个文件的 [文件名]、[输入/输出]、[核心逻辑]。
- **自指声明：** "一旦本目录下的逻辑发生变化，必须立即同步更新本 README。"

### 4.2 文件级：三行元数据 (File Header)

每个 `.go` 文件开头必须包含：

```go
// Input: 本文件依赖哪些外部组件
// Output: 本文件对外提供什么
// Role: 本文件在系统中的具体角色
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
```

---

## 5. 架构与工程红线 (Dokploy-Go Specials)

### 数据层 (Data Layer)

- **camelCase 列名**：原 TS 版使用 camelCase 列名（如 `applicationId`、`createdAt`），GORM tag 必须用 `gorm:"column:xxx"` 精确匹配。
- **nanoid 主键**：所有主键使用 `BeforeCreate` hook 生成 nanoid，严禁使用自增 ID。
- **JSON/JSONB 字段**：Swarm 配置等复杂字段使用 `JSONField[T]` 自定义类型，确保序列化兼容。
- **无 AutoMigrate**：生产环境严禁使用 GORM AutoMigrate，表结构由原 TS 版 Drizzle 迁移管理。

### API 层 (API Layer)

- **tRPC 兼容层**：`handler/trpc.go` 实现了 tRPC 协议适配，支持 batch/单请求、query/mutation 语义。
- **superjson 兼容**：JSON 响应必须处理 Date 格式（ISO 8601）、null slice 返回空数组 `[]`。
- **权限层级**：protectedProcedure（需登录）→ adminProcedure（owner/admin）→ enterpriseProcedure（需有效许可证）。

### 部署流水线 (Deploy Pipeline)

- **队列化部署**：所有部署/重部署必须通过 asynq 队列异步执行，严禁同步阻塞。
- **构建器复用**：6 种构建类型（nixpacks/heroku/paketo/dockerfile/static/railpack）的 CLI 命令格式与 TS 版完全一致。
- **Docker Swarm**：使用 Docker Service（非 Container）管理应用生命周期，支持 scale/rolling update。

### 反向代理 (Reverse Proxy)

- **Traefik v3**：通过文件提供者（File Provider）动态管理路由，每个应用一个 YAML 配置文件。
- **证书管理**：优先使用 Traefik 内置 ACME（Let's Encrypt），支持自定义证书上传。
- **中间件**：Basic Auth、重定向、路径重写等配置存放在共享的 `middlewares.yml` 中。

---

## 6. 业务逻辑映射准则

| 平台功能           | 实现方式                             | 核心注意事项                               |
| ------------------ | ------------------------------------ | ------------------------------------------ |
| **应用部署**       | asynq 队列 → 构建 → Docker Service  | 支持 6 种构建类型，构建日志实时 WebSocket   |
| **Compose 部署**   | Docker Compose/Stack deploy          | 支持 Compose 文件 suffix 转换和命名空间隔离 |
| **数据库服务**     | Docker Service (5 种数据库)          | MySQL/PostgreSQL/MariaDB/MongoDB/Redis      |
| **域名路由**       | Traefik YAML 配置                    | 支持自定义域名、HTTPS、路径重写             |
| **备份恢复**       | RClone → S3 兼容存储                 | 支持定时 Cron 备份 + 手动触发               |
| **多服务器**       | SSH 远程执行                         | 远程 Docker/Traefik 管理                    |
| **通知系统**       | 11 种渠道 (Slack/Discord/...)        | 多事件类型 × 多渠道矩阵                    |
| **预览部署**       | GitHub PR Webhook → 临时环境         | PR 关闭自动清理                             |
| **定时任务**       | robfig/cron + asynq                  | 备份/清理/自定义脚本                        |
| **WebSocket 实时** | gorilla/websocket (6 端点)           | 部署日志/容器日志/终端/监控统计             |

---

## 7. 项目结构

```
dokploy-go/
├── cmd/
│   ├── server/main.go        # 主服务入口 (Echo + GORM + asynq + WS)
│   ├── scheduler/main.go     # 定时任务服务
│   └── api/main.go           # 外部部署队列 API
├── internal/
│   ├── config/               # 环境变量配置 + 文件路径常量
│   ├── db/                   # GORM PostgreSQL 连接 + 迁移
│   │   └── schema/           # 全部数据表模型 (21 文件, ~65 struct)
│   ├── auth/                 # Session/API Key 认证
│   ├── middleware/            # Echo 认证中间件
│   ├── handler/              # HTTP 处理器 + tRPC 兼容层 (50+ 文件, 351 procedure)
│   ├── service/              # 业务逻辑层 (部署流水线)
│   ├── docker/               # Docker SDK 封装 (容器/服务/Registry)
│   ├── traefik/              # Traefik YAML 配置生成 + 域名管理
│   ├── builder/              # 构建命令生成 (6 种构建器)
│   ├── process/              # 本地/SSH 远程命令执行
│   ├── git/                  # Git 克隆 (多提供商认证)
│   ├── provider/             # GitHub/GitLab/Gitea/Bitbucket API
│   ├── queue/                # asynq 部署任务队列
│   ├── ws/                   # WebSocket 处理器 (6 端点)
│   ├── notify/               # 多渠道通知发送
│   ├── email/                # HTML 邮件模板 + SMTP/Resend
│   ├── backup/               # S3 备份/恢复 (RClone)
│   ├── compose/              # Compose 文件转换
│   ├── scheduler/            # Cron 定时任务
│   └── setup/                # 服务器初始化 (目录/Swarm/Traefik)
└── docs/architecture/        # 28 篇中文架构文档
```

---

## 8. 强制工作流钩子 (Workflow Hooks)

### [HOOK: PRE-WORK] (环境与资产扫描)

1. **模块定位：** 读取当前目录 `README.md`，确认改动影响的模块边界。
2. **兼容性校验：** 确认改动不会破坏 tRPC 协议兼容性或数据库表结构。

### [HOOK: IMPLEMENTATION] (编码与自修正)

1. **原子化变更：** 实现功能时，同步修改对应文件的 **三行元数据**。
2. **空 slice 保护：** JSON 响应中 slice 字段返回 `[]` 而非 `null`，保持前端兼容。

### [HOOK: POST-WORK] (自蔓延同步)

1. **局部地图更新：** 修改所属目录的 `README.md`。
2. **编译验证：** 执行 `go build ./cmd/server/` 和 `go vet ./...` 确保无编译错误。
3. **提交：** 确认通过后进行 `git commit`。
