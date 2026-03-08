# scheduler 模块 Gap 分析

## Go 当前实现 (1 文件, 235 行)
- robfig/cron 定时任务管理

## TS 原版实现 (apps/schedules + BullMQ)
- Hono HTTP 服务 + BullMQ Redis 队列
- 4 种任务类型：backup, server(清理), schedule(自定义), volume-backup
- API 端点管理任务的添加/删除/立即执行

## Gap 详情

### 已实现 ✅
1. Cron 任务注册/取消
2. 备份定时任务
3. Docker 清理定时任务
4. 自定义脚本定时任务
5. RunNow 立即执行

### 需验证 ⚠️
1. 任务执行日志记录
2. 任务执行失败通知
3. 卷备份定时任务

## 影响评估
- **严重度**: 低。
