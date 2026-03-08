# email 模块 Gap 分析

## Go 当前实现 (2 文件, 366 行)
- `templates.go`: Go html/template HTML 邮件模板
- `sender.go`: SMTP (go-mail) + Resend API 双通道

## TS 原版实现 (React Email)
- JSX 组件渲染邮件
- Nodemailer + Resend SDK
- 模板：build-success, build-error, backup-success, backup-error, cleanup, restart

## Gap 详情

### 已实现 ✅
1. SMTP 发送
2. Resend API 发送
3. 构建成功/失败模板
4. 备份成功/失败模板
5. Docker 清理模板
6. 重启通知模板

### 需验证 ⚠️
1. HTML 模板样式是否与 TS 版 React Email 输出一致
2. 邮件验证码模板（注册流程用）

## 影响评估
- **严重度**: 低。功能完整。
