# service 模块 Gap 分析

## Go 当前实现 (4 文件, 1297 行)
- `application.go` (443行): ApplicationService - FindByID + Deploy 全流水线 (克隆→构建→Docker Service)
- `compose.go` (248行): ComposeService - Compose 部署 (docker stack deploy)
- `database.go` (379行): DatabaseService - 5 种数据库的 Docker Service 部署
- `preview.go` (227行): PreviewService - PR 预览部署生命周期

## TS 原版实现 (43 个 service 文件)
涵盖：project, environment, application, compose, deployment, previewDeployment,
mysql, postgres, redis, mariadb, mongo, server, docker, cluster, domain, certificate,
mount, port, security, redirect, registry, notification, gitProvider, github, gitlab,
gitea, bitbucket, sshKey, backup, destination, volumeBackup, rollback, schedule,
admin, settings, user, organization, patch, webServer, sso, stripe, ai, licenseKey

## Gap 详情

### 已在 service 层实现 ✅
1. **application** - 完整部署流水线：FindByID → 克隆 → 构建 → Docker Service → Traefik
2. **compose** - 完整部署流水线：拉取代码 → 转换文件 → docker stack deploy
3. **database** - 5 种数据库的 Docker Service 创建/启停
4. **preview** - PR 预览部署创建/清理

### 逻辑在 handler 层实现（未分离到 service）⚠️
以下 TS 版 service 的逻辑在 Go 中直接写在 handler 的 tRPC procedure 内：

1. **project** - CRUD 直接在 trpc_project.go 中用 GORM
2. **environment** - CRUD 直接在 trpc_misc.go 中
3. **deployment** - 查询/取消直接在 trpc_deployment.go 中
4. **server** - CRUD + setup 直接在 trpc_server.go 中
5. **docker** - 容器查询直接在 trpc_docker.go 中
6. **domain** - CRUD 直接在 trpc_misc.go 中
7. **mount/port/security/redirect** - CRUD 直接在 trpc_misc.go 中
8. **registry** - CRUD 直接在 trpc_misc.go 中
9. **notification** - CRUD 直接在 trpc_notification.go 中
10. **gitProvider/github/gitlab/gitea/bitbucket** - 直接在 trpc_git_provider.go 中
11. **sshKey** - CRUD 直接在 trpc_ssh_key.go 中
12. **backup/destination** - CRUD 直接在 trpc_backup.go 中
13. **volumeBackup** - CRUD 直接在 trpc_misc.go 中
14. **rollback** - CRUD 直接在 trpc_misc.go 中
15. **schedule** - CRUD 直接在 trpc_schedule.go 中
16. **admin/settings** - 直接在 trpc_settings.go 中
17. **user** - CRUD 直接在 trpc_user.go 中
18. **organization** - CRUD 直接在 trpc_organization.go 中

### 完全缺失 ❌
1. **cluster** - Swarm 集群管理（stub）
2. **certificate** - 证书服务逻辑（handler 有但 service 层缺失）
3. **sso** - SSO 服务逻辑（handler 有 CRUD 但核心 SSO 认证流程缺失）

## 影响评估
- **严重度**: 低。功能上等价（CRUD 在 handler 完成），但架构分层不清晰。
- **建议**: 复杂业务逻辑（如部署、备份、服务器 setup）已在 service 层，简单 CRUD 在 handler 层是可接受的 Go 风格实践。如需改进可后续逐步抽取。
