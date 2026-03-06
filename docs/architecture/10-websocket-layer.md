# WebSocket 层

## 1. 模块概述

WebSocket 层为 Dokploy 提供实时双向通信能力，支持日志流、终端交互、容器监控等场景。系统在同一个 HTTP 服务器上挂载了 6 个独立的 WebSocket 端点，每个端点负责不同类型的实时数据流。

所有 WebSocket 连接都需要通过 `validateRequest` 进行身份验证，确保只有已登录用户才能建立连接。所有操作支持**本地/远程**双模式：本地直接执行命令，远程通过 SSH 在目标服务器上执行。

在系统架构中的位置：
```
前端 UI → WebSocket Client → HTTP Upgrade → WebSocket Server → Docker CLI / SSH / tRPC
```

## 2. WebSocket 服务器初始化

### 2.1 统一注册入口

所有 WebSocket 服务器在 `server.ts` 中统一注册到同一个 HTTP 服务器实例上：

```typescript
// apps/dokploy/server/server.ts
const server = http.createServer((req, res) => { handle(req, res); });

setupDrawerLogsWebSocketServer(server);
setupDeploymentLogsWebSocketServer(server);
setupDockerContainerLogsWebSocketServer(server);
setupDockerContainerTerminalWebSocketServer(server);
setupTerminalWebSocketServer(server);
if (!IS_CLOUD) {
    setupDockerStatsMonitoringSocketServer(server);
}
```

### 2.2 通用连接模式

每个 WebSocket 处理器遵循相同的模式：

1. 创建 `WebSocketServer` 实例（`noServer: true`，指定路径）
2. 监听 HTTP `upgrade` 事件，按 `pathname` 路由到对应的 WSS
3. 在 `connection` 事件中验证身份、解析参数、建立数据流
4. WebSocket 关闭时清理资源（杀进程、关闭 SSH 连接、清除定时器）

```typescript
const wssTerm = new WebSocketServer({
    noServer: true,
    path: "/some-path",
});

server.on("upgrade", (req, socket, head) => {
    const { pathname } = new URL(req.url || "", `http://${req.headers.host}`);
    if (pathname === "/_next/webpack-hmr") return; // 跳过 HMR
    if (pathname === "/some-path") {
        wssTerm.handleUpgrade(req, socket, head, (ws) => {
            wssTerm.emit("connection", ws, req);
        });
    }
});
```

### 2.3 身份验证

所有 WebSocket 端点都通过 `validateRequest(req)` 验证请求，该函数从 HTTP 请求中提取 session 信息：

```typescript
const { user, session } = await validateRequest(req);
if (!user || !session) {
    ws.close();
    return;
}
```

## 3. 六种 WebSocket 类型

### 3.1 Drawer Logs（抽屉日志）

| 属性 | 值 |
|------|-----|
| 路径 | `/drawer-logs` |
| 文件 | `apps/dokploy/server/wss/drawer-logs.ts` |
| 用途 | 通过 tRPC WebSocket 订阅实时日志流 |
| 协议 | tRPC over WebSocket |

与其他 WebSocket 端点不同，Drawer Logs 使用 tRPC 的 `applyWSSHandler` 将 tRPC router 绑定到 WebSocket 上，客户端可以通过 tRPC 订阅（subscription）获取实时数据：

```typescript
applyWSSHandler({
    wss: wssTerm,
    router: appRouter,
    createContext: createTRPCContext,
});
```

### 3.2 Deployment Logs（部署日志）

| 属性 | 值 |
|------|-----|
| 路径 | `/listen-deployment` |
| 文件 | `apps/dokploy/server/wss/listen-deployment.ts` |
| 用途 | 实时跟踪部署日志文件 |
| 参数 | `logPath`（日志文件路径）、`serverId`（可选，远程服务器） |
| 协议 | 单向数据流（服务器 → 客户端） |

工作原理：
- **本地模式**：使用 `spawn("tail", ["-n", "+1", "-f", logPath])` 跟踪日志文件
- **远程模式**：通过 SSH 执行 `tail -n +1 -f ${logPath}`
- 安全验证：通过 `readValidDirectory(logPath, serverId)` 确保日志路径在 `BASE_PATH` 下，防止路径遍历攻击
- 资源清理：WebSocket 关闭时先发 SIGTERM，1 秒后如未终止则发 SIGKILL

### 3.3 Docker Container Logs（容器日志）

| 属性 | 值 |
|------|-----|
| 路径 | `/docker-container-logs` |
| 文件 | `apps/dokploy/server/wss/docker-container-logs.ts` |
| 用途 | 实时查看 Docker 容器/服务日志 |
| 参数 | `containerId`、`tail`（行数，默认100）、`search`（过滤）、`since`（时间范围）、`serverId`、`runType`（swarm/container） |
| 协议 | 双向（客户端可发送输入到 PTY） |

工作原理：
- 根据 `runType` 构建命令：`docker container logs` 或 `docker service logs`
- 支持 `--tail`、`--since`、`--timestamps`、`--follow` 参数
- 搜索过滤：通过管道 `| grep -iF '${search}'` 实现
- 本地使用 `node-pty` 创建伪终端进程，远程通过 SSH `pty: true` 执行
- Keep-alive 机制：每 45 秒发送 ping 防止连接超时

#### 输入验证

所有参数都经过严格验证以防止命令注入：

| 验证函数 | 规则 |
|---------|------|
| `isValidContainerId` | 12-64 位十六进制或合法容器名称（字母数字、下划线、连字符、点） |
| `isValidTail` | 纯数字，0-10000 |
| `isValidSince` | `"all"` 或 `数字+[smhd]` 格式（如 `5m`、`1h`） |
| `isValidSearch` | 仅允许字母数字、空格、点、下划线、连字符，最长 500 字符 |
| `isValidShell` | 白名单：`sh`、`bash`、`zsh`、`ash` 及其 `/bin/` 路径 |

### 3.4 Docker Container Terminal（容器终端）

| 属性 | 值 |
|------|-----|
| 路径 | `/docker-container-terminal` |
| 文件 | `apps/dokploy/server/wss/docker-container-terminal.ts` |
| 用途 | 在容器内打开交互式终端 |
| 参数 | `containerId`、`activeWay`（shell 类型，默认 `sh`）、`serverId` |
| 协议 | 全双工交互（终端输入/输出） |

工作原理：
- **本地模式**：使用 `node-pty` 执行 `docker exec -it -w / ${containerId} ${shell}`
- **远程模式**：通过 SSH 的 `pty: true` 模式执行相同的 docker exec 命令
- 客户端发送的消息（键盘输入）写入 PTY 进程的 stdin
- PTY 进程的 stdout 通过 WebSocket 发送给客户端
- 连接关闭时杀死 PTY 进程或关闭 SSH 连接

### 3.5 Terminal（服务器终端）

| 属性 | 值 |
|------|-----|
| 路径 | `/terminal` |
| 文件 | `apps/dokploy/server/wss/terminal.ts` |
| 用途 | 在服务器上打开 SSH 交互式终端 |
| 参数 | `serverId`（必需，`"local"` 表示本地服务器） |
| 协议 | 全双工交互（SSH shell） |

工作原理：
- **本地服务器**（`serverId === "local"`）：
  - 自动生成 SSH 密钥对（`auto_generated-dokploy-local`），存储在 `SSH_PATH` 下
  - 通过 `getDockerHost()` 获取 Docker 宿主机 IP
  - 首次连接时用户需手动将公钥添加到 `~/.ssh/authorized_keys`
- **远程服务器**：从数据库获取服务器的 SSH 配置
- 统一通过 `ssh2.Client` 建立 SSH 连接，调用 `conn.shell()` 打开交互式 shell
- 连接成功后发送 `\x1bc`（ANSI 清屏）清除连接提示信息

### 3.6 Docker Stats Monitoring（Docker 统计监控）

| 属性 | 值 |
|------|-----|
| 路径 | `/listen-docker-stats-monitoring` |
| 文件 | `apps/dokploy/server/wss/docker-stats.ts` |
| 用途 | 实时监控容器/服务的资源使用情况 |
| 参数 | `appName`、`appType`（`application`/`stack`/`docker-compose`） |
| 协议 | 定时推送（服务器 → 客户端，每 1.3 秒） |
| 限制 | 仅本地模式，Cloud 版本不可用 |

工作原理：
- 每 1.3 秒执行一次轮询
- **特殊情况**：当 `appName === "dokploy"` 时，获取宿主机系统统计信息（`getHostSystemStats`）
- **正常情况**：
  - 根据 `appType` 构建 Docker 容器过滤条件（label 或 name）
  - 使用 Dockerode SDK 的 `docker.listContainers` 查找目标容器
  - 使用 `docker stats --no-stream --format` 获取 JSON 格式的统计数据
  - 调用 `recordAdvancedStats` 记录统计数据，`getLastAdvancedStatsFile` 读取历史数据
- 数据格式包含：BlockIO、CPUPerc、MemPerc、MemUsage、NetIO 等字段
- WebSocket 关闭时清除定时器

## 4. 安全工具层

### 4.1 apps/dokploy/server/wss/utils.ts

提供 WebSocket 处理器使用的安全验证和工具函数：

| 函数 | 功能 |
|------|------|
| `isValidContainerId(id)` | 验证容器 ID 格式 |
| `isValidTail(tail)` | 验证日志行数参数 |
| `isValidSince(since)` | 验证时间范围参数 |
| `isValidSearch(search)` | 验证搜索关键词（防注入） |
| `isValidShell(shell)` | 验证 shell 类型（白名单） |
| `getShell()` | 根据操作系统返回默认 shell |
| `setupLocalServerSSHKey()` | 生成/读取本地服务器 SSH 密钥 |

### 4.2 packages/server/src/wss/utils.ts

提供路径验证和公共 IP 获取功能：

| 函数 | 功能 |
|------|------|
| `readValidDirectory(directory, serverId?)` | 验证路径是否在 `BASE_PATH` 下（防路径遍历） |
| `getPublicIpWithFallback()` | 获取公网 IP（优先 IPv4，回退 IPv6） |
| `getShell()` | 根据操作系统返回默认 shell |

## 5. 消息协议总结

| WebSocket 类型 | 方向 | 消息格式 | 说明 |
|---------------|------|---------|------|
| Drawer Logs | 双向 | tRPC JSON-RPC | tRPC 订阅协议 |
| Deployment Logs | 服务器→客户端 | 纯文本 | tail -f 的原始输出 |
| Container Logs | 双向 | 纯文本 | docker logs 输出 + 客户端可输入 |
| Container Terminal | 全双工 | 纯文本/二进制 | PTY 终端 I/O |
| Server Terminal | 全双工 | 纯文本/二进制 | SSH shell I/O |
| Docker Stats | 服务器→客户端 | JSON (`{ data: ... }`) | 定时推送统计数据 |

## 6. 依赖关系

```
WebSocket 层依赖：
├── ws (WebSocket 实现)
├── ssh2 (SSH 客户端，用于远程模式)
├── node-pty (伪终端，用于本地终端交互)
├── @trpc/server/adapters/ws (tRPC WebSocket 适配器)
├── public-ip (获取公网 IP)
├── dockerode (Docker SDK，用于 stats 容器查找)
├── @dokploy/server/validateRequest (身份验证)
└── @dokploy/server/execAsync (命令执行)
```

被依赖：
```
├── apps/dokploy/server/server.ts (入口注册)
└── 前端 UI 组件 (WebSocket 客户端)
```

## 7. 源文件清单

```
apps/dokploy/server/
├── server.ts                                 ← WebSocket 统一注册入口
└── wss/
    ├── utils.ts                             ← 安全验证工具（输入校验、SSH 密钥）
    ├── drawer-logs.ts                       ← tRPC WebSocket 日志订阅
    ├── listen-deployment.ts                 ← 部署日志 tail -f 流
    ├── docker-container-logs.ts             ← 容器日志流（支持搜索/过滤）
    ├── docker-container-terminal.ts         ← 容器内交互式终端
    ├── terminal.ts                          ← 服务器 SSH 终端
    └── docker-stats.ts                      ← Docker 资源统计监控

packages/server/src/
└── wss/
    └── utils.ts                             ← 路径验证、公网 IP 获取
```

## 8. Go 重写注意事项

- **WebSocket 库**: 使用 `github.com/gorilla/websocket` 或标准库 `nhooyr.io/websocket` 替代 `ws`
- **HTTP Upgrade**: Go 的 WebSocket 库原生支持 HTTP Upgrade，无需 `noServer` 模式，直接在 HTTP handler 中调用 `Upgrader.Upgrade()`
- **PTY 终端**: 使用 `github.com/creack/pty` 替代 `node-pty`，创建伪终端进程
- **SSH 客户端**: 使用 `golang.org/x/crypto/ssh` 替代 `ssh2`，Go 原生 SSH 支持更完善
- **tRPC WebSocket**: Drawer Logs 的 tRPC over WebSocket 需要重新设计，Go 没有 tRPC，可改用原生 WebSocket + JSON 消息协议或 gRPC streaming
- **并发模型**: Go 的 goroutine 天然适合 WebSocket 并发处理，每个连接一个 goroutine，无需事件循环
- **输入验证**: 正则验证逻辑可直接用 `regexp` 包移植
- **资源清理**: 使用 `context.Context` 管理 WebSocket 生命周期，配合 `defer` 确保资源释放
- **Keep-alive**: Go WebSocket 库支持内置 ping/pong 机制，比手动 `setInterval` 更可靠
- **Docker Stats**: `docker stats --no-stream --format` 命令是语言无关的，可直接复用
