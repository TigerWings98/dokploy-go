# 项目总览与架构概述

## 1. 项目简介

Dokploy 是一个免费、自托管的 PaaS（Platform as a Service）平台，提供应用部署、数据库管理、域名路由、SSL 证书、多服务器管理等能力。类似于 Heroku/Vercel 的自托管替代方案。

**版本**: v0.28.3
**许可证**: Apache-2.0（含部分企业级付费功能）
**Node.js**: ^24.4.0
**pnpm**: >=9.12.0

## 2. 整体架构

### 2.1 Monorepo 结构

项目使用 pnpm workspaces 管理，包含 4 个运行进程和 1 个共享库：

```
dokploy/
├── apps/
│   ├── dokploy/       # 主应用：Next.js 前端 + tRPC 后端 + WebSocket（端口 3000）
│   ├── api/           # 外部 REST API：Hono + Inngest 事件驱动（端口 4000）
│   ├── schedules/     # 定时任务服务：Hono + BullMQ（端口 4001）
│   └── monitoring/    # 监控服务：Go + Fiber + SQLite（端口 3001）
├── packages/
│   └── server/        # 共享后端库：数据库、服务层、工具函数
├── Dockerfile         # 主应用 Docker 镜像
├── Dockerfile.schedule # 定时任务 Docker 镜像
├── Dockerfile.monitoring # 监控服务 Docker 镜像
├── Dockerfile.server  # 服务器安装镜像
├── Dockerfile.cloud   # 云版本镜像
└── pnpm-workspace.yaml
```

### 2.2 进程架构图

```
                    ┌─────────────────────────────────────────┐
                    │            Traefik 反向代理               │
                    │     HTTP:80  HTTPS:443  Dashboard:8080   │
                    └──────┬──────────────┬───────────────────┘
                           │              │
              ┌────────────▼──┐    ┌──────▼──────────┐
              │  dokploy:3000  │    │  monitoring:3001 │
              │  (Next.js +    │    │  (Go + Fiber)    │
              │   tRPC + WSS)  │    │  系统/容器指标     │
              └───┬────────┬──┘    └─────────────────┘
                  │        │
         ┌────────▼──┐  ┌──▼─────────┐
         │ api:4000   │  │schedules:  │
         │ (Hono +    │  │  4001      │
         │  Inngest)  │  │(BullMQ)    │
         └────────┬──┘  └──┬─────────┘
                  │        │
              ┌───▼────────▼───┐
              │  PostgreSQL     │  ← 主数据库
              │  dokploy-       │
              │  postgres:5432  │
              └────────────────┘
              ┌────────────────┐
              │  Redis          │  ← 任务队列
              └────────────────┘
```

### 2.3 进程间通信

| 通信路径 | 方式 | 说明 |
|---------|------|------|
| 前端 → 主后端 | tRPC over HTTP | 类型安全 RPC 调用 |
| 前端 → 主后端 | WebSocket | 实时日志、终端、监控 |
| 主后端 → API 服务 | HTTP + API Key | 部署任务分发 |
| 主后端 → 调度服务 | HTTP | 定时任务管理 |
| 主后端 → 监控服务 | HTTP + Token | 指标查询 |
| 主后端 → 远程服务器 | SSH | 远程命令执行、Docker 操作 |
| 主后端 → Docker | Unix Socket / TCP | 容器管理（Dockerode） |
| 调度服务 → Redis | TCP | BullMQ 任务队列 |

## 3. 技术栈

### 3.1 前端
- **框架**: Next.js 16 + React 18
- **样式**: TailwindCSS + Radix UI (shadcn/ui)
- **状态**: TanStack React Query + tRPC Client
- **表单**: React Hook Form + Zod 验证
- **图表**: Recharts
- **终端**: Xterm.js
- **数据表**: TanStack Table

### 3.2 后端（Node.js）
- **RPC 框架**: tRPC v11（类型安全远程过程调用）
- **HTTP 框架**: Hono v4（API 和调度服务）
- **ORM**: Drizzle ORM + postgres.js 驱动
- **认证**: Better Auth（支持 SSO、2FA、API Key）
- **Docker**: Dockerode（Docker API 客户端）
- **SSH**: ssh2（远程服务器管理）
- **终端**: node-pty（伪终端）
- **队列**: BullMQ + Redis（任务队列）
- **事件**: Inngest（事件驱动任务处理）
- **验证**: Zod（Schema 验证）
- **日志**: Pino
- **邮件**: Nodemailer + Resend + React Email

### 3.3 后端（Go）
- **框架**: Fiber v2
- **数据库**: SQLite（指标存储）
- **环境**: godotenv

### 3.4 基础设施
- **容器**: Docker + Docker Compose + Docker Swarm
- **反向代理**: Traefik v3.6.7
- **数据库**: PostgreSQL（主存储）、Redis（队列）、SQLite（监控指标）
- **备份**: RClone（S3 兼容存储）
- **构建工具**: Nixpacks v1.41.0、Heroku/Paketo Buildpacks、Railpack v0.15.4

### 3.5 集成
- **Git**: GitHub（App + Webhook）、GitLab、Gitea、Bitbucket
- **通知**: Slack、Discord、Telegram、Email
- **支付**: Stripe（企业版）
- **AI**: Vercel AI SDK（OpenAI、Anthropic、Azure、Mistral、Cohere）
- **追踪**: HubSpot（可选）

## 4. 目录结构详解

### 4.1 apps/dokploy/ — 主应用

```
apps/dokploy/
├── pages/                    # Next.js 页面和 API 路由
│   ├── api/                  # 后端 API 端点
│   │   ├── auth/[...all].ts  # Better Auth 认证路由
│   │   ├── deploy/           # 部署 Webhook 触发
│   │   ├── providers/        # Git OAuth 回调
│   │   ├── stripe/           # 支付 Webhook
│   │   └── trpc/[trpc].ts    # tRPC 入口
│   └── dashboard/            # 管理面板页面
├── components/               # React 组件
│   ├── dashboard/            # 面板组件
│   ├── ui/                   # shadcn/ui 基础组件
│   └── layouts/              # 页面布局
├── server/                   # 后端逻辑
│   ├── server.ts             # HTTP 服务器入口（启动序列）
│   ├── api/
│   │   ├── root.ts           # tRPC 路由注册（appRouter）
│   │   ├── trpc.ts           # tRPC 上下文和中间件
│   │   └── routers/          # 43 个 tRPC 路由文件（含 proprietary/）
│   ├── wss/                  # 6 个 WebSocket 处理器
│   └── queues/               # BullMQ 部署队列
├── hooks/                    # React Hooks
├── lib/                      # 前端工具库
├── utils/                    # 前端工具函数
├── drizzle/                  # 数据库迁移文件（自动生成）
└── docker/                   # Docker 构建脚本
```

### 4.2 apps/api/ — 外部 REST API

```
apps/api/src/
├── index.ts      # Hono 服务器 + Inngest 集成
├── schema.ts     # Zod 验证 Schema
├── service.ts    # 业务逻辑
├── utils.ts      # 工具函数
└── logger.ts     # 日志配置
```

### 4.3 apps/schedules/ — 定时任务服务

```
apps/schedules/src/
├── index.ts      # Hono 服务器 + 队列初始化
├── queue.ts      # BullMQ 队列配置
├── workers.ts    # 任务处理 Worker
├── schema.ts     # Zod 验证
├── utils.ts      # 工具函数
└── logger.ts     # 日志配置
```

### 4.4 apps/monitoring/ — 监控服务（Go）

```
apps/monitoring/
├── main.go           # 入口
├── config/           # 配置管理
├── database/         # SQLite 指标存储
├── containers/       # Docker 容器监控
├── middleware/       # Token 认证中间件
└── monitoring/       # 指标采集逻辑
```

### 4.5 packages/server/ — 共享后端库

```
packages/server/src/
├── index.ts              # 导出清单（134 个导出）
├── auth/                 # 密码生成
├── constants/            # 常量、Docker 客户端、文件路径
├── db/
│   ├── schema/           # 42 个 Drizzle ORM 表定义
│   ├── validations/      # Zod API 验证 Schema
│   ├── index.ts          # 数据库连接
│   └── constants.ts      # 数据库 URL 配置
├── lib/
│   ├── auth.ts           # Better Auth 完整配置
│   └── logger.ts         # Pino 日志
├── services/             # 43 个业务服务文件
├── setup/                # 9 个初始化文件
├── utils/
│   ├── builders/         # 10 个构建器
│   ├── docker/           # Docker 操作
│   ├── traefik/          # 8 个 Traefik 配置
│   ├── providers/        # 7 个 Git 提供商
│   ├── servers/          # 远程 Docker 连接
│   ├── cluster/          # 集群镜像上传
│   ├── process/          # 命令执行
│   ├── filesystem/       # 文件系统操作
│   ├── backups/          # 8 个备份工具
│   ├── notifications/    # 8 个通知工具
│   ├── databases/        # 数据库重建
│   ├── schedules/        # 定时任务工具
│   ├── crons/            # Cron 任务
│   ├── volume-backups/   # 卷备份
│   ├── access-log/       # 访问日志
│   └── watch-paths/      # 文件监听
├── emails/               # 邮件模板
├── verification/         # 邮件验证
├── wss/                  # WebSocket 工具
├── monitoring/           # 监控工具
└── templates/            # 模板处理
```

## 5. 文件系统路径

生产环境下，Dokploy 使用 `/etc/dokploy/` 作为基础目录：

```
/etc/dokploy/
├── traefik/
│   └── dynamic/
│       └── certificates/     # SSL 证书存储
├── applications/             # 应用源代码和构建产物
├── compose/                  # Docker Compose 文件
├── ssh/                      # SSH 密钥
├── logs/                     # 部署日志
├── monitoring/               # 监控数据
├── registry/                 # 镜像仓库配置
├── schedules/                # 定时任务数据
├── volume-backups/           # 卷备份数据
├── volume-backup-lock/       # 卷备份锁文件
└── patch-repos/              # 补丁仓库
```

开发环境使用 `<project_root>/.docker/` 作为替代路径。

## 6. 环境变量清单

### 6.1 核心配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `PORT` | `3000` | 主应用端口 |
| `HOST` | `0.0.0.0` | 绑定地址 |
| `NODE_ENV` | - | 运行环境（production/development） |
| `IS_CLOUD` | `false` | 是否云版本模式 |

### 6.2 数据库

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `DATABASE_URL` | - | 完整数据库连接串（优先级最高） |
| `POSTGRES_PASSWORD_FILE` | - | Docker Secret 密码文件路径 |
| `POSTGRES_USER` | `dokploy` | PostgreSQL 用户名 |
| `POSTGRES_DB` | `dokploy` | PostgreSQL 数据库名 |
| `POSTGRES_HOST` | `dokploy-postgres` | PostgreSQL 主机 |
| `POSTGRES_PORT` | `5432` | PostgreSQL 端口 |

### 6.3 Docker

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `DOCKER_API_VERSION` | - | Docker API 版本 |
| `DOCKER_HOST` | - | Docker 守护进程地址 |
| `DOCKER_PORT` | - | Docker 守护进程端口 |

### 6.4 Traefik

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `TRAEFIK_PORT` | `80` | HTTP 入口端口 |
| `TRAEFIK_SSL_PORT` | `443` | HTTPS 入口端口 |
| `TRAEFIK_HTTP3_PORT` | `443` | HTTP/3 入口端口（UDP） |
| `TRAEFIK_VERSION` | `3.6.7` | Traefik 镜像版本 |

### 6.5 认证

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `BETTER_AUTH_SECRET` | `better-auth-secret-123456789` | 认证加密密钥 |
| `GITHUB_CLIENT_ID` | - | GitHub OAuth 客户端 ID |
| `GITHUB_CLIENT_SECRET` | - | GitHub OAuth 客户端密钥 |
| `GOOGLE_CLIENT_ID` | - | Google OAuth 客户端 ID |
| `GOOGLE_CLIENT_SECRET` | - | Google OAuth 客户端密钥 |

### 6.6 其他

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `RELEASE_TAG` | `latest` | Docker 镜像标签 |
| `HUBSPOT_PORTAL_ID` | - | HubSpot 追踪（可选） |
| `HUBSPOT_FORM_GUID` | - | HubSpot 表单（可选） |
| `USER_ADMIN_ID` | - | 管理员用户 ID（云版本） |

## 7. Docker 容器化

### 7.1 主应用镜像（Dockerfile）

- **基础镜像**: node:24.4.0-slim
- **构建依赖**: python3, make, g++, git, pkg-config, libsecret-1-dev
- **运行时依赖**: curl, unzip, zip, apache2-utils, iproute2, rsync, git-lfs
- **内置工具**:
  - Docker CLI v28.5.2
  - RClone（备份到 S3）
  - Nixpacks v1.41.0
  - Railpack v0.15.4
  - Pack（Buildpacks CLI）v0.39.1
  - tsx（TypeScript 执行）
- **端口**: 3000
- **健康检查**: `curl -fs http://localhost:3000/api/trpc/settings.health`
- **启动命令**: `pnpm run wait-for-postgres && exec pnpm start`

### 7.2 定时任务镜像（Dockerfile.schedule）

- **基础镜像**: node:24.4.0-slim
- **构建**: pnpm + esbuild
- **启动命令**: `pnpm start`
- **无额外运行时依赖**

### 7.3 监控镜像（Dockerfile.monitoring）

- **构建阶段**: golang:1.21-alpine3.19 + gcc + musl-dev + sqlite-dev
- **运行时**: alpine:3.19 + sqlite-libs + docker-cli
- **端口**: 3001
- **启动命令**: `./main`

## 8. 启动序列

主应用 `server.ts` 的启动流程：

```
1. 加载 .env 配置
2. [生产+自托管] 同步初始化:
   ├── setupDirectories()        → 创建 /etc/dokploy/ 目录结构
   ├── createDefaultTraefikConfig() → 生成 Traefik 静态配置
   └── createDefaultServerTraefikConfig() → 生成服务器 Traefik 配置
3. Next.js 应用准备 (app.prepare())
4. 创建 HTTP 服务器
5. 挂载 6 个 WebSocket 处理器:
   ├── setupDrawerLogsWebSocketServer      → 抽屉日志
   ├── setupDeploymentLogsWebSocketServer  → 部署日志流
   ├── setupDockerContainerLogsWebSocketServer → 容器日志
   ├── setupDockerContainerTerminalWebSocketServer → 容器终端
   ├── setupTerminalWebSocketServer        → 服务器终端
   └── setupDockerStatsMonitoringSocketServer → Docker 统计（仅自托管）
6. [生产+自托管] 异步初始化:
   ├── createDefaultMiddlewares()  → 创建 Traefik 默认中间件
   ├── initializeNetwork()         → 确保 dokploy-network 存在
   ├── initCronJobs()              → 启动定时清理任务
   ├── initSchedules()             → 从数据库加载定时备份
   ├── initCancelDeployments()     → 取消超时部署
   ├── initVolumeBackupsCronJobs() → 启动卷备份定时
   └── sendDokployRestartNotifications() → 发送重启通知
7. 监听端口 3000
8. 启动企业版备份 Cron
9. [自托管] 启动部署 Worker (BullMQ)
```

## 9. 模块依赖关系

```
                          ┌─────────────┐
                          │  前端 (Next.js)  │
                          └──────┬──────┘
                                 │ tRPC / WebSocket
                          ┌──────▼──────┐
                          │  路由层 (tRPC)  │ ← 41 个 Router
                          └──────┬──────┘
                                 │
                          ┌──────▼──────┐
                          │  服务层        │ ← 43 个 Service
                          └──┬───┬───┬──┘
                             │   │   │
              ┌──────────────┤   │   ├──────────────┐
              │              │   │   │              │
        ┌─────▼─────┐  ┌────▼───▼────┐  ┌──────────▼──────┐
        │ 数据库层    │  │ Docker/构建  │  │  Traefik/域名    │
        │ (Drizzle)  │  │ (Dockerode)  │  │  (YAML 配置)     │
        └─────┬─────┘  └──────┬──────┘  └────────┬────────┘
              │               │                   │
        ┌─────▼─────┐  ┌─────▼──────┐  ┌────────▼────────┐
        │ PostgreSQL  │  │ Docker      │  │  Traefik         │
        │ (postgres)  │  │ Daemon      │  │  (反向代理)       │
        └────────────┘  └────────────┘  └─────────────────┘
```

## 10. npm Scripts

### 根目录

| 脚本 | 说明 |
|------|------|
| `dokploy:dev` | 启动主应用开发模式 |
| `dokploy:build` | 构建主应用 |
| `dokploy:start` | 启动主应用生产模式 |
| `server:dev` | 启动 server 包开发模式 |
| `server:build` | 构建 server 包 |
| `build` | 构建整个 monorepo |
| `test` | 运行测试（Vitest） |
| `typecheck` | 全局类型检查 |
| `format-and-lint` | Biome 格式化和检查 |
| `generate:openapi` | 生成 OpenAPI 规范 |

## 11. 关键源文件索引

| 文件 | 说明 |
|------|------|
| `apps/dokploy/server/server.ts` | 主服务器启动入口 |
| `apps/dokploy/server/api/root.ts` | tRPC 路由注册 |
| `apps/dokploy/server/api/trpc.ts` | tRPC 上下文和中间件 |
| `packages/server/src/index.ts` | 共享库导出清单 |
| `packages/server/src/constants/index.ts` | 常量和路径定义 |
| `packages/server/src/db/index.ts` | 数据库连接 |
| `packages/server/src/db/constants.ts` | 数据库 URL 配置 |
| `packages/server/src/lib/auth.ts` | 认证系统配置 |
| `Dockerfile` | 主应用 Docker 镜像定义 |
| `Dockerfile.schedule` | 定时任务镜像定义 |
| `Dockerfile.monitoring` | 监控镜像定义 |
| `pnpm-workspace.yaml` | Monorepo 工作区定义 |

## 12. Go 重写注意事项

### 需要重写的部分
- `packages/server/` — 共享后端库（核心）
- `apps/dokploy/server/` — tRPC 路由 + WebSocket + 部署队列
- `apps/api/` — 外部 REST API
- `apps/schedules/` — 定时任务服务

### 不需要重写的部分
- `apps/monitoring/` — 已经是 Go 实现，可直接复用
- `apps/dokploy/pages/`、`components/`、`hooks/` 等 — 前端代码保持不变

### 语言无关可直接复用的部分
- Docker CLI 命令（构建、部署、清理）
- Nixpacks/Railpack/Buildpacks CLI 命令
- Traefik YAML 配置模板格式
- RClone 备份命令
- SSH 远程命令
- `/etc/dokploy/` 目录结构
- Docker Compose 文件格式
- Dockerfile 模板
