# 数据库 Schema 与数据模型

## 1. ORM 与数据库连接

### 1.1 技术选型
- **ORM**: Drizzle ORM（TypeScript-first ORM）
- **驱动**: postgres.js（轻量 PostgreSQL 驱动）
- **验证**: drizzle-zod（自动生成 Zod Schema）
- **迁移**: drizzle-kit（数据库迁移工具）

### 1.2 数据库连接

**源文件**: `packages/server/src/db/constants.ts`

数据库 URL 优先级：
1. `DATABASE_URL` 环境变量（完整连接串）
2. `POSTGRES_PASSWORD_FILE`（Docker Secret 方式）
3. 旧版硬编码凭证（已标记为废弃）

默认配置：
- 用户: `dokploy`
- 数据库: `dokploy`
- 主机: `dokploy-postgres:5432`

**源文件**: `packages/server/src/db/index.ts`

```typescript
// 生产环境：直接创建连接
// 开发环境：使用全局缓存避免热重载时多连接
export const db: PostgresJsDatabase<typeof schema>
```

### 1.3 ID 生成策略
- 所有主键使用 `nanoid()` 生成（默认 21 字符）
- `appName` 使用 `generateAppName(type)` 生成，格式：`{type}-{verb}-{adjective}-{noun}-{nanoid6}`
- 示例: `app-hack-digital-pixel-abc123`

## 2. 枚举类型汇总

**源文件**: `packages/server/src/db/schema/shared.ts`, 以及各 schema 文件

| 枚举名 | 值 | 所在文件 |
|--------|------|---------|
| `applicationStatus` | idle, running, done, error | shared.ts |
| `certificateType` | letsencrypt, none, custom | shared.ts |
| `triggerType` | push, tag | shared.ts |
| `sourceType` | docker, git, github, gitlab, bitbucket, gitea, drop | application.ts |
| `buildType` | dockerfile, heroku_buildpacks, paketo_buildpacks, nixpacks, static, railpack | application.ts |
| `sourceTypeCompose` | git, github, gitlab, bitbucket, gitea, raw | compose.ts |
| `composeType` | docker-compose, stack | compose.ts |
| `deploymentStatus` | running, done, error, cancelled | deployment.ts |
| `domainType` | compose, application, preview | domain.ts |
| `serverStatus` | active, inactive | server.ts |
| `serverType` | deploy, build | server.ts |
| `databaseType` | postgres, mariadb, mysql, mongo, web-server | backups.ts |
| `backupType` | database, compose | backups.ts |
| `serviceType` | application, postgres, mysql, mariadb, mongo, redis, compose | mount.ts |
| `mountType` | bind, volume, file | mount.ts |
| `notificationType` | slack, telegram, discord, email, resend, gotify, ntfy, pushover, custom, lark, teams | notification.ts |
| `scheduleType` | application, compose, server, dokploy-server | schedule.ts |
| `shellType` | bash, sh | schedule.ts |

## 3. 核心数据模型

### 3.1 用户与认证

#### user 表
**源文件**: `packages/server/src/db/schema/user.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | nanoid |
| firstName | text | 名 |
| lastName | text | 姓 |
| email | text UNIQUE | 邮箱 |
| emailVerified | boolean | 邮箱已验证 |
| image | text | 头像 |
| role | text | 角色（默认 "user"） |
| isRegistered | boolean | 是否已注册完成 |
| twoFactorEnabled | boolean | 2FA 启用 |
| banned | boolean | 是否封禁 |
| banReason | text | 封禁原因 |
| banExpires | timestamp | 封禁过期时间 |
| enablePaidFeatures | boolean | 付费功能 |
| allowImpersonation | boolean | 允许模拟登录 |
| enableEnterpriseFeatures | boolean | 企业功能 |
| licenseKey | text | 许可证密钥 |
| isValidEnterpriseLicense | boolean | 企业许可证有效 |
| stripeCustomerId | text | Stripe 客户 ID |
| stripeSubscriptionId | text | Stripe 订阅 ID |
| serversQuantity | integer | 服务器数量限制 |
| trustedOrigins | text[] | 信任的来源 |
| expirationDate | text | 过期日期 |
| createdAt | text/timestamp | 创建时间 |
| updatedAt | timestamp | 更新时间 |

**关系**: → account (1:1), → organization (1:N), → project (1:N), → apikey (1:N), → ssoProvider (1:N), → backup (1:N), → schedule (1:N)

#### account 表
**源文件**: `packages/server/src/db/schema/account.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | nanoid |
| accountId | text | 账户标识 |
| providerId | text | 提供商 ID（credential/github/google） |
| userId | text FK → user | 用户 ID |
| accessToken | text | OAuth 访问令牌 |
| refreshToken | text | OAuth 刷新令牌 |
| idToken | text | OAuth ID 令牌 |
| accessTokenExpiresAt | timestamp | 访问令牌过期 |
| refreshTokenExpiresAt | timestamp | 刷新令牌过期 |
| scope | text | OAuth 范围 |
| password | text | 密码哈希（credential 模式） |
| is2FAEnabled | boolean | 2FA 是否启用 |
| resetPasswordToken | text | 重置密码令牌 |
| resetPasswordExpiresAt | text | 重置密码过期 |
| confirmationToken | text | 确认令牌 |
| confirmationExpiresAt | text | 确认过期 |
| createdAt/updatedAt | timestamp | 时间戳 |

#### verification 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | |
| identifier | text | 标识符 |
| value | text | 验证值 |
| expiresAt | timestamp | 过期时间 |

#### twoFactor 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | |
| secret | text | TOTP 密钥 |
| backupCodes | text | 备份码 |
| userId | text FK → user | |

#### apikey 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | |
| name | text | 名称 |
| key | text | API Key 哈希 |
| start/prefix | text | 前缀显示 |
| userId | text FK → user | |
| enabled | boolean | 启用 |
| rateLimitEnabled | boolean | 速率限制 |
| rateLimitTimeWindow | integer | 窗口时间 |
| rateLimitMax | integer | 最大请求数 |
| requestCount | integer | 请求计数 |
| remaining | integer | 剩余配额 |
| expiresAt | timestamp | 过期时间 |

### 3.2 组织与权限

#### organization 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | nanoid |
| name | text | 名称 |
| slug | text UNIQUE | URL 标识 |
| logo | text | Logo |
| ownerId | text FK → user | 所有者 |
| createdAt | timestamp | |
| metadata | text | |

**关系**: → user (owner), → server (1:N), → project (1:N), → member (1:N), → ssoProvider (1:N)

#### member 表（权限控制核心）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | nanoid |
| organizationId | text FK → organization | |
| userId | text FK → user | |
| role | text | owner/member/admin |
| isDefault | boolean | 默认成员 |
| canCreateProjects | boolean | 权限：创建项目 |
| canAccessToSSHKeys | boolean | 权限：SSH 密钥 |
| canCreateServices | boolean | 权限：创建服务 |
| canDeleteProjects | boolean | 权限：删除项目 |
| canDeleteServices | boolean | 权限：删除服务 |
| canAccessToDocker | boolean | 权限：Docker |
| canAccessToAPI | boolean | 权限：API |
| canAccessToGitProviders | boolean | 权限：Git 提供商 |
| canAccessToTraefikFiles | boolean | 权限：Traefik |
| canDeleteEnvironments | boolean | 权限：删除环境 |
| canCreateEnvironments | boolean | 权限：创建环境 |
| accessedProjects | text[] | 可访问的项目 ID 列表 |
| accessedEnvironments | text[] | 可访问的环境 ID 列表 |
| accessedServices | text[] | 可访问的服务 ID 列表 |

#### invitation 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | |
| organizationId | text FK → organization | |
| email | text | 被邀请人邮箱 |
| role | text | owner/member/admin |
| status | text | 状态 |
| expiresAt | timestamp | 过期时间 |
| inviterId | text FK → user | 邀请人 |

### 3.3 项目层级结构

#### project 表
**源文件**: `packages/server/src/db/schema/project.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| projectId | text PK | nanoid |
| name | text | 项目名称 |
| description | text | 描述 |
| env | text | 项目级环境变量 |
| organizationId | text FK → organization | 所属组织 |
| createdAt | text | |

**关系**: → environment (1:N), → organization (N:1)

#### environment 表
**源文件**: `packages/server/src/db/schema/environment.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| environmentId | text PK | nanoid |
| name | text | 环境名称（如 dev/staging/production） |
| description | text | |
| env | text | 环境级环境变量 |
| projectId | text FK → project | |
| isDefault | boolean | 是否默认环境 |
| createdAt | text | |

**关系**: → application (1:N), → compose (1:N), → mysql (1:N), → postgres (1:N), → redis (1:N), → mongo (1:N), → mariadb (1:N)

### 3.4 应用（application）

**源文件**: `packages/server/src/db/schema/application.ts`

这是系统中最复杂的表，包含应用配置的所有方面。

#### 基础字段

| 字段 | 类型 | 说明 |
|------|------|------|
| applicationId | text PK | nanoid |
| name | text | 显示名称 |
| appName | text UNIQUE | Docker 服务名称（自动生成） |
| description | text | |
| env | text | 环境变量（KEY=VALUE 格式） |
| buildArgs | text | 构建参数 |
| buildSecrets | text | 构建密钥 |
| command | text | 运行命令 |
| args | text[] | 运行参数 |
| refreshToken | text | Webhook 刷新令牌 |
| createdAt | text | |

#### 代码源配置

| 字段 | 类型 | 说明 |
|------|------|------|
| sourceType | enum | docker/git/github/gitlab/bitbucket/gitea/drop |
| triggerType | enum | push/tag |
| autoDeploy | boolean | 自动部署 |
| watchPaths | text[] | 监控路径 |
| enableSubmodules | boolean | 启用子模块 |

#### GitHub 专用字段
repository, owner, branch, buildPath

#### GitLab 专用字段
gitlabProjectId, gitlabRepository, gitlabOwner, gitlabBranch, gitlabBuildPath, gitlabPathNamespace

#### Gitea 专用字段
giteaRepository, giteaOwner, giteaBranch, giteaBuildPath

#### Bitbucket 专用字段
bitbucketRepository, bitbucketRepositorySlug, bitbucketOwner, bitbucketBranch, bitbucketBuildPath

#### Docker 专用字段
username, password, dockerImage, registryUrl

#### Git 通用字段
customGitUrl, customGitBranch, customGitBuildPath, customGitSSHKeyId

#### 构建配置

| 字段 | 类型 | 说明 |
|------|------|------|
| buildType | enum | dockerfile/heroku_buildpacks/paketo_buildpacks/nixpacks/static/railpack |
| dockerfile | text | Dockerfile 路径（默认 "Dockerfile"） |
| dockerContextPath | text | Docker 构建上下文 |
| dockerBuildStage | text | 多阶段构建目标 |
| railpackVersion | text | Railpack 版本 |
| herokuVersion | text | Heroku 版本 |
| publishDirectory | text | 静态文件发布目录 |
| isStaticSpa | boolean | 是否 SPA |
| createEnvFile | boolean | 创建 .env 文件 |
| cleanCache | boolean | 清理构建缓存 |
| dropBuildPath | text | Drop 构建路径 |

#### 资源限制

| 字段 | 类型 | 说明 |
|------|------|------|
| memoryReservation | text | 内存预留 |
| memoryLimit | text | 内存限制 |
| cpuReservation | text | CPU 预留 |
| cpuLimit | text | CPU 限制 |
| replicas | integer | 副本数（默认 1） |

#### Docker Swarm 配置（JSON 字段）

| 字段 | 类型 | 说明 |
|------|------|------|
| healthCheckSwarm | json | 健康检查配置 |
| restartPolicySwarm | json | 重启策略 |
| placementSwarm | json | 放置约束 |
| updateConfigSwarm | json | 更新配置 |
| rollbackConfigSwarm | json | 回滚配置 |
| modeSwarm | json | 服务模式（Replicated/Global） |
| labelsSwarm | json | 标签 |
| networkSwarm | json | 网络 |
| stopGracePeriodSwarm | bigint | 停止等待时间 |
| endpointSpecSwarm | json | 端点配置 |
| ulimitsSwarm | json | 资源限制 |

#### 预览部署配置

| 字段 | 类型 | 说明 |
|------|------|------|
| previewEnv | text | 预览环境变量 |
| previewBuildArgs | text | 预览构建参数 |
| previewBuildSecrets | text | 预览构建密钥 |
| previewLabels | text[] | 预览标签 |
| previewWildcard | text | 通配符域名 |
| previewPort | integer | 预览端口（默认 3000） |
| previewHttps | boolean | 预览 HTTPS |
| previewPath | text | 预览路径 |
| previewCertificateType | enum | 预览证书类型 |
| previewCustomCertResolver | text | 自定义证书解析器 |
| previewLimit | integer | 预览部署上限（默认 3） |
| isPreviewDeploymentsActive | boolean | 启用预览部署 |
| previewRequireCollaboratorPermissions | boolean | 要求协作者权限 |

#### 外键关系

| 字段 | 引用 | 删除行为 |
|------|------|---------|
| environmentId | → environment | CASCADE |
| githubId | → github | SET NULL |
| gitlabId | → gitlab | SET NULL |
| giteaId | → gitea | SET NULL |
| bitbucketId | → bitbucket | SET NULL |
| serverId | → server | CASCADE |
| buildServerId | → server | SET NULL |
| registryId | → registry | SET NULL |
| rollbackRegistryId | → registry | SET NULL |
| buildRegistryId | → registry | SET NULL |
| customGitSSHKeyId | → ssh_key | SET NULL |

**关系**: → environment, → deployments (1:N), → domains (1:N), → mounts (1:N), → redirects (1:N), → security (1:N), → ports (1:N), → previewDeployments (1:N), → patches (1:N), → github, → gitlab, → gitea, → bitbucket, → server, → buildServer, → registry, → buildRegistry, → rollbackRegistry, → customGitSSHKey

### 3.5 Docker Compose 服务

**源文件**: `packages/server/src/db/schema/compose.ts`

与 application 类似但用于 Docker Compose 部署：

| 字段 | 类型 | 说明 |
|------|------|------|
| composeId | text PK | |
| name | text | 名称 |
| appName | text | Docker 服务名 |
| composeFile | text | Compose 文件内容 |
| composePath | text | Compose 文件路径（默认 ./docker-compose.yml） |
| composeType | enum | docker-compose / stack |
| sourceType | enum | git/github/gitlab/bitbucket/gitea/raw |
| suffix | text | 随机化后缀 |
| randomize | boolean | 是否随机化 |
| isolatedDeployment | boolean | 隔离部署 |
| command | text | 自定义命令 |
| composeStatus | enum | idle/running/done/error |
| （Git 提供商字段同 application） | | |

### 3.6 部署记录

**源文件**: `packages/server/src/db/schema/deployment.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| deploymentId | text PK | |
| title | text | 部署标题 |
| description | text | |
| status | enum | running/done/error/cancelled |
| logPath | text | 日志文件路径 |
| pid | text | 进程 ID |
| isPreviewDeployment | boolean | 是否预览部署 |
| errorMessage | text | 错误信息 |
| createdAt | text | |
| startedAt | text | 开始时间 |
| finishedAt | text | 完成时间 |

**外键**: applicationId, composeId, serverId, buildServerId, previewDeploymentId, scheduleId, backupId, rollbackId, volumeBackupId

### 3.7 域名

**源文件**: `packages/server/src/db/schema/domain.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| domainId | text PK | |
| host | text | 域名 |
| https | boolean | HTTPS |
| port | integer | 目标端口（默认 3000） |
| path | text | URL 路径（默认 /） |
| serviceName | text | Compose 服务名 |
| domainType | enum | compose/application/preview |
| certificateType | enum | letsencrypt/none/custom |
| customCertResolver | text | 自定义证书解析器 |
| internalPath | text | 内部路径 |
| stripPath | boolean | 路径剥离 |
| uniqueConfigKey | serial | 唯一配置键 |

**外键**: applicationId, composeId, previewDeploymentId

### 3.8 服务器

**源文件**: `packages/server/src/db/schema/server.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| serverId | text PK | |
| name | text | 名称 |
| description | text | |
| ipAddress | text | IP 地址 |
| port | integer | SSH 端口 |
| username | text | SSH 用户名（默认 root） |
| appName | text | |
| enableDockerCleanup | boolean | 启用 Docker 清理 |
| serverStatus | enum | active/inactive |
| serverType | enum | deploy/build |
| command | text | 自定义命令 |
| sshKeyId | text FK → ssh_key | |
| organizationId | text FK → organization | |
| metricsConfig | jsonb | 监控配置（详细结构见下） |

**metricsConfig 结构**:
```typescript
{
  server: {
    type: "Dokploy" | "Remote",
    refreshRate: number,      // 采集间隔秒数
    port: number,             // 监控服务端口
    token: string,            // 认证令牌
    urlCallback: string,      // 回调 URL
    retentionDays: number,    // 数据保留天数
    cronJob: string,          // 清理 Cron
    thresholds: {
      cpu: number,            // CPU 阈值 %
      memory: number          // 内存阈值 %
    }
  },
  containers: {
    refreshRate: number,
    services: {
      include: string[],
      exclude: string[]
    }
  }
}
```

## 4. 数据库服务表

以下五个表结构高度相似，都包含：基础信息、数据库凭证、Docker 镜像、资源限制、Swarm 配置。

### 4.1 mysql 表
**源文件**: `packages/server/src/db/schema/mysql.ts`

独有字段: databaseName, databaseUser, databasePassword, databaseRootPassword, dockerImage, externalPort

### 4.2 postgres 表
**源文件**: `packages/server/src/db/schema/postgres.ts`

独有字段: databaseName, databaseUser, databasePassword, dockerImage, externalPort

### 4.3 redis 表
**源文件**: `packages/server/src/db/schema/redis.ts`

独有字段: databasePassword, dockerImage, externalPort（无数据库名和用户名）

### 4.4 mariadb 表
**源文件**: `packages/server/src/db/schema/mariadb.ts`

独有字段: 同 mysql（databaseName, databaseUser, databasePassword, databaseRootPassword）

### 4.5 mongo 表
**源文件**: `packages/server/src/db/schema/mongo.ts`

独有字段: databaseName, databaseUser, databasePassword, dockerImage, externalPort

**共有字段**:
- ID, name, appName (UNIQUE), description
- env, command, args
- memoryReservation, memoryLimit, cpuReservation, cpuLimit
- applicationStatus (enum)
- replicas
- 所有 Swarm JSON 配置字段
- environmentId (FK), serverId (FK)
- createdAt

**关系**: → environment, → backups (1:N), → mounts (1:N), → server

## 5. 辅助实体

### 5.1 mount 表
**源文件**: `packages/server/src/db/schema/mount.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| mountId | text PK | |
| type | enum | bind/volume/file |
| hostPath | text | 主机路径（bind 类型） |
| volumeName | text | 卷名（volume 类型） |
| filePath | text | 文件路径（file 类型） |
| content | text | 文件内容（file 类型） |
| mountPath | text | 容器内挂载路径 |
| serviceType | enum | application/postgres/mysql/mariadb/mongo/redis/compose |

**外键**: applicationId, postgresId, mariadbId, mongoId, mysqlId, redisId, composeId

### 5.2 port 表
**源文件**: `packages/server/src/db/schema/port.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| portId | text PK | |
| publishedPort | integer | 发布端口 |
| targetPort | integer | 目标端口 |
| protocol | enum | tcp/udp |
| applicationId | text FK | |

### 5.3 redirect 表
**源文件**: `packages/server/src/db/schema/redirects.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| redirectId | text PK | |
| regex | text | 匹配规则 |
| replacement | text | 替换值 |
| permanent | boolean | 永久重定向 |
| uniqueConfigKey | serial | |
| applicationId | text FK | |

### 5.4 security 表
**源文件**: `packages/server/src/db/schema/security.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| securityId | text PK | |
| username | text | 基本认证用户名 |
| password | text | 基本认证密码 |
| uniqueConfigKey | serial | |
| applicationId | text FK | |

### 5.5 certificate 表
**源文件**: `packages/server/src/db/schema/certificate.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| certificateId | text PK | |
| name | text | 名称 |
| certificateData | text | 证书内容 |
| privateKey | text | 私钥 |
| certificatePath | text | 文件路径 |
| autoRenew | boolean | 自动续期 |
| serverId | text FK → server | |

### 5.6 ssh_key 表
**源文件**: `packages/server/src/db/schema/ssh-key.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| sshKeyId | text PK | |
| name | text | 名称 |
| description | text | |
| publicKey | text | 公钥 |
| privateKey | text | 私钥 |
| organizationId | text FK → organization | |

### 5.7 registry 表
**源文件**: `packages/server/src/db/schema/registry.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| registryId | text PK | |
| registryName | text | 名称 |
| registryType | enum | selfHosted/cloud |
| imagePrefix | text | 镜像前缀 |
| username | text | 用户名 |
| password | text | 密码 |
| registryUrl | text | 仓库 URL |
| organizationId | text FK | |

### 5.8 destination 表（备份目的地）
**源文件**: `packages/server/src/db/schema/destination.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| destinationId | text PK | |
| name | text | 名称 |
| accessKey | text | S3 Access Key |
| secretAccessKey | text | S3 Secret Key |
| bucket | text | S3 Bucket |
| region | text | S3 Region |
| endpoint | text | S3 Endpoint |
| organizationId | text FK | |

### 5.9 notification 表（及其子表）
**源文件**: `packages/server/src/db/schema/notification.ts`

**主表 notification**: 包含事件开关（appDeploy, appBuildError, databaseBackup, volumeBackup, dokployRestart, dockerCleanup, serverThreshold）和各渠道 FK。

**子表**:
- **slack**: webhookUrl, channel
- **telegram**: botToken, chatId, messageThreadId
- **discord**: webhookUrl, decoration
- **email**: smtpServer, smtpPort, username, password, fromAddress, toAddresses[]
- **resend**: apiKey, fromAddress, toAddresses[]
- **gotify**: serverUrl, appToken, priority, decoration
- **ntfy**: serverUrl, topic, accessToken, priority
- **custom**: endpoint, headers (jsonb)
- **lark**: webhookUrl
- **pushover**: userKey, apiToken, priority, retry, expire
- **teams**: webhookUrl

### 5.10 backup 表
**源文件**: `packages/server/src/db/schema/backups.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| backupId | text PK | |
| appName | text UNIQUE | |
| schedule | text | Cron 表达式 |
| enabled | boolean | |
| database | text | 数据库名 |
| prefix | text | 备份前缀 |
| serviceName | text | Compose 服务名 |
| keepLatestCount | integer | 保留最新 N 个 |
| backupType | enum | database/compose |
| databaseType | enum | postgres/mariadb/mysql/mongo/web-server |
| destinationId | text FK → destination | |
| metadata | jsonb | 数据库凭证（Compose 备份用） |

**外键**: postgresId, mariadbId, mysqlId, mongoId, composeId, userId

### 5.11 volume_backup 表
**源文件**: `packages/server/src/db/schema/volume-backups.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| volumeBackupId | text PK | |
| appName | text | |
| schedule | text | Cron 表达式 |
| enabled | boolean | |
| prefix | text | |
| sourcePath | text | 源路径 |
| destinationId | text FK → destination | |

### 5.12 rollback 表
**源文件**: `packages/server/src/db/schema/rollbacks.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| rollbackId | text PK | |
| dockerImage | text | 回滚到的镜像 |
| applicationId | text FK → application | |
| deploymentId | text FK → deployment | |

### 5.13 schedule 表
**源文件**: `packages/server/src/db/schema/schedule.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| scheduleId | text PK | |
| name | text | |
| cronExpression | text | Cron 表达式 |
| appName | text | |
| serviceName | text | |
| shellType | enum | bash/sh |
| scheduleType | enum | application/compose/server/dokploy-server |
| command | text | |
| script | text | |
| enabled | boolean | |
| timezone | text | |

**外键**: applicationId, composeId, serverId, userId

### 5.14 preview_deployment 表
**源文件**: `packages/server/src/db/schema/preview-deployments.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| previewDeploymentId | text PK | |
| branch | text | PR 分支 |
| pullRequestId | text | PR 编号 |
| pullRequestNumber | text | PR 序号 |
| pullRequestTitle | text | PR 标题 |
| pullRequestURL | text | PR URL |
| pullRequestCommentId | text | PR 评论 ID |
| appName | text | 预览应用名 |
| applicationId | text FK → application | |
| previewStatus | enum | idle/running/done/error |

### 5.15 patch 表
**源文件**: `packages/server/src/db/schema/patch.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| patchId | text PK | |
| patchType | enum | application/compose |
| status | enum | pending/running/done/error |
| applicationId | text FK | |
| composeId | text FK | |

## 6. Git 提供商表

### 6.1 git_provider 表
**源文件**: `packages/server/src/db/schema/git-provider.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| gitProviderId | text PK | |
| providerType | enum | github/gitlab/bitbucket/gitea |
| name | text | |
| organizationId | text FK | |

### 6.2 github 表
**源文件**: `packages/server/src/db/schema/github.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| githubId | text PK | |
| githubAppName | text | App 名称 |
| githubAppId | integer | App ID |
| githubInstallationId | text | 安装 ID |
| githubClientId | text | OAuth Client ID |
| githubClientSecret | text | OAuth Client Secret |
| githubPrivateKey | text | App 私钥 |
| githubWebhookSecret | text | Webhook 密钥 |
| gitProviderId | text FK → git_provider | |

### 6.3 gitlab 表
**源文件**: `packages/server/src/db/schema/gitlab.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| gitlabId | text PK | |
| gitlabUrl | text | GitLab 实例 URL |
| application_id | text | App ID |
| redirect_uri | text | 回调 URL |
| secret | text | App Secret |
| accessToken | text | |
| refreshToken | text | |
| groupName | text | |
| expiresAt | integer | |
| gitProviderId | text FK | |

### 6.4 gitea 表
**源文件**: `packages/server/src/db/schema/gitea.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| giteaId | text PK | |
| giteaUrl | text | Gitea 实例 URL |
| clientId | text | |
| clientSecret | text | |
| accessToken | text | |
| refreshToken | text | |
| giteaOrganization | text | |
| gitProviderId | text FK | |

### 6.5 bitbucket 表
**源文件**: `packages/server/src/db/schema/bitbucket.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| bitbucketId | text PK | |
| bitbucketUrl | text | |
| clientId | text | |
| clientSecret | text | |
| accessToken | text | |
| refreshToken | text | |
| bitbucketWorkspace | text | |
| gitProviderId | text FK | |

## 7. 其他表

### 7.1 session 表
**源文件**: `packages/server/src/db/schema/session.ts`

| 字段 | 类型 | 说明 |
|------|------|------|
| id | text PK | |
| expiresAt | timestamp | |
| token | text UNIQUE | |
| ipAddress | text | |
| userAgent | text | |
| userId | text FK → user | |
| activeOrganizationId | text | 当前活跃组织 |
| impersonatedBy | text | 模拟登录者 |

### 7.2 sso_provider 表
**源文件**: `packages/server/src/db/schema/sso.ts`

SSO 配置（企业功能）。

### 7.3 web_server_settings 表
**源文件**: `packages/server/src/db/schema/web-server-settings.ts`

Traefik 相关设置。

### 7.4 ai 表
**源文件**: `packages/server/src/db/schema/ai.ts`

AI 功能配置。

## 8. Swarm 共享类型

**源文件**: `packages/server/src/db/schema/shared.ts`

这些 TypeScript 接口对应 Docker Swarm API 的配置结构，以 JSON 形式存储在数据库中：

```typescript
interface HealthCheckSwarm {
    Test?: string[];      // 检查命令
    Interval?: number;    // 间隔（纳秒）
    Timeout?: number;     // 超时
    StartPeriod?: number; // 启动等待
    Retries?: number;     // 重试次数
}

interface RestartPolicySwarm {
    Condition?: string;   // none/on-failure/any
    Delay?: number;       // 重启延迟
    MaxAttempts?: number; // 最大重试
    Window?: number;      // 观察窗口
}

interface PlacementSwarm {
    Constraints?: string[];     // 放置约束
    Preferences?: [{Spread: {SpreadDescriptor: string}}];
    MaxReplicas?: number;
    Platforms?: [{Architecture: string, OS: string}];
}

interface UpdateConfigSwarm {
    Parallelism: number;      // 并行更新数
    Delay?: number;
    FailureAction?: string;   // pause/continue/rollback
    Monitor?: number;
    MaxFailureRatio?: number;
    Order: string;            // stop-first/start-first
}

interface ServiceModeSwarm {
    Replicated?: { Replicas?: number };
    Global?: {};
    ReplicatedJob?: { MaxConcurrent?: number; TotalCompletions?: number };
    GlobalJob?: {};
}

interface NetworkSwarm { Target?, Aliases?, DriverOpts? }
interface LabelsSwarm { [name: string]: string }
interface EndpointSpecSwarm { Mode?, Ports?: EndpointPortConfigSwarm[] }
interface UlimitSwarm { Name, Soft, Hard }
```

每个接口都有对应的 Zod Schema 用于 API 验证。

## 9. 实体关系概览

```
organization ──1:N──> project ──1:N──> environment ──1:N──┬──> application
      │                                                    ├──> compose
      │                                                    ├──> mysql
      │                                                    ├──> postgres
      │                                                    ├──> redis
      │                                                    ├──> mariadb
      │                                                    └──> mongo
      │
      ├──1:N──> server ──1:N──> deployment
      ├──1:N──> member (权限)
      ├──1:N──> notification
      ├──1:N──> ssh_key
      ├──1:N──> registry
      └──1:N──> destination

application ──1:N──> domain
            ──1:N──> mount
            ──1:N──> port
            ──1:N──> redirect
            ──1:N──> security
            ──1:N──> deployment
            ──1:N──> preview_deployment
            ──N:1──> github/gitlab/gitea/bitbucket
            ──N:1──> server (deploy + build)
            ──N:1──> registry (main + build + rollback)

backup ──N:1──> destination
       ──N:1──> mysql/postgres/mariadb/mongo/compose
       ──1:N──> deployment
```

## 10. Zod 验证 Schema 命名规范

每个表对应的 API 验证 Schema 遵循以下命名模式：

- `apiCreate{Entity}` — 创建验证
- `apiFind{Entity}` / `apiFindOne{Entity}` — 查找验证
- `apiUpdate{Entity}` — 更新验证（通常是 partial）
- `apiRemove{Entity}` / `apiDelete{Entity}` — 删除验证
- `apiDeploy{Entity}` — 部署验证
- `apiSave{Feature}` — 特定功能保存验证

**源文件**: 每个 schema 文件底部，以及 `packages/server/src/db/validations/` 目录

## 11. 源文件清单

| 文件 | 主要表 |
|------|--------|
| `packages/server/src/db/schema/user.ts` | user |
| `packages/server/src/db/schema/account.ts` | account, verification, organization, member, invitation, twoFactor, apikey |
| `packages/server/src/db/schema/session.ts` | session |
| `packages/server/src/db/schema/project.ts` | project |
| `packages/server/src/db/schema/environment.ts` | environment |
| `packages/server/src/db/schema/application.ts` | application + 枚举 sourceType, buildType |
| `packages/server/src/db/schema/compose.ts` | compose + 枚举 sourceTypeCompose, composeType |
| `packages/server/src/db/schema/deployment.ts` | deployment + 枚举 deploymentStatus |
| `packages/server/src/db/schema/domain.ts` | domain + 枚举 domainType |
| `packages/server/src/db/schema/server.ts` | server + 枚举 serverStatus, serverType |
| `packages/server/src/db/schema/mysql.ts` | mysql |
| `packages/server/src/db/schema/postgres.ts` | postgres |
| `packages/server/src/db/schema/redis.ts` | redis |
| `packages/server/src/db/schema/mariadb.ts` | mariadb |
| `packages/server/src/db/schema/mongo.ts` | mongo |
| `packages/server/src/db/schema/mount.ts` | mount + 枚举 mountType, serviceType |
| `packages/server/src/db/schema/port.ts` | port |
| `packages/server/src/db/schema/redirects.ts` | redirect |
| `packages/server/src/db/schema/security.ts` | security |
| `packages/server/src/db/schema/certificate.ts` | certificate |
| `packages/server/src/db/schema/ssh-key.ts` | ssh_key |
| `packages/server/src/db/schema/registry.ts` | registry |
| `packages/server/src/db/schema/destination.ts` | destination |
| `packages/server/src/db/schema/notification.ts` | notification + 11个渠道子表 |
| `packages/server/src/db/schema/backups.ts` | backup + 枚举 databaseType, backupType |
| `packages/server/src/db/schema/volume-backups.ts` | volume_backup |
| `packages/server/src/db/schema/rollbacks.ts` | rollback |
| `packages/server/src/db/schema/schedule.ts` | schedule + 枚举 scheduleType, shellType |
| `packages/server/src/db/schema/preview-deployments.ts` | preview_deployment |
| `packages/server/src/db/schema/patch.ts` | patch |
| `packages/server/src/db/schema/git-provider.ts` | git_provider |
| `packages/server/src/db/schema/github.ts` | github |
| `packages/server/src/db/schema/gitlab.ts` | gitlab |
| `packages/server/src/db/schema/gitea.ts` | gitea |
| `packages/server/src/db/schema/bitbucket.ts` | bitbucket |
| `packages/server/src/db/schema/sso.ts` | sso_provider |
| `packages/server/src/db/schema/web-server-settings.ts` | web_server_settings |
| `packages/server/src/db/schema/ai.ts` | ai |
| `packages/server/src/db/schema/shared.ts` | 枚举 + Swarm 接口 + Zod Schema |
| `packages/server/src/db/schema/utils.ts` | appName 生成工具 |
| `packages/server/src/db/schema/dbml.ts` | DBML Schema 生成脚本（drizzle-dbml-generator） |
| `packages/server/src/db/schema/index.ts` | 导出汇总 |
| `packages/server/src/db/index.ts` | 数据库连接 |
| `packages/server/src/db/constants.ts` | 数据库 URL |

## 12. Go 重写注意事项

- **ORM 选择**: 可用 GORM、Ent、sqlc 或 sqlx。数据库 Schema 需要完全兼容（同一个 PostgreSQL 实例）
- **迁移兼容**: Go 版本的迁移必须与现有 Drizzle 迁移兼容，不能破坏已有数据
- **nanoid**: Go 中使用 `github.com/matoous/go-nanoid/v2`
- **appName 生成**: 需要保持相同的格式（`{type}-{random-words}-{id}`）
- **JSON 字段**: Swarm 配置存储为 PostgreSQL JSON/JSONB，Go 中用 struct + `json` tag
- **枚举**: PostgreSQL pgEnum 在 Go 中用自定义 string 类型 + 常量
- **验证**: Zod 验证在 Go 中用 struct tag 验证（如 `go-playground/validator`）
- **关系加载**: Drizzle 的 `with` 嵌套查询在 Go 中需要手动 JOIN 或使用 ORM 的 Preload/Eager Loading
