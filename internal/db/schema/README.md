# db/schema

全部数据表的 GORM 模型定义，包含 ~65 个 struct，覆盖原 TS 版 42 个 schema 文件的全部业务表。
通过文件合并策略（如 database.go 包含 5 种数据库模型，user.go 包含用户/账户/组织/成员等）减少文件数量。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| application.go | 无 / Application struct + JSONField 泛型 | 应用模型，含 Swarm JSONB 字段和全部关系 |
| appname.go | 无 / GenerateAppName 函数 | 应用名生成（verb-adjective-noun-nanoid 格式） |
| backup.go | 无 / Destination, Backup, VolumeBackup struct | 备份目的地 + 数据库备份 + 卷备份模型 |
| compose.go | 无 / Compose struct | Docker Compose 服务模型 |
| database.go | 无 / Postgres, MySQL, MariaDB, Mongo, Redis struct | 5 种数据库服务模型 + SwarmConfig |
| deployment.go | 无 / Deployment struct | 部署记录模型 |
| domain.go | 无 / Domain struct | 域名配置模型 |
| enums.go | 无 / 全部枚举类型常量 | SourceType, BuildType, DeploymentStatus 等 |
| git_provider.go | 无 / GitProvider, Github, Gitlab, Bitbucket, Gitea struct | Git 提供商模型 |
| mount.go | 无 / Mount struct | 挂载点模型（bind/volume/file） |
| notification.go | 无 / Notification struct | 通知配置模型（11 种渠道） |
| patch.go | 无 / Patch struct | 补丁管理模型 |
| preview_deployment.go | 无 / PreviewDeployment, Rollback, Schedule struct | 预览部署 + 回滚 + 定时任务模型 |
| project.go | 无 / Project, Environment struct | 项目 + 环境模型 |
| server.go | 无 / Server, MetricsConfig 等 struct | 远程服务器模型 + 监控配置 |
| services.go | 无 / Port, Security, Redirect, Registry, SSHKey, Certificate struct | 应用附属配置模型 |
| settings.go | 无 / WebServerSettings struct | 全局设置模型 |
| sso.go | 无 / SSOProvider struct | SSO 提供商模型 |
| swarm.go | 无 / HealthCheckSwarm, RestartPolicySwarm 等 struct | Docker Swarm JSONB 子结构 |
| types.go | 无 / JSON[T] 泛型类型 | GORM JSONB 序列化/反序列化工具类型 |
| user.go | 无 / User, Account, Verification, Organization, Member, Invitation, TwoFactor, APIKey, Session struct | 用户体系全部模型 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
