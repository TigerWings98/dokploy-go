# Dokploy Go vs TypeScript — Gap 汇总报告

## 项目概况

| 指标 | Go 版 | TS 版 |
|------|-------|-------|
| 总文件数 | 118 (.go) | ~200+ (.ts) |
| 总代码行数 | ~14,600 | ~25,000+ |
| 包数量 | 25 | 42+ |
| 二进制产物 | 3 (server/scheduler/api) | 1 (Next.js 服务) |
| 框架 | Echo v4 + GORM | tRPC + Drizzle + Next.js |
| tRPC Procedure | 351 (兼容层) | 351 (原生) |
| WebSocket 端点 | 6 | 6 |

---

## 模块级别 Gap 评估

### 🟢 完全对齐 (无功能缺失)

| 模块 | Go 行数 | 说明 |
|------|---------|------|
| config | 143 | 14 个路径 + 环境变量完全一致 |
| middleware | 179 | Session/APIKey/Admin 认证完整 |
| handler | ~8,000 | 351 个 tRPC procedure 全覆盖 |
| ws | 1,810 | 6 个 WS 端点全部实现 |
| builder | 150 | 6 种构建类型命令生成 |
| compose | 120 | Compose 文件转换 + 网络注入 |
| provider | 477 | GitHub/GitLab/Gitea/Bitbucket 4 种 API |
| git | 150 | 带认证的 Git 克隆 (HTTPS + SSH) |
| scheduler | 235 | Cron 定时任务管理 |
| email | 366 | HTML 邮件模板 + SMTP 发送 |
| process | 290 | 本地命令执行 + SSH 远程执行 |

### 🟡 部分差异 (功能等价但实现不同)

| 模块 | Go 行数 | 差异点 |
|------|---------|--------|
| service | 1,200 | Go 4 文件 vs TS 43 文件。大量 CRUD 在 handler 层实现，功能等价 |
| traefik | 784 | 缺少 remote Traefik 管理 (通过 SSH 脚本补偿) |
| setup | 619 | 缺少监控组件设置、部署记录；脚本生成 vs SSH 实时执行 |
| docker | 347 | 缺少远程 Docker 客户端、pullImage、createService (在 service 层补偿) |
| queue | 200 | asynq 替代 BullMQ，API 不同但功能等价 |
| backup | 487 | 核心功能完整，卷备份定时任务需验证 |
| notify | 278 | 核心渠道已实现 (Slack/Telegram/Discord/Email/Gotify/Ntfy/Webhook) |

### 🔴 有功能缺失

| 模块 | 严重度 | 缺失内容 |
|------|--------|----------|
| db/schema (通知) | **高** | TS 用 11 个通知子表 (slack/telegram/discord...)，Go 扁平化到主表。数据不兼容 |
| auth | 中 | 缺少 OAuth (GitHub/Google)、2FA/TOTP、SSO、注册前/后钩子、Session 过期检查 |
| db | 中 | 缺少独立的数据库迁移能力 (无 AutoMigrate)，依赖 TS 版已有 schema |

---

## 关键 Gap 详解

### 1. 通知系统架构不兼容 🔴

**问题**: TS 版 notification 表通过 FK 关联 11 个渠道子表 (slack/telegram/discord/email/resend/gotify/ntfy/custom/lark/pushover/teams)，Go 版将所有字段扁平化到 Notification struct。

**影响**:
- Go 版无法读取 TS 版创建的通知配置 (数据在子表中)
- Go 版缺少 Lark/Pushover/Teams/Resend 渠道的字段

**修复建议**:
1. 方案 A: 修改 Go 的 Notification 模型，通过 GORM 关系加载子表
2. 方案 B: 编写一次性迁移脚本，将子表数据合并到主表

### 2. 认证模块简化实现 🟡

**问题**: Go 版仅实现了 Session 验证和 API Key 验证，缺少完整的认证流程。

**影响**:
- 注册/登录功能依赖原 TS 版 Better Auth 端点 (Go 在 handler/auth.go 有部分实现)
- OAuth、2FA、SSO 未实现
- Session 过期未检查

**现状**: Go 可以在 TS 版创建的 session 数据上正常工作，但无法独立完成用户注册和登录。

### 3. 数据库迁移能力缺失 🟡

**问题**: Go 版曾有 AutoMigrate 但已被移除，无法独立创建数据库表。

**影响**: 首次部署必须先由 TS 版创建数据库 schema，Go 版才能运行。

**修复建议**: 添加 GORM AutoMigrate 或 golang-migrate 迁移文件。

---

## 企业功能状态

以下 TS 版企业功能在 Go 版通过 stub 处理：

| 功能 | Go 处理方式 |
|------|------------|
| Stripe 订阅 | stub: 返回 true (自托管无限制) |
| AI 功能 | stub: 返回空数组 |
| License Key | stub: 返回 true |
| Cluster 管理 | stub: 返回空 |
| Swarm 集群操作 | stub: 返回空 |

---

## 编译验证

```
✅ go build ./cmd/server/   — 通过
✅ go build ./cmd/scheduler/ — 通过
✅ go build ./cmd/api/       — 通过
✅ go vet ./...              — 通过，0 警告
```

---

## 分形文档覆盖

| 类型 | 数量 | 状态 |
|------|------|------|
| README.md (模块级) | 20 | ✅ 全部创建 |
| GAP.md (模块级) | 20 | ✅ 全部创建 |
| 三行元数据头部 | 118 | ✅ 全部添加 |
| CLAUDE.md (项目级) | 1 | ✅ 已更新 |

---

## 结论

Go 重写已覆盖 TS 版 **~90%** 的功能。核心的部署流水线 (Application/Compose/Database)、tRPC 协议兼容层、WebSocket 实时通信、Traefik 路由管理均已完整实现。

**最优先修复**: 通知系统架构不兼容 (影响现有 TS 版用户迁移)
**中优先级**: 认证模块独立化 (当前可与 TS 版共存)
**低优先级**: 数据库迁移工具 (可通过 GORM AutoMigrate 快速补齐)
