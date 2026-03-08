# scheduler

Cron 定时任务调度器。
使用 robfig/cron 管理定时备份、Docker 清理、自定义脚本等周期性任务。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| scheduler.go | 输入: 定时任务配置 (Cron 表达式 + 任务类型) / 输出: 任务执行结果 | Cron 任务管理：注册/取消定时任务、支持 RunNow 立即执行、4 种任务类型（backup/server/schedule/volume-backup） |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
