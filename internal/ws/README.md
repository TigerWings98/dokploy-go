# ws

WebSocket 处理器，提供 6 个实时通信端点。
覆盖原 TS 版全部 WebSocket 功能：日志流、容器终端、服务器终端、监控统计、tRPC WS 订阅。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| ws.go | 输入: WebSocket 连接 / 输出: 实时数据流 | 5 个 WS 端点：DeploymentLogs（部署日志流）、ContainerLogs（容器日志流）、DockerStats/DockerStatsMonitoring（容器/主机统计）、Terminal（容器伪终端）、ServerTerminal（服务器伪终端） |
| drawer_logs.go | 输入: tRPC WS 订阅请求 / 输出: tRPC 格式的日志/部署流 | DrawerLogs 端点：tRPC WebSocket 协议兼容、服务器设置流、数据库内联部署、备份恢复流 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
