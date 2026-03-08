# ws 模块 Gap 分析

## Go 当前实现 (2 文件, 1810 行)
- `ws.go` (1020行): 5 个 WS 端点 + 认证 + 统计记录
  - DeploymentLogs, ContainerLogs, DockerStats, DockerStatsMonitoring, Terminal, ServerTerminal
- `drawer_logs.go` (790行): tRPC WS 兼容端点
  - DrawerLogs (tRPC subscription 协议), handleServerSetup, handleDatabaseDeploy, handleBackupRestore, handleVolumeBackupRestore

## TS 原版实现 (6 个 WS 文件)
- `drawer-logs-wss.ts` - tRPC WS 日志订阅
- `deployment-logs-wss.ts` - 部署日志流
- `docker-container-logs-wss.ts` - 容器日志流
- `docker-container-terminal-wss.ts` - 容器终端
- `terminal-wss.ts` - 服务器终端
- `docker-stats-monitoring-wss.ts` - Docker 统计监控

## Gap 详情

### 已完全实现 ✅
1. **DeploymentLogs** - 部署日志文件流式读取，支持 tail/since/search 参数
2. **ContainerLogs** - 容器日志流，支持本地和远程 (SSH) 模式
3. **DockerStats/DockerStatsMonitoring** - 容器和主机统计数据采集 + 文件持久化
4. **Terminal** - 容器伪终端 (creack/pty)，支持本地和远程
5. **ServerTerminal** - 服务器伪终端，支持本地和远程
6. **DrawerLogs** - tRPC WS 订阅协议兼容，支持：
   - 服务器设置流 (server.setup)
   - 数据库内联部署 (5 种数据库)
   - 备份恢复流
   - 卷备份恢复流

### 可能的缺失 ❌
1. **应用部署流 (DrawerLogs)**: TS 版 DrawerLogs 支持应用部署/重部署的实时日志订阅，Go 版需确认是否支持
2. **Compose 部署流 (DrawerLogs)**: 同上
3. **WebSocket 心跳/重连**: 需确认 Go 版是否有心跳机制防止连接断开

## 影响评估
- **严重度**: 低。6 个 WS 端点全部实现，tRPC 协议兼容。
- **建议**: 在实际测试中验证 DrawerLogs 的应用/Compose 部署流。
