# db/schema 模块 Gap 分析

## Go 当前实现 (21 文件, 2231 行, ~64 struct, 43 个表)
通过文件合并策略覆盖了大部分表。

## TS 原版实现 (42 文件, 55 个 pgTable)

## Gap 详情

### 缺失表 ❌

| TS 表名 | 说明 | 影响 |
|---------|------|------|
| `ai` | AI 功能表 | 企业功能，已通过 stub 处理，不影响自托管 |
| `slack` | Slack 通知子表 | **通知架构不兼容**，见下方详述 |
| `telegram` | Telegram 通知子表 | 同上 |
| `discord` | Discord 通知子表 | 同上 |
| `email` | Email 通知子表 | 同上 |
| `resend` | Resend 通知子表 | 同上 |
| `gotify` | Gotify 通知子表 | 同上 |
| `ntfy` | Ntfy 通知子表 | 同上 |
| `custom` | Custom Webhook 通知子表 | 同上 |
| `lark` | Lark 通知子表 | 同上 |
| `pushover` | Pushover 通知子表 | 同上 |
| `teams` | Teams 通知子表 | 同上 |

### Go 独有表 ⚠️

| Go 表名 | 说明 |
|---------|------|
| `patch` | 补丁管理表，TS 版也有但可能用不同机制 |
| `security` | 安全策略表，TS 版有同名表 |

### 关键架构差异：通知系统 🔴

**TS 版设计**：notification 主表 + 11 个渠道子表，通过 slackId/telegramId 等外键一对一关联
```
notification → slack (FK: slackId)
notification → telegram (FK: telegramId)
notification → discord (FK: discordId)
...
```

**Go 版设计**：所有渠道字段直接扁平化到 Notification struct 中
```
Notification {
    SlackWebhookURL, SlackChannel,
    TelegramBotToken, TelegramChatID,
    DiscordWebhookURL,
    SmtpServer, SmtpPort, ...
}
```

**影响**：
- Go 版读写 notification 表时，不会访问 slack/telegram 等子表
- 如果数据库中通知配置存在子表中（由 TS 版创建），Go 版将无法读取
- 需要修改 Go 的 Notification 模型，改为通过关系加载子表，或编写迁移将子表数据合并到主表

### 字段级差异

未逐字段对比（42 vs 21 文件规模太大），建议在集成测试时通过实际数据库验证。关键已知差异：
1. TS 版 notification 表有 `slackId/telegramId/discordId` 等外键字段，Go 版没有这些字段
2. Lark/Pushover/Teams 通知在 Go 版的 Notification struct 中缺少对应字段

## 影响评估
- **严重度**: 高（通知系统架构不兼容）
- **建议**: 优先修复通知模型，改为关系查询或数据迁移
