# email

HTML 邮件模板和发送功能，支持 SMTP 和 Resend 双通道。
替代原 TS 版的 React Email，使用 Go html/template 生成 HTML 邮件。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| templates.go | 输入: 模板参数 / 输出: HTML 邮件内容 | HTML 邮件模板：构建成功/失败、备份成功/失败、清理、重启等事件模板 |
| sender.go | 输入: 收件人 + HTML 内容 / 输出: 发送结果 | 邮件发送：SMTP (go-mail) 和 Resend API 双通道发送 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
