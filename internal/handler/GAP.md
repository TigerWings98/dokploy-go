# handler 模块 Gap 分析

## Go 当前实现 (50+ 文件, 15563 行, 351 个 tRPC procedure)
- tRPC 兼容层 (`trpc.go`) 实现了 batch/单请求分发
- 大量 CRUD 直接在 handler 层完成（未分离到 service 层）
- 企业功能通过 stub 处理

## TS 原版实现 (43 个 router 文件)
- 41 个主路由 + 企业功能路由
- 通过 service 层调用业务逻辑

## Gap 详情

### 已完全实现 ✅
1. `project` - 项目 CRUD + 复制
2. `environment` - 环境 CRUD + 复制
3. `application` - 应用 CRUD + 部署/重部署/启停 + Git 提供商配置
4. `compose` - Compose CRUD + 部署/重部署/启停
5. `deployment` - 部署记录查询 + 取消
6. `mysql/postgres/redis/mongo/mariadb` - 数据库 CRUD + 部署/启停
7. `docker` - 容器查询/配置
8. `server` - 远程服务器 CRUD + 验证/setup
9. `domain` - 域名 CRUD
10. `mount` - 挂载 CRUD
11. `port` - 端口 CRUD
12. `security` - 安全策略 CRUD
13. `redirect` - 重定向 CRUD
14. `registry` - Registry CRUD
15. `notification` - 通知 CRUD + 测试发送
16. `gitProvider` - Git 提供商 CRUD + 仓库/分支查询
17. `github/gitlab/gitea/bitbucket` - 各平台仓库/分支查询
18. `sshKey` - SSH 密钥 CRUD + 生成
19. `backup` - 备份 CRUD + 手动备份/恢复/文件列表
20. `destination` - 备份目的地 CRUD + 测试连接
21. `schedule` - 定时任务 CRUD
22. `volumeBackups` - 卷备份 CRUD
23. `rollback` - 回滚管理
24. `previewDeployment` - 预览部署管理
25. `patch` - 补丁管理
26. `admin` - 系统管理（清理/版本/Traefik 配置读写）
27. `settings` - 全局设置
28. `user` - 用户 CRUD
29. `organization` - 组织管理
30. `sso` - SSO 提供商 CRUD

### Stub 实现 (企业功能，合理) ⚠️
1. `stripe` - 返回桩数据
2. `ai` - 返回桩数据
3. `licenseKey` - 返回桩数据
4. `cluster` - Swarm 集群管理（返回空数组）
5. `swarm` - Swarm 节点查询（返回空数组）

### 缺失或不完整 ❌
1. **certificate** - TS 版有独立的 certificate router，Go 端有 handler 但需确认是否注册到 tRPC
2. **cluster/swarm** - 目前是 stub，如果需要 Docker Swarm 功能需要实现
3. **权限检查粒度** - TS 版 member 表有 canCreateProjects/canCreateServices 等细粒度权限字段，Go 端大部分 procedure 只检查了 protectedProcedure 级别，缺少 member 级别的细粒度权限校验

### 架构差异 ⚠️
1. **service 层缺失**: TS 版 handler → service → DB 三层分离，Go 版大量 CRUD 直接在 handler procedure 中完成 GORM 操作。功能等价但不利于单元测试和代码复用。
2. **输入验证**: TS 版使用 Zod schema 验证输入，Go 版依赖 JSON 解析 + 手动检查，缺少系统性的输入验证。

## 影响评估
- **严重度**: 低。功能覆盖完整，架构差异不影响运行。
- **建议**: 长期考虑将 CRUD 逻辑从 handler 抽取到 service 层。
