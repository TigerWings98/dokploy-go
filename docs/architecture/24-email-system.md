# 邮件系统

## 1. 模块概述

Dokploy 的邮件系统承担两大职责：

1. **认证邮件** -- 邮箱验证、密码重置、组织邀请等与用户身份相关的邮件，由 Better Auth 框架的钩子触发，通过环境变量配置的全局 SMTP 服务发送。
2. **通知邮件** -- 构建成功/失败、数据库备份、卷备份、Docker 清理、服务重启等运维事件通知，通过数据库中 `notification` + `email`/`resend` 表配置的渠道发送。

两条路径使用不同的配置源（环境变量 vs 数据库），但底层都依赖 Nodemailer（SMTP）或 Resend SDK。邮件模板使用 React Email 编写（JSX/TSX），在发送前通过 `@react-email/components` 的 `renderAsync()` 渲染为 HTML 字符串。

在系统架构中的位置：
```
Better Auth (认证) ──> sendEmail() ──> Nodemailer (SMTP)
                                       ^ 环境变量配置

通知事件触发 ──> sendBuildSuccessNotifications() 等
                 |──> renderAsync(Template) -> sendEmailNotification() -> Nodemailer (SMTP)  <- DB notification.email
                 |──> renderAsync(Template) -> sendResendNotification() -> Resend SDK       <- DB notification.resend
```

## 2. 设计详解

### 2.1 认证邮件

#### 2.1.1 发送函数

**源文件**: `packages/server/src/verification/send-verification-email.tsx`

```typescript
export const sendEmail = async ({ email, subject, text }: {
    email: string;
    subject: string;
    text: string;
}) => {
    await sendEmailNotification(
        {
            fromAddress: process.env.SMTP_FROM_ADDRESS || "",
            toAddresses: [email],
            smtpServer: process.env.SMTP_SERVER || "",
            smtpPort: Number(process.env.SMTP_PORT),
            username: process.env.SMTP_USERNAME || "",
            password: process.env.SMTP_PASSWORD || "",
        },
        subject,
        text,
    );
};
```

SMTP 配置完全通过环境变量注入，不存储在数据库中。该函数是认证邮件的唯一出口。

#### 2.1.2 Better Auth 钩子调用

在 `packages/server/src/lib/auth.ts` 中，Better Auth 配置了以下邮件钩子：

| 钩子 | 触发条件 | 邮件内容 |
|------|---------|---------|
| `emailVerification.sendVerificationEmail` | 用户注册（云模式下发送） | 包含验证链接的 HTML 邮件 |
| `emailAndPassword.sendResetPassword` | 用户请求密码重置 | 包含重置链接的 HTML 邮件 |
| `organization.sendInvitationEmail` | 管理员邀请用户加入组织 | 包含邀请链接的 HTML 邮件 |

#### 2.1.3 邮箱验证流程

```
1. 用户注册 -> Better Auth 创建 user 记录
2. [云模式] sendVerificationEmail 钩子触发
3. 生成验证 URL（包含 token），写入 verification 表
4. 调用 sendEmail() 发送验证邮件
5. 用户点击邮件中的链接
6. Better Auth 验证 token，更新 user.emailVerified = true
7. autoSignInAfterVerification: true -> 自动创建 session 并登录
```

自托管模式下 `emailVerification` 虽然配置了 `sendOnSignUp: true`，但由于 SMTP 环境变量通常未配置，验证邮件不会发送。注册后自动登录，不要求邮箱验证。

#### 2.1.4 密码重置流程

```
1. 用户在登录页点击"忘记密码"
2. Better Auth 生成 resetPasswordToken，写入 account 表
3. sendResetPassword 钩子触发 -> sendEmail() 发送重置邮件
4. 用户点击邮件中的重置链接（包含 token）
5. 前端展示新密码表单
6. Better Auth 验证 token 有效性和过期时间
7. 更新 account.password（bcrypt 哈希）
```

#### 2.1.5 组织邀请流程

```
1. 管理员在面板中邀请用户（输入邮箱）
2. Better Auth 创建 invitation 记录（含 expiresAt）
3. sendInvitationEmail 钩子触发 -> sendEmail() 发送邀请邮件
4. 被邀请人点击邮件中的链接
5. 前端调用 getUserByToken() 验证令牌
6. 如果用户已存在 -> 直接加入组织
7. 如果用户不存在 -> 显示注册表单 -> 注册后加入组织
```

#### 2.1.6 Discord 通知（云模式附加）

```typescript
export const sendDiscordNotificationWelcome = async (email: string) => {
    await sendDiscordNotification(
        { webhookUrl: process.env.DISCORD_WEBHOOK_URL || "" },
        { title: "New User Registered", color: 0x00ff00, fields: [{ name: "Email", value: email }], ... }
    );
};
```

云模式下，新用户注册时会向 Discord Webhook 发送通知，用于运营监控。

#### 2.1.7 SMTP 环境变量

| 环境变量 | 说明 |
|---------|------|
| `SMTP_SERVER` | SMTP 服务器地址 |
| `SMTP_PORT` | SMTP 端口 |
| `SMTP_USERNAME` | SMTP 用户名 |
| `SMTP_PASSWORD` | SMTP 密码 |
| `SMTP_FROM_ADDRESS` | 发件人地址 |

### 2.2 邮件模板

#### 2.2.1 技术栈

- **React Email** v2.1.5 -- 邮件模板开发框架
- **@react-email/components** v0.0.21 -- 邮件 UI 组件库（Body, Button, Container, Heading, Img, Link, Text, Section, Tailwind 等）
- **React** 18 -- JSX 渲染

模板使用 Tailwind CSS（通过 `<Tailwind>` 组件内联样式），确保在各邮件客户端的兼容性。

#### 2.2.2 模板清单

| 模板文件 | 组件名 | 用途 | 模板参数（TemplateProps） |
|---------|-------|------|---------|
| `build-success.tsx` | `BuildSuccessEmail` | 构建成功通知 | `projectName, applicationName, applicationType, buildLink, date, environmentName` |
| `build-failed.tsx` | `BuildFailedEmail` | 构建失败通知 | `projectName, applicationName, applicationType, errorMessage, buildLink, date` |
| `database-backup.tsx` | `DatabaseBackupEmail` | 数据库备份通知 | `projectName, applicationName, databaseType("postgres"|"mysql"|"mongodb"|"mariadb"), type("error"|"success"), errorMessage?, date` |
| `volume-backup.tsx` | `VolumeBackupEmail` | 卷备份通知 | `projectName, applicationName, volumeName, serviceType("application"|"postgres"|...|"compose"), type("error"|"success"), errorMessage?, backupSize?, date` |
| `docker-cleanup.tsx` | `DockerCleanupEmail` | Docker 清理通知 | `message, date` |
| `dokploy-restart.tsx` | `DokployRestartEmail` | 服务重启通知 | `date` |
| `invitation.tsx` | `InvitationEmail` | 组织邀请 | `inviteLink, toEmail` |

#### 2.2.3 模板视觉结构

所有模板遵循统一的视觉布局：

```
+-----------------------------+
|         Dokploy Logo         |  <- 从 GitHub 加载
+-----------------------------+
|       标题（Heading）         |
+-----------------------------+
|       正文描述                |
+-----------------------------+
|   详情区（灰色背景卡片）       |  <- 项目名、应用名、日期等
+-----------------------------+
|   [错误信息区]（仅失败时）     |
+-----------------------------+
|       [CTA 按钮]             |  <- "View build" / "Join the team"
+-----------------------------+
|       备用链接文本             |
+-----------------------------+
```

核心样式：
- 最大宽度 465px，居中显示
- 圆角边框 `border-[#eaeaea]`
- 品牌色 `#007291`
- CTA 按钮黑色背景白色文字
- Logo 从 `https://raw.githubusercontent.com/Dokploy/dokploy/refs/heads/canary/apps/dokploy/logo.png` 加载

#### 2.2.4 模板代码示例

以 `BuildSuccessEmail` 为例：

```tsx
export type TemplateProps = {
    projectName: string;
    applicationName: string;
    applicationType: string;
    buildLink: string;
    date: string;
    environmentName: string;
};

export const BuildSuccessEmail = ({ projectName, applicationName, ... }: TemplateProps) => {
    return (
        <Html>
            <Head />
            <Preview>{`Build success for ${applicationName}`}</Preview>
            <Tailwind config={{ theme: { extend: { colors: { brand: "#007291" } } } }}>
                <Body>
                    <Container>
                        <Img src="...logo.png" />
                        <Heading>Build success for <strong>{applicationName}</strong></Heading>
                        <Section className="bg-[#F4F4F5] rounded-lg p-2">
                            {/* 项目名、应用名、环境、类型、日期 */}
                        </Section>
                        <Button href={buildLink}>View build</Button>
                    </Container>
                </Body>
            </Tailwind>
        </Html>
    );
};
```

#### 2.2.5 其他模板文件（React Email 示例，非业务使用）

- `vercel-invite-user.tsx` -- Vercel 邀请示例
- `stripe-welcome.tsx` -- Stripe 欢迎示例
- `notion-magic-link.tsx` -- Notion Magic Link 示例
- `plaid-verify-identity.tsx` -- Plaid 身份验证示例

### 2.3 通知邮件发送

#### 2.3.1 SMTP 发送（Nodemailer）

**源文件**: `packages/server/src/utils/notifications/utils.ts`

```typescript
export const sendEmailNotification = async (
    connection: typeof email.$inferInsert,
    subject: string,
    htmlContent: string,
) => {
    const transporter = nodemailer.createTransport({
        host: connection.smtpServer,
        port: connection.smtpPort,
        auth: { user: connection.username, pass: connection.password },
    });
    await transporter.sendMail({
        from: connection.fromAddress,
        to: connection.toAddresses.join(", "),
        subject,
        html: htmlContent,
        textEncoding: "base64",
    });
};
```

每次发送创建新的 transporter（无连接池），SMTP 配置来自数据库 `email` 表。

#### 2.3.2 Resend 发送

```typescript
export const sendResendNotification = async (
    connection: typeof resend.$inferInsert,
    subject: string,
    htmlContent: string,
) => {
    const client = new Resend(connection.apiKey);
    const result = await client.emails.send({
        from: connection.fromAddress,
        to: connection.toAddresses,
        subject,
        html: htmlContent,
    });
    if (result.error) throw new Error(result.error.message);
};
```

#### 2.3.3 通知事件发送流程

以构建成功为例（`packages/server/src/utils/notifications/build-success.ts`）：

```
sendBuildSuccessNotifications({ projectName, applicationName, ..., organizationId })
|-- 1. 查询 notifications 表
|   WHERE appDeploy = true AND organizationId = ?
|   WITH: email, discord, telegram, slack, resend, gotify, ntfy, custom, lark, pushover, teams
|-- 2. 遍历每条通知配置
|   |-- if (email || resend):
|   |     renderAsync(BuildSuccessEmail({ ... })) -> HTML 字符串
|   |     if (email) -> sendEmailNotification(email, subject, html)
|   |     if (resend) -> sendResendNotification(resend, subject, html)
|   |-- if (discord) -> sendDiscordNotification(discord, embed)
|   |-- if (telegram) -> sendTelegramNotification(telegram, htmlText, inlineButtons)
|   |-- if (slack) -> sendSlackNotification(slack, attachments)
|   |-- if (gotify) -> sendGotifyNotification(gotify, title, text)
|   |-- if (ntfy) -> sendNtfyNotification(ntfy, title, tags, actions, text)
|   |-- if (custom) -> sendCustomNotification(custom, jsonPayload)
|   |-- if (lark) -> sendLarkNotification(lark, interactiveCard)
|   |-- if (pushover) -> sendPushoverNotification(pushover, title, text)
|   |-- if (teams) -> sendTeamsNotification(teams, adaptiveCard)
|-- 3. 每个通知独立 try-catch，单个失败不影响其他
```

#### 2.3.4 通知事件类型

| 事件 | 触发函数 | notification 表字段 | 邮件模板 |
|------|---------|-------------------|---------|
| 构建成功 | `sendBuildSuccessNotifications()` | `appDeploy` | `BuildSuccessEmail` |
| 构建失败 | `sendBuildErrorNotifications()` | `appBuildError` | `BuildFailedEmail` |
| 数据库备份 | `sendDatabaseBackupNotifications()` | `databaseBackup` | `DatabaseBackupEmail` |
| 卷备份 | `sendVolumeBackupNotifications()` | `volumeBackup` | `VolumeBackupEmail` |
| Docker 清理 | `sendDockerCleanupNotifications()` | `dockerCleanup` | `DockerCleanupEmail` |
| 服务重启 | `sendDokployRestartNotifications()` | `dokployRestart` | `DokployRestartEmail` |
| 服务器阈值告警 | `sendServerThresholdNotifications()` | `serverThreshold` | 无邮件模板（仅其他渠道） |

### 2.4 数据库表

#### 2.4.1 notification 主表

记录通知规则，每条记录关联一个具体渠道：

| 字段 | 类型 | 说明 |
|------|------|------|
| notificationId | text PK | 主键 |
| name | text | 通知名称 |
| appDeploy | boolean | 订阅应用部署事件 |
| appBuildError | boolean | 订阅构建失败事件 |
| databaseBackup | boolean | 订阅数据库备份事件 |
| volumeBackup | boolean | 订阅卷备份事件 |
| dokployRestart | boolean | 订阅重启事件 |
| dockerCleanup | boolean | 订阅清理事件 |
| serverThreshold | boolean | 订阅服务器阈值事件 |
| notificationType | enum | 渠道类型 |
| organizationId | text FK | 所属组织 |
| emailId / slackId / ... | text FK | 各渠道配置外键 |

#### 2.4.2 email 子表

| 字段 | 类型 | 说明 |
|------|------|------|
| emailId | text PK | 主键 |
| smtpServer | text | SMTP 服务器 |
| smtpPort | integer | SMTP 端口 |
| username | text | 用户名 |
| password | text | 密码 |
| fromAddress | text | 发件人地址 |
| toAddresses | text[] | 收件人地址列表 |

#### 2.4.3 resend 子表

| 字段 | 类型 | 说明 |
|------|------|------|
| resendId | text PK | 主键 |
| apiKey | text | Resend API Key |
| fromAddress | text | 发件人地址 |
| toAddresses | text[] | 收件人列表 |

### 2.5 通知 CRUD 服务

**源文件**: `packages/server/src/services/notification.ts`

邮件渠道的 CRUD 使用数据库事务保证一致性：

```typescript
export const createEmailNotification = async (input, organizationId) => {
    await db.transaction(async (tx) => {
        // 1. 插入 email 表
        const newEmail = await tx.insert(email).values({
            smtpServer, smtpPort, username, password, fromAddress, toAddresses
        }).returning();
        // 2. 插入 notifications 主表（关联 emailId + 事件订阅开关）
        await tx.insert(notifications).values({
            emailId: newEmail.emailId,
            name, appDeploy, appBuildError, ...,
            notificationType: "email",
            organizationId,
        }).returning();
    });
};
```

update 方法同样在事务中同时更新主表和子表。

## 3. 源文件清单

| 文件 | 说明 |
|------|------|
| `packages/server/src/verification/send-verification-email.tsx` | 认证邮件发送函数 + Discord 注册通知 |
| `packages/server/src/lib/auth.ts` | Better Auth 配置（调用 sendEmail 的钩子） |
| `packages/server/src/emails/emails/build-success.tsx` | 构建成功邮件模板 |
| `packages/server/src/emails/emails/build-failed.tsx` | 构建失败邮件模板 |
| `packages/server/src/emails/emails/database-backup.tsx` | 数据库备份邮件模板 |
| `packages/server/src/emails/emails/volume-backup.tsx` | 卷备份邮件模板 |
| `packages/server/src/emails/emails/docker-cleanup.tsx` | Docker 清理邮件模板 |
| `packages/server/src/emails/emails/dokploy-restart.tsx` | 服务重启邮件模板 |
| `packages/server/src/emails/emails/invitation.tsx` | 组织邀请邮件模板 |
| `packages/server/src/emails/package.json` | 邮件模板依赖配置 |
| `packages/server/src/utils/notifications/utils.ts` | 所有通知渠道底层发送函数（含 sendEmailNotification, sendResendNotification） |
| `packages/server/src/utils/notifications/build-success.ts` | 构建成功通知编排 |
| `packages/server/src/utils/notifications/build-error.ts` | 构建失败通知编排 |
| `packages/server/src/utils/notifications/database-backup.ts` | 数据库备份通知编排 |
| `packages/server/src/utils/notifications/volume-backup.ts` | 卷备份通知编排 |
| `packages/server/src/utils/notifications/docker-cleanup.ts` | Docker 清理通知编排 |
| `packages/server/src/utils/notifications/dokploy-restart.ts` | 服务重启通知编排 |
| `packages/server/src/utils/notifications/server-threshold.ts` | 服务器阈值通知编排 |
| `packages/server/src/services/notification.ts` | 通知渠道 CRUD 服务（含 createEmailNotification 等） |
| `packages/server/src/services/admin.ts` | getDokployUrl()（构建邮件链接用） |
| `packages/server/src/db/schema/notification.ts` | notification 及子表 Schema 定义 |
| `apps/dokploy/server/api/routers/notification.ts` | 通知 tRPC 路由 |

## 4. 对外接口

### 4.1 认证邮件接口

```typescript
// packages/server/src/verification/send-verification-email.tsx
sendEmail({ email: string, subject: string, text: string }): Promise<boolean>
sendDiscordNotificationWelcome(email: string): Promise<void>
```

### 4.2 通知邮件发送接口

```typescript
// packages/server/src/utils/notifications/utils.ts
sendEmailNotification(connection: EmailConfig, subject: string, htmlContent: string): Promise<void>
sendResendNotification(connection: ResendConfig, subject: string, htmlContent: string): Promise<void>
```

### 4.3 通知事件编排接口

```typescript
// packages/server/src/utils/notifications/build-success.ts
sendBuildSuccessNotifications({
    projectName, applicationName, applicationType, buildLink,
    organizationId, domains, environmentName
}): Promise<void>

// packages/server/src/utils/notifications/build-error.ts
sendBuildErrorNotifications({
    projectName, applicationName, applicationType, errorMessage,
    buildLink, organizationId
}): Promise<void>

// packages/server/src/utils/notifications/database-backup.ts
sendDatabaseBackupNotifications({
    projectName, applicationName, databaseType, type, errorMessage?,
    organizationId, databaseName
}): Promise<void>

// packages/server/src/utils/notifications/volume-backup.ts
sendVolumeBackupNotifications({ ... }): Promise<void>

// packages/server/src/utils/notifications/docker-cleanup.ts
sendDockerCleanupNotifications(organizationId, message?): Promise<void>

// packages/server/src/utils/notifications/dokploy-restart.ts
sendDokployRestartNotifications(): Promise<void>
```

### 4.4 通知 CRUD 服务接口

```typescript
// packages/server/src/services/notification.ts
createEmailNotification(input: z.infer<typeof apiCreateEmail>, organizationId: string): Promise<void>
updateEmailNotification(input: z.infer<typeof apiUpdateEmail>): Promise<void>
createResendNotification(input: z.infer<typeof apiCreateResend>, organizationId: string): Promise<void>
updateResendNotification(input: z.infer<typeof apiUpdateResend>): Promise<void>
findNotificationById(notificationId: string): Promise<Notification>
removeNotificationById(notificationId: string): Promise<Notification>
```

### 4.5 tRPC 路由接口

```typescript
// apps/dokploy/server/api/routers/notification.ts
notificationRouter = {
    createEmail:       adminProcedure.input(apiCreateEmail).mutation()
    updateEmail:       adminProcedure.input(apiUpdateEmail).mutation()
    testEmailConnection: adminProcedure.input(apiTestEmailConnection).mutation()
    createResend:      adminProcedure.input(apiCreateResend).mutation()
    updateResend:      adminProcedure.input(apiUpdateResend).mutation()
    testResendConnection: adminProcedure.input(apiTestResendConnection).mutation()
    remove:            adminProcedure.input(apiFindOneNotification).mutation()
    one:               protectedProcedure.input(apiFindOneNotification).query()
    all:               adminProcedure.query()
    getEmailProviders: adminProcedure.query()
    // ... 其他渠道的 create/update/test
}
```

## 5. 依赖关系

### 上游依赖

```
邮件系统依赖:
|-- nodemailer              <- SMTP 发送
|-- resend                  <- Resend API 发送
|-- @react-email/components <- 模板渲染 (renderAsync)
|-- react                   <- JSX 支持
|-- better-auth             <- 认证邮件触发钩子
|-- drizzle-orm             <- 查询 notification 配置
|-- date-fns                <- 日期格式化
|-- zod                     <- 输入验证 Schema
```

### 下游被依赖

```
被以下模块调用:
|-- 部署流程 (build-success / build-error)
|-- 备份流程 (database-backup / volume-backup)
|-- 定时清理 (docker-cleanup)
|-- 启动序列 (dokploy-restart)
|-- 监控服务 (server-threshold, 由 Go 监控回调触发)
|-- 认证系统 (Better Auth 钩子)
|-- tRPC 路由层 (通知管理 CRUD)
```

## 6. Go 重写注意事项

### 需要重写的部分

- **SMTP 发送**: 使用 Go 标准库 `net/smtp` 或 `github.com/wneessen/go-mail` 替代 Nodemailer
- **Resend 集成**: 使用 `github.com/resend/resend-go/v2` 替代 Resend JS SDK
- **邮件模板**: React Email (JSX) 无法在 Go 中使用，需要改为 Go `html/template` 模板引擎
- **通知编排**: 查询 notification 表并分发到各渠道的逻辑需要完全重写
- **认证邮件**: Better Auth 的邮件钩子需要在 Go 认证框架中重新实现

### 语言无关可复用的部分

以下内容可直接复用，不受语言限制：

| 可复用项 | 说明 |
|---------|------|
| 邮件 HTML 结构 | 可将 React Email 模板预编译为静态 HTML 模板文件，Go 端做变量替换 |
| 通知渠道 HTTP API | Discord Webhook、Telegram Bot API、Slack Webhook 等的请求格式和端点地址完全相同 |
| 数据库表结构 | notification 及子表（email、resend 等）Schema 不变 |
| SMTP 协议 | SMTP 是标准协议，配置参数（host/port/auth）完全相同 |
| Resend API | REST API 端点和请求格式语言无关 |

### 推荐方案

1. **SMTP 库**: 使用 `github.com/wneessen/go-mail` -- 功能完善、活跃维护的 Go SMTP 客户端
2. **模板引擎**: 使用 Go 内置 `html/template`，将 7 个邮件模板转换为 `.html` 模板文件
3. **模板变量**: 每种模板定义对应的 Go struct（如 `BuildSuccessData`），通过 `template.Execute()` 渲染
4. **通知抽象**: 建议定义 `EmailSender` 接口，支持 SMTP 和 Resend 两种实现：

```go
type EmailSender interface {
    Send(ctx context.Context, to []string, subject string, htmlBody string) error
}

type SMTPSender struct { /* host, port, username, password, fromAddress */ }
type ResendSender struct { /* apiKey, fromAddress */ }
```

5. **通知渠道包**: 将所有通知渠道发送函数提取为独立的 `pkg/notify` 包，每个渠道一个文件
6. **并发发送**: 当前 TypeScript 实现是串行发送，Go 中可使用 goroutine + errgroup 并发发送多个渠道
