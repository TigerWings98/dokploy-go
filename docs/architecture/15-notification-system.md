# 15. 通知系统

## 1. 模块概述

通知系统负责在 Dokploy 平台关键事件发生时，向用户配置的多个通知渠道发送格式化消息。系统采用多渠道、多事件类型的架构设计，支持 11 种通知渠道和 7 种事件类型。每个组织（Organization）可以独立配置多个通知目标，并为每个目标选择订阅的事件类型。

**在系统中的角色：** 通知系统是一个横切关注点（cross-cutting concern），被构建系统、备份系统、Docker 清理、服务器监控等多个模块调用，是用户感知平台运行状态的核心渠道。

## 2. 设计详解

### 2.1 整体架构

通知系统由三层组成：

1. **数据层（Schema）**：定义通知配置的数据库表结构，包括通知主表 `notifications` 和各渠道子表（`slack`、`telegram`、`discord` 等）
2. **服务层（Service）**：提供通知配置的 CRUD 操作，使用数据库事务确保主表和子表的一致性
3. **发送层（Utils）**：实现各渠道的消息发送逻辑和各事件类型的消息格式化

### 2.2 数据结构

#### 通知主表 `notifications`

```typescript
export const notifications = pgTable("notification", {
  notificationId: text("notificationId").notNull().primaryKey(),
  name: text("name").notNull(),
  // 事件订阅开关
  appDeploy: boolean("appDeploy").notNull().default(false),
  appBuildError: boolean("appBuildError").notNull().default(false),
  databaseBackup: boolean("databaseBackup").notNull().default(false),
  volumeBackup: boolean("volumeBackup").notNull().default(false),
  dokployRestart: boolean("dokployRestart").notNull().default(false),
  dockerCleanup: boolean("dockerCleanup").notNull().default(false),
  serverThreshold: boolean("serverThreshold").notNull().default(false),
  // 渠道类型枚举
  notificationType: notificationType("notificationType").notNull(),
  // 各渠道外键（一对一关系，仅一个非空）
  slackId: text("slackId").references(() => slack.slackId),
  telegramId: text("telegramId").references(() => telegram.telegramId),
  discordId: text("discordId").references(() => discord.discordId),
  emailId: text("emailId").references(() => email.emailId),
  // ... 其他渠道外键（resendId, gotifyId, ntfyId, customId, larkId, pushoverId, teamsId）
  organizationId: text("organizationId").notNull(),
});
```

#### 通知渠道类型枚举

```typescript
export const notificationType = pgEnum("notificationType", [
  "slack", "telegram", "discord", "email", "resend",
  "gotify", "ntfy", "pushover", "custom", "lark", "teams",
]);
```

#### 各渠道子表字段

| 渠道 | 关键字段 | 发送方式 |
|------|---------|---------|
| Slack | `webhookUrl`, `channel` | Webhook POST（attachments 格式） |
| Discord | `webhookUrl`, `decoration` | Webhook POST（embeds 格式） |
| Telegram | `botToken`, `chatId`, `messageThreadId` | Bot API `sendMessage` |
| Email | `smtpServer`, `smtpPort`, `username`, `password`, `fromAddress`, `toAddresses[]` | SMTP (nodemailer) |
| Resend | `apiKey`, `fromAddress`, `toAddresses[]` | Resend API |
| Gotify | `serverUrl`, `appToken`, `priority`, `decoration` | REST API POST |
| Ntfy | `serverUrl`, `topic`, `accessToken`, `priority` | REST API POST（Header 传参） |
| Custom | `endpoint`, `headers` (jsonb) | HTTP POST (JSON payload) |
| Lark | `webhookUrl` | Webhook POST（Interactive Card） |
| Teams | `webhookUrl` | Webhook POST（Adaptive Card） |
| Pushover | `userKey`, `apiToken`, `priority`, `retry`, `expire` | REST API POST (FormData) |

### 2.3 事件类型与通知流程

支持的 7 种事件类型：

| 事件标志字段 | 触发函数 | 触发场景 |
|-------------|---------|---------|
| `appDeploy` | `sendBuildSuccessNotifications` | 应用构建部署成功 |
| `appBuildError` | `sendBuildErrorNotifications` | 应用构建失败 |
| `databaseBackup` | `sendDatabaseBackupNotifications` | 数据库备份成功/失败 |
| `volumeBackup` | `sendVolumeBackupNotifications` | 卷备份成功/失败 |
| `dokployRestart` | `sendDokployRestartNotifications` | Dokploy 服务重启 |
| `dockerCleanup` | `sendDockerCleanupNotifications` | Docker 资源清理完成 |
| `serverThreshold` | `sendServerThresholdNotifications` | 服务器 CPU/内存超阈值 |

#### 通知发送流程

每个事件通知函数遵循统一模式：

```
1. 从数据库查询所有订阅了该事件类型的 notification 记录（按 organizationId 过滤）
2. 使用 `with` 关联查询加载所有渠道子表数据
3. 遍历 notificationList，对每个 notification：
   a. 检查各渠道子对象是否存在（如 email、discord、slack 等）
   b. 如存在，构造该渠道特定格式的消息
   c. 调用对应的 send* 函数发送
4. 单个渠道发送失败不影响其他渠道（try-catch 包裹）
```

以 `sendBuildSuccessNotifications` 为例，查询条件为 `notifications.appDeploy === true && notifications.organizationId === organizationId`，然后遍历每个 notification 记录，按渠道分别格式化消息并发送。

### 2.4 消息格式化策略

每种渠道有独特的消息格式：

- **Discord**：使用 Embed 格式，支持 `decoration` 开关控制是否显示 emoji 装饰符，字段使用 `inline` 布局，带颜色条（成功绿色 `0x57f287`，失败红色 `0xed4245`），错误消息截断至 800 字符
- **Slack**：使用 Attachments 格式，带颜色条（成功 `#00FF00`，失败 `#FF0000`）和字段列表，支持 Action Button
- **Telegram**：使用 HTML 格式（`parse_mode: "HTML"`），支持 `inline_keyboard` 按钮，构建成功时展示域名链接按钮（2 列布局分块）
- **Email/Resend**：使用 React Email 模板渲染 HTML，通过 `renderAsync` 异步渲染，如 `BuildSuccessEmail`、`BuildFailedEmail`、`DockerCleanupEmail` 等
- **Lark**：使用 Interactive Card (schema 2.0)，支持 `column_set` 两列布局，带 header 模板颜色
- **Teams**：使用 Adaptive Card (v1.4)，包含 `FactSet` 事实列表和 `Action.OpenUrl` 操作按钮
- **Gotify**：纯文本格式，通过 `X-Gotify-Key` header 认证，支持 `decoration` 开关和 `priority` 设置
- **Ntfy**：纯文本 body，通过 HTTP header 传递标题（`X-Title`）、标签（`X-Tags`）、优先级（`X-Priority`）和操作（`X-Actions`）
- **Pushover**：使用 `application/x-www-form-urlencoded` 表单提交，紧急优先级（2）需要 `retry` 和 `expire` 参数
- **Custom**：发送结构化 JSON payload，包含所有事件上下文数据（title、message、status、type 等）

### 2.5 服务层 CRUD 模式

每种渠道的 create/update 操作使用数据库事务：

```typescript
// 创建模式（以 Slack 为例）
export const createSlackNotification = async (input, organizationId) => {
  await db.transaction(async (tx) => {
    // 1. 插入渠道子表
    const newSlack = await tx.insert(slack).values({
      channel: input.channel,
      webhookUrl: input.webhookUrl,
    }).returning();
    // 2. 插入 notifications 主表，关联子表 ID
    const newDestination = await tx.insert(notifications).values({
      slackId: newSlack.slackId,
      name: input.name,
      appDeploy: input.appDeploy,
      appBuildError: input.appBuildError,
      databaseBackup: input.databaseBackup,
      volumeBackup: input.volumeBackup,
      dokployRestart: input.dokployRestart,
      dockerCleanup: input.dockerCleanup,
      serverThreshold: input.serverThreshold,
      notificationType: "slack",
      organizationId: organizationId,
    }).returning();
    return newDestination;
  });
};
```

更新操作同样使用事务，分别更新主表和子表。删除操作通过外键级联（`onDelete: "cascade"`）自动清理子表。

通用操作函数包括：

- `findNotificationById` — 按 ID 查询，`with` 加载所有渠道子表
- `removeNotificationById` — 按 ID 删除（级联清理子表）
- `updateNotificationById` — 通用更新（仅更新主表字段）

## 3. 源文件清单

### 数据库 Schema
- `dokploy/packages/server/src/db/schema/notification.ts` — 通知主表、所有渠道子表定义（`slack`/`telegram`/`discord`/`email`/`resend`/`gotify`/`ntfy`/`custom`/`lark`/`pushover`/`teams`）、关系映射、所有 API Schema（apiCreate*/apiUpdate*/apiTest* 各 11 组）

### 服务层（CRUD）
- `dokploy/packages/server/src/services/notification.ts` — 所有渠道的 create/update 函数（22 个）、findNotificationById、removeNotificationById、updateNotificationById

### 通知发送工具
- `dokploy/packages/server/src/utils/notifications/utils.ts` — 11 种渠道的底层发送函数：`sendEmailNotification`、`sendResendNotification`、`sendDiscordNotification`、`sendTelegramNotification`、`sendSlackNotification`、`sendGotifyNotification`、`sendNtfyNotification`、`sendCustomNotification`、`sendLarkNotification`、`sendTeamsNotification`、`sendPushoverNotification`
- `dokploy/packages/server/src/utils/notifications/build-success.ts` — `sendBuildSuccessNotifications`
- `dokploy/packages/server/src/utils/notifications/build-error.ts` — `sendBuildErrorNotifications`
- `dokploy/packages/server/src/utils/notifications/docker-cleanup.ts` — `sendDockerCleanupNotifications`
- `dokploy/packages/server/src/utils/notifications/dokploy-restart.ts` — `sendDokployRestartNotifications`
- `dokploy/packages/server/src/utils/notifications/database-backup.ts` — `sendDatabaseBackupNotifications`
- `dokploy/packages/server/src/utils/notifications/volume-backup.ts` — `sendVolumeBackupNotifications`
- `dokploy/packages/server/src/utils/notifications/server-threshold.ts` — `sendServerThresholdNotifications`

## 4. 对外接口

### 底层发送函数（utils.ts）

```typescript
sendEmailNotification(connection: EmailConfig, subject: string, htmlContent: string): Promise<void>
sendResendNotification(connection: ResendConfig, subject: string, htmlContent: string): Promise<void>
sendDiscordNotification(connection: DiscordConfig, embed: object): Promise<void>
sendTelegramNotification(connection: TelegramConfig, messageText: string, inlineButton?: {text: string; url: string}[][]): Promise<void>
sendSlackNotification(connection: SlackConfig, message: object): Promise<void>
sendGotifyNotification(connection: GotifyConfig, title: string, message: string): Promise<void>
sendNtfyNotification(connection: NtfyConfig, title: string, tags: string, actions: string, message: string): Promise<void>
sendCustomNotification(connection: CustomConfig, payload: Record<string, any>): Promise<Response>
sendLarkNotification(connection: LarkConfig, message: object): Promise<void>
sendTeamsNotification(connection: TeamsConfig, message: TeamsAdaptiveCardMessage): Promise<void>
sendPushoverNotification(connection: PushoverConfig, title: string, message: string): Promise<void>
```

`TeamsAdaptiveCardMessage` 接口：

```typescript
interface TeamsAdaptiveCardMessage {
  title: string;
  themeColor?: string;
  facts?: { name: string; value: string }[];
  potentialAction?: { type: "Action.OpenUrl"; title: string; url: string };
}
```

### 事件通知函数

```typescript
sendBuildSuccessNotifications(props: {
  projectName: string; applicationName: string; applicationType: string;
  buildLink: string; organizationId: string; domains: Domain[]; environmentName: string;
}): Promise<void>

sendBuildErrorNotifications(props: {
  projectName: string; applicationName: string; applicationType: string;
  errorMessage: string; buildLink: string; organizationId: string;
}): Promise<void>

sendDockerCleanupNotifications(organizationId: string, message?: string): Promise<void>

sendDokployRestartNotifications(): Promise<void>

sendDatabaseBackupNotifications(props: {
  projectName: string; applicationName: string;
  databaseType: "postgres" | "mysql" | "mongodb" | "mariadb";
  type: "error" | "success"; organizationId: string; errorMessage?: string; databaseName: string;
}): Promise<void>

sendVolumeBackupNotifications(props: {
  projectName: string; applicationName: string; volumeName: string;
  serviceType: "application" | "postgres" | "mysql" | "mongodb" | "mariadb" | "redis" | "compose";
  type: "error" | "success"; organizationId: string; errorMessage?: string; backupSize?: string;
}): Promise<void>

sendServerThresholdNotifications(organizationId: string, payload: {
  Type: "CPU" | "Memory"; Value: number; Threshold: number;
  Message: string; Timestamp: string; Token: string; ServerName: string;
}): Promise<void>
```

### 服务层 CRUD 函数

```typescript
// 每种渠道均提供 create + update 函数对（共 11 组 22 个函数）
createSlackNotification(input: ApiCreateSlack, organizationId: string): Promise<void>
updateSlackNotification(input: ApiUpdateSlack): Promise<void>
createTelegramNotification(input: ApiCreateTelegram, organizationId: string): Promise<void>
updateTelegramNotification(input: ApiUpdateTelegram): Promise<void>
createDiscordNotification(input: ApiCreateDiscord, organizationId: string): Promise<void>
updateDiscordNotification(input: ApiUpdateDiscord): Promise<void>
createEmailNotification(input: ApiCreateEmail, organizationId: string): Promise<void>
updateEmailNotification(input: ApiUpdateEmail): Promise<void>
createResendNotification(input: ApiCreateResend, organizationId: string): Promise<void>
updateResendNotification(input: ApiUpdateResend): Promise<void>
createGotifyNotification(input: ApiCreateGotify, organizationId: string): Promise<void>
updateGotifyNotification(input: ApiUpdateGotify): Promise<void>
createNtfyNotification(input: ApiCreateNtfy, organizationId: string): Promise<void>
updateNtfyNotification(input: ApiUpdateNtfy): Promise<void>
createCustomNotification(input: ApiCreateCustom, organizationId: string): Promise<void>
updateCustomNotification(input: ApiUpdateCustom): Promise<void>
createLarkNotification(input: ApiCreateLark, organizationId: string): Promise<void>
updateLarkNotification(input: ApiUpdateLark): Promise<void>
createTeamsNotification(input: ApiCreateTeams, organizationId: string): Promise<void>
updateTeamsNotification(input: ApiUpdateTeams): Promise<void>
createPushoverNotification(input: ApiCreatePushover, organizationId: string): Promise<void>
updatePushoverNotification(input: ApiUpdatePushover): Promise<void>

// 通用操作
findNotificationById(notificationId: string): Promise<NotificationWithAllRelations>
removeNotificationById(notificationId: string): Promise<Notification>
updateNotificationById(notificationId: string, data: Partial<Notification>): Promise<Notification>
```

## 5. 依赖关系

### 上游依赖
- `drizzle-orm` — 数据库 ORM 查询
- `nodemailer` — SMTP 邮件发送
- `resend` — Resend 邮件 API SDK
- `@react-email/components`（`renderAsync`） — React Email 模板渲染
- `date-fns`（`format`） — 日期格式化
- `bcrypt` — 无（通知模块不使用）
- 各外部 API endpoint：
  - Telegram Bot API: `https://api.telegram.org/bot{token}/sendMessage`
  - Discord Webhook: 自定义 `webhookUrl`
  - Slack Webhook: 自定义 `webhookUrl`
  - Gotify: `{serverUrl}/message`
  - Ntfy: `{serverUrl}/{topic}`
  - Pushover: `https://api.pushover.net/1/messages.json`
  - Lark Webhook: 自定义 `webhookUrl`
  - Teams Webhook: 自定义 `webhookUrl`
  - Custom: 自定义 `endpoint`

### 下游被依赖
- 构建系统 — 调用 `sendBuildSuccessNotifications` / `sendBuildErrorNotifications`
- 备份系统 — 调用 `sendDatabaseBackupNotifications` / `sendVolumeBackupNotifications`
- Docker 清理 — 调用 `sendDockerCleanupNotifications`
- 服务器监控 — 调用 `sendServerThresholdNotifications`
- 应用启动 — 调用 `sendDokployRestartNotifications`
- tRPC Router — 调用服务层 CRUD 函数管理通知配置

## 6. Go 重写注意事项

### 可直接复用的部分

1. **所有外部 API 调用格式**：各渠道的 HTTP 请求格式（URL、Header、Body 结构）与语言无关，可直接复用：
   - Discord Webhook: `POST webhookUrl` with `{ embeds: [embed] }`
   - Telegram: `POST https://api.telegram.org/bot{token}/sendMessage` with `{ chat_id, text, parse_mode: "HTML", disable_web_page_preview: true, reply_markup: { inline_keyboard } }`
   - Slack Webhook: `POST webhookUrl` with attachments JSON
   - Gotify: `POST {serverUrl}/message` with `X-Gotify-Key` header, body `{ title, message, priority, extras }`
   - Ntfy: `POST {serverUrl}/{topic}` with `X-Priority`, `X-Title`, `X-Tags`, `X-Actions` headers, body 为纯文本
   - Pushover: `POST https://api.pushover.net/1/messages.json` with `application/x-www-form-urlencoded`
   - Teams: Adaptive Card JSON schema v1.4, `{ type: "message", attachments: [{ contentType: "application/vnd.microsoft.card.adaptive", content: cardContent }] }`
   - Lark: Interactive Card schema v2.0, `{ msg_type: "interactive", card: { schema: "2.0", ... } }`
   - Custom: `POST endpoint` with custom headers + JSON body

2. **数据库 Schema**：表结构和关系可直接映射到 Go struct

3. **消息模板字段**：各渠道的消息字段名称和布局结构

### 需要重新实现的部分

1. **邮件发送**：TypeScript 使用 `nodemailer`，Go 可使用标准库 `net/smtp` 或 `gomail` 库
2. **HTML 邮件模板**：TypeScript 使用 React Email（`renderAsync`），Go 需要使用 `html/template` 或其他模板引擎
3. **Resend SDK**：Go 需使用 Resend 的 Go SDK (`github.com/resend/resend-go`) 或直接调用 REST API
4. **HTTP 客户端**：TypeScript 使用 `fetch`，Go 使用 `net/http` 标准库

### 架构优化建议

1. **引入 Notifier 接口**：定义统一的 `Notifier` interface，每种渠道实现该接口，避免当前代码中大量的 if-else 分支：

```go
type Notifier interface {
    Send(ctx context.Context, event NotificationEvent) error
}

type NotificationEvent struct {
    Type      EventType          // build-success, build-error, etc.
    Title     string
    Fields    map[string]string  // 统一字段
    Link      string
    Timestamp time.Time
    Status    string             // "success" | "error"
}
```

2. **消息格式与发送解耦**：将消息构造（Formatter）和发送（Sender）分离，便于单元测试

3. **并发发送**：当前代码串行遍历所有 notification 并串行发送各渠道，Go 可使用 goroutine + `errgroup` 并发发送

4. **错误处理统一**：当前部分渠道（如 Telegram、Lark）静默吞掉错误（仅 `console.log`），部分渠道（如 Discord、Slack）throw error。建议 Go 版本统一返回错误并记录日志，但不影响其他渠道发送

5. **服务层模板化**：11 组 create/update 函数结构几乎完全一致，Go 可使用泛型或代码生成减少重复
