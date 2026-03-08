# handler

HTTP 处理器层，包含 REST API 和 tRPC 兼容层。
实现了 351 个 tRPC procedure，覆盖原 TS 版全部前端调用路径。企业功能（Stripe/AI/LicenseKey）通过 stub 实现。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| handler.go | 输入: DB, Docker, Config 等依赖 / 输出: Handler struct | Handler 构造 + 依赖注入 (Options 模式) |
| trpc.go | 输入: HTTP 请求 / 输出: tRPC 格式 JSON 响应 | tRPC 协议适配层：batch/单请求、query/mutation 路由分发 |
| trpc_routes.go | 输入: Handler / 输出: procedureRegistry | 汇总注册所有 tRPC procedure |
| trpc_application.go | 输入: tRPC 请求 / 输出: 应用 CRUD + 部署操作 | 应用管理的 18 个 procedure |
| trpc_compose.go | 输入: tRPC 请求 / 输出: Compose CRUD + 部署操作 | Compose 服务的 28 个 procedure |
| trpc_database.go | 输入: tRPC 请求 / 输出: 5 种数据库 CRUD | 数据库服务统一入口 |
| trpc_deployment.go | 输入: tRPC 请求 / 输出: 部署记录查询/取消 | 部署记录管理 |
| trpc_project.go | 输入: tRPC 请求 / 输出: 项目/环境 CRUD | 项目管理的 14 个 procedure |
| trpc_server.go | 输入: tRPC 请求 / 输出: 远程服务器 CRUD | 服务器管理 |
| trpc_settings.go | 输入: tRPC 请求 / 输出: 全局设置操作 | 设置管理的 54 个 procedure |
| trpc_user.go | 输入: tRPC 请求 / 输出: 用户 CRUD | 用户管理 |
| trpc_organization.go | 输入: tRPC 请求 / 输出: 组织 CRUD | 组织管理 |
| trpc_git_provider.go | 输入: tRPC 请求 / 输出: Git 提供商操作 | Git 提供商 + 仓库/分支查询 |
| trpc_notification.go | 输入: tRPC 请求 / 输出: 通知 CRUD | 通知管理 |
| trpc_ssh_key.go | 输入: tRPC 请求 / 输出: SSH 密钥 CRUD | SSH 密钥管理 |
| trpc_docker.go | 输入: tRPC 请求 / 输出: Docker 容器查询 | Docker 容器/配置查询 |
| trpc_schedule.go | 输入: tRPC 请求 / 输出: 定时任务 CRUD | 定时任务管理 |
| trpc_backup.go | 输入: tRPC 请求 / 输出: 备份 CRUD + 恢复 | 备份管理 |
| trpc_misc.go | 输入: tRPC 请求 / 输出: 杂项操作 | Domain/Mount/Port/Security/Redirect/Registry/Certificate/VolumeBackup/Rollback/Environment |
| trpc_patch.go | 输入: tRPC 请求 / 输出: 补丁操作 | 补丁管理 |
| trpc_sso.go | 输入: tRPC 请求 / 输出: SSO 操作 | SSO 提供商管理 |
| trpc_stubs.go | 输入: tRPC 请求 / 输出: 桩响应 | Stripe/AI/LicenseKey/Cluster/Swarm stub |
| webhook.go | 输入: HTTP POST / 输出: 部署触发 | GitHub/GitLab/Gitea/Bitbucket Webhook 处理 |
| auth.go | 输入: HTTP 请求 / 输出: 认证响应 | Better Auth 兼容认证端点 |
| frontend.go | 输入: HTTP 请求 / 输出: 静态文件 | 前端静态资源代理 |
| application.go | REST API / 应用操作 | REST 风格应用 handler |
| compose.go | REST API / Compose 操作 | REST 风格 Compose handler |
| project.go | REST API / 项目操作 | REST 风格项目 handler |
| server.go | REST API / 服务器操作 | REST 风格服务器 handler |
| user.go | REST API / 用户操作 | REST 风格用户 handler |
| admin.go | REST API / 管理操作 | 系统管理（清理/版本/配置） |
| docker.go | REST API / Docker 查询 | Docker 容器查询 |
| domain.go | REST API / 域名操作 | 域名 CRUD |
| deployment.go | REST API / 部署查询 | 部署记录查询 |
| backup.go | REST API / 备份操作 | 备份 CRUD |
| schedule.go | REST API / 定时任务 | 定时任务 CRUD |
| notification.go | REST API / 通知操作 | 通知 CRUD |
| database_common.go | REST API / 数据库通用 | 数据库公共逻辑 |
| database_*.go | REST API / 各数据库类型 | MySQL/PostgreSQL/MariaDB/MongoDB/Redis |
| git_provider.go | REST API / Git 提供商 | Git 提供商 CRUD |
| github.go, gitlab.go, gitea.go, bitbucket.go | REST API / 各 Git 平台 | 仓库/分支查询 |
| organization.go | REST API / 组织操作 | 组织管理 |
| mount.go, port.go, security.go, redirect.go | REST API / 配置项 | 挂载/端口/安全/重定向 CRUD |
| registry.go | REST API / Registry | Docker Registry CRUD |
| certificate.go | REST API / 证书 | SSL 证书 CRUD |
| ssh_key.go | REST API / SSH 密钥 | SSH 密钥 CRUD |
| destination.go | REST API / 备份目的地 | 备份目的地 CRUD |
| environment.go | REST API / 环境 | 环境 CRUD |
| volume_backup.go | REST API / 卷备份 | 卷备份 CRUD |
| rollback.go | REST API / 回滚 | 回滚 CRUD |
| preview_deployment.go | REST API / 预览部署 | 预览部署 CRUD |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
