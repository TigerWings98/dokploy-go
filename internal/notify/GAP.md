# notify 模块 Gap 分析

## Go 当前实现 (1 文件, 278 行)
- 7 种事件类型
- 7 种发送渠道：Slack, Discord, Telegram, Webhook(Custom), Email(SMTP), Gotify, Ntfy
- Send: 查询组织通知配置 → 按事件过滤 → 并发发送
- SendTest: 单渠道测试发送

## TS 原版实现 (8 个文件, 11 种渠道)
- Slack, Discord, Telegram, Email(SMTP), Resend, Gotify, Ntfy, Pushover, Custom Webhook, Lark, Teams

## Gap 详情

### 已实现 ✅
1. Slack (webhook URL)
2. Discord (webhook URL)
3. Telegram (bot token + chat ID)
4. Email/SMTP (smtp 直连)
5. Gotify (URL + token)
6. Ntfy (URL + topic)
7. Custom Webhook (URL)
8. 事件类型过滤
9. 测试发送

### 缺失渠道 ❌
1. **Resend** - 通过 Resend API 发送邮件（TS 版使用 resend SDK）
2. **Pushover** - 推送通知服务
3. **Lark** (飞书) - Webhook
4. **Teams** (Microsoft Teams) - Webhook

### 架构问题 🔴
- 与 db/schema 的 GAP 关联：Go 版 Notification struct 将渠道字段扁平化到主表，但 TS 版使用独立子表。如果通知配置由 TS 版创建，Go 版将无法正确读取渠道配置。

## 影响评估
- **严重度**: 中等。7/11 渠道已实现，但通知表架构不兼容是核心问题。
- **建议**: 先解决 Notification 数据模型兼容性问题，再补充 4 个缺失渠道。
