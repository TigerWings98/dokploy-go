# notify

多渠道通知发送，支持 11 种通知渠道。
根据组织配置的通知目标和事件订阅，向 Slack/Discord/Telegram/Email 等渠道发送格式化消息。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| notify.go | 输入: 事件类型 + 组织 ID / 输出: 发送结果 | 多渠道通知分发：查询组织通知配置 → 按事件类型过滤 → 并发发送到各渠道 (Slack/Discord/Telegram/Email/Resend/Gotify/Ntfy/Pushover/Custom/Lark/Teams) |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
