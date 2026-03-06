# 监控服务

## 1. 模块概述

监控服务（`apps/monitoring`）是 Dokploy 的独立监控组件，**已经使用 Go 实现**。它作为一个轻量级的 Fiber HTTP 服务运行在每台被管理的服务器上（以 Docker 容器 `dokploy-monitoring` 的形式部署），负责：

1. **服务器指标采集** - 定时收集 CPU、内存、磁盘、网络等系统指标
2. **容器指标采集** - 通过 `docker stats` 命令收集指定容器的资源使用数据
3. **指标存储** - 使用 SQLite 本地存储所有指标数据
4. **阈值告警** - 当 CPU/内存超过配置阈值时，通过 HTTP 回调通知 Dokploy 主服务
5. **指标查询 API** - 提供 REST 端点供 Dokploy 前端查询历史指标
6. **数据清理** - 基于 cron 定时清理过期指标数据

在系统架构中的位置：
```
Dokploy 主服务 → 部署 dokploy-monitoring 容器 → 远程服务器
                ← HTTP API 查询指标 ←
                ← HTTP 回调告警 ←
```

与 TypeScript 侧的关联：`packages/server/src/services/docker.ts` 中的 `getContainers()` 等函数通过 `docker ps` CLI 命令列出和过滤容器，与监控服务通过 `docker stats` 采集容器资源指标形成互补。`getContainers` 获取容器列表和状态信息（ID、名称、镜像、端口、状态），而监控服务关注容器的 CPU、内存、网络、磁盘 IO 等运行时指标。

## 2. 设计详解

### 2.1 配置管理 - config/metrics.go

配置通过环境变量 `METRICS_CONFIG` 以 JSON 格式传入，使用 `sync.Once` 确保只解析一次：

```go
type Config struct {
    Server struct {
        ServerType    string `json:"type"`         // 服务器类型标识
        RefreshRate   int    `json:"refreshRate"`   // 服务器指标采集间隔（秒）
        Port          int    `json:"port"`          // HTTP 服务端口
        Token         string `json:"token"`         // API 认证 token
        UrlCallback   string `json:"urlCallback"`   // 告警回调 URL
        CronJob       string `json:"cronJob"`       // 清理 cron 表达式
        RetentionDays int    `json:"retentionDays"`  // 数据保留天数
        Thresholds    struct {
            CPU    int `json:"cpu"`     // CPU 阈值百分比
            Memory int `json:"memory"`  // 内存阈值百分比
        } `json:"thresholds"`
    } `json:"server"`
    Containers struct {
        RefreshRate int `json:"refreshRate"` // 容器采集间隔（秒）
        Services    struct {
            Include []string `json:"include"` // 要监控的容器名（白名单）
            Exclude []string `json:"exclude"` // 要排除的容器名（黑名单）
        } `json:"services"`
    } `json:"containers"`
}
```

配置读取使用单例模式 (`sync.Once`)，首次调用 `GetMetricsConfig()` 时从环境变量解析并缓存。

### 2.2 数据库层

#### SQLite 初始化 (database/db.go)

```go
type DB struct {
    *sql.DB
}

func InitDB() (*DB, error) {
    db, err := sql.Open("sqlite3", "./monitoring.db")
    // 创建 server_metrics 表
}
```

#### 服务器指标表 (server_metrics)

```sql
CREATE TABLE IF NOT EXISTS server_metrics (
    timestamp TEXT PRIMARY KEY,
    cpu REAL, cpu_model TEXT, cpu_cores INTEGER, cpu_physical_cores INTEGER,
    cpu_speed REAL, os TEXT, distro TEXT, kernel TEXT, arch TEXT,
    mem_used REAL, mem_used_gb REAL, mem_total REAL, uptime INTEGER,
    disk_used REAL, total_disk REAL, network_in REAL, network_out REAL
);
```

`ServerMetric` 结构体包含 18 个字段：时间戳、CPU（使用率/型号/核数/物理核数/频率）、OS 信息（系统/发行版/内核/架构）、内存（使用百分比/已用GB/总量GB）、运行时长、磁盘（已用百分比/总量GB）、网络（入/出流量MB）。

数据库操作方法：

| 方法 | 功能 |
|------|------|
| `SaveMetric(metric)` | 插入一条服务器指标记录，自动补全空时间戳 |
| `GetMetricsInRange(start, end)` | 按时间范围查询指标（ASC 排序） |
| `GetLastNMetrics(n)` | 查询最近 N 条指标（CTE 子查询 DESC 取 N 条，外层 ASC 排序） |
| `GetAllMetrics()` | 查询所有指标（ASC 排序） |

#### 容器指标表 (container_metrics)

```sql
CREATE TABLE IF NOT EXISTS container_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    container_id TEXT NOT NULL,
    container_name TEXT NOT NULL,
    metrics_json TEXT NOT NULL  -- JSON 序列化的完整指标
);
-- 索引
CREATE INDEX IF NOT EXISTS idx_container_metrics_timestamp ON container_metrics(timestamp);
CREATE INDEX IF NOT EXISTS idx_container_metrics_name ON container_metrics(container_name);
```

`ContainerMetric` 结构体及嵌套类型：

```go
type ContainerMetric struct {
    Timestamp string        `json:"timestamp"`
    CPU       float64       `json:"CPU"`
    Memory    MemoryMetric  `json:"Memory"`   // percentage, used, total, usedUnit, totalUnit
    Network   NetworkMetric `json:"Network"`  // input, output, inputUnit, outputUnit
    BlockIO   BlockIOMetric `json:"BlockIO"`  // read, write, readUnit, writeUnit
    Container string        `json:"Container"`
    ID        string        `json:"ID"`
    Name      string        `json:"Name"`
}
```

容器指标以 JSON 文本形式存储在 `metrics_json` 列中，查询时反序列化。

| 方法 | 功能 |
|------|------|
| `InitContainerMetricsTable()` | 创建表和索引 |
| `SaveContainerMetric(metric)` | JSON 序列化后插入 |
| `GetLastNContainerMetrics(name, limit)` | 按容器名查询最近 N 条（去掉名称前缀 `/`） |
| `GetAllMetricsContainer(name)` | 按容器名查询所有指标 |

#### 数据清理 (database/cleanup.go)

使用 `robfig/cron/v3` 按配置的 cron 表达式定期清理过期数据：

```go
func CleanupMetrics(db *sql.DB, retentionDays int) error {
    cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
    // DELETE FROM container_metrics WHERE timestamp < cutoffDate
    // DELETE FROM server_metrics WHERE timestamp < cutoffDate
}

func StartMetricsCleanup(db *sql.DB, retentionDays int, cronExpression string) (*cron.Cron, error) {
    // 创建 cron 实例，注册清理函数，启动
}
```

### 2.3 服务器指标采集 - monitoring/monitor.go

使用 `gopsutil/v3` 库采集系统指标：

```go
func GetServerMetrics() database.ServerMetric {
    v, _ := mem.VirtualMemory()       // 内存
    c, _ := cpu.Percent(time.Second, false) // CPU 使用率（阻塞 1 秒采样）
    cpuInfo, _ := cpu.Info()           // CPU 型号/频率
    diskInfo, _ := disk.Usage("/")     // 磁盘使用
    netInfo, _ := net.IOCounters(false) // 网络流量
    hostInfo, _ := host.Info()         // 内核/架构/运行时长
    distro := getRealOS()              // 操作系统发行版
    // 计算内存使用百分比 = (total - available) / total * 100
    // 网络流量转换为 MB
}
```

`getRealOS()` 检测优先级：
1. `/etc/os-release` 中的 `PRETTY_NAME` -> `NAME + VERSION` -> `ID`
2. `/etc/system-release`（RHEL/CentOS/Fedora）
3. `uname -a` 输出匹配
4. `runtime.GOOS` 兜底

#### 阈值告警

```go
func CheckThresholds(metrics database.ServerMetric) error {
    // 当 CPU/内存超过阈值时，向 urlCallback 发送 POST 请求
    // 告警载荷包裹在 {"json": AlertPayload} 中
}

type AlertPayload struct {
    ServerType string  `json:"ServerType"`
    Type       string  `json:"Type"`       // "CPU" 或 "Memory"
    Value      float64 `json:"Value"`
    Threshold  float64 `json:"Threshold"`
    Message    string  `json:"Message"`    // 人类可读描述
    Timestamp  string  `json:"Timestamp"`
    Token      string  `json:"Token"`      // 认证 token
}
```

#### 指标格式转换

`ConvertToSystemMetrics()` 将数据库层的 `float64` 指标转换为 `string` 格式（`%.2f`），用于 API 响应的 `SystemMetrics` 结构体。API 返回的 CPU、内存等数值字段都是字符串类型。

### 2.4 容器指标采集

#### containers/monitor.go - ContainerMonitor

```go
type ContainerMonitor struct {
    db        *database.DB
    isRunning bool       // 互斥锁保护，防止并发采集
    mu        sync.Mutex
    stopChan  chan struct{}
}
```

采集流程：
1. 执行 `docker stats --no-stream --format '{"BlockIO":"{{.BlockIO}}","CPUPerc":"{{.CPUPerc}}",...}'`
2. 逐行解析 JSON 输出为 `Container` 结构体
3. 通过 `ShouldMonitorContainer()` 过滤容器
4. 通过 `GetServiceName()` 提取服务名并去重（同一服务只保留一条）
5. 调用 `processContainerMetrics()` 解析百分比/内存/网络/IO 字符串为数值
6. 存入数据库

`processContainerMetrics()` 解析逻辑：
- CPU: 去掉 `%` 后缀，`ParseFloat`
- Memory: 解析 `"100MiB / 2GiB"` 格式，分离数值和单位，`MiB` -> `MB`，`GiB` -> `GB`
- Network: 解析 `"1.5kB / 2.3kB"` 格式
- BlockIO: 解析 `"1.2MB / 3.4MB"` 格式

#### containers/config.go - 容器过滤

```go
func ShouldMonitorContainer(containerName string) bool {
    // 1. 在 Exclude 列表中 → false（包含匹配 strings.Contains）
    // 2. Include 列表非空 → 只监控列表中的容器
    // 3. Include 为空 → 监控所有非排除容器
}

func GetServiceName(containerName string) string {
    // 去掉 "/" 前缀，按 "-" 分割，去掉最后一部分（容器副本 ID）
    // 例如 "/myapp-1" → "myapp"
}
```

### 2.5 HTTP 服务与路由 - main.go

启动流程：

```
main()
  |-- godotenv.Load()
  |-- config.GetMetricsConfig() → 解析 METRICS_CONFIG
  |-- database.InitDB() → 创建 SQLite 和 server_metrics 表
  |-- database.StartMetricsCleanup() → cron 定期清理
  |-- fiber.New() + CORS 配置
  |-- 注册路由:
  |     GET /health        (无认证)
  |     AuthMiddleware      (Bearer Token, 跳过 /health)
  |     GET /metrics        (服务器指标查询, ?limit=N|all)
  |     GET /metrics/containers (容器指标查询, ?appName=xxx&limit=N|all)
  |-- containers.NewContainerMonitor(db) → 初始化容器指标表
  |-- containerMonitor.Start() → goroutine 定期采集容器指标
  |-- goroutine: ticker 定期采集服务器指标
  |     |-- monitoring.GetServerMetrics()
  |     |-- db.SaveMetric()
  |     |-- monitoring.CheckThresholds() → 可能触发 HTTP 告警回调
  |-- app.Listen(:port) (默认 3001)
```

### 2.6 TypeScript 侧 docker.ts 容器管理函数

`packages/server/src/services/docker.ts` 提供了容器列表和管理功能，与监控服务互补：

| 函数 | 功能 | 所用命令 |
|------|------|----------|
| `getContainers(serverId?)` | 列出所有容器（过滤掉 dokploy 自身容器） | `docker ps -a --format '...'` |
| `getContainersByAppNameMatch(appName, appType?, serverId?)` | 按应用名/compose 标签匹配容器 | `docker ps -a --filter/grep` |
| `getContainersByAppLabel(appName, type, serverId?)` | 按标签过滤容器（standalone/swarm/compose） | `docker ps --filter "label=..."` |
| `getStackContainersByAppName(appName, serverId?)` | 列出 Stack 的任务/容器 | `docker stack ps` |
| `getServiceContainersByAppName(appName, serverId?)` | 列出 Service 的任务/容器 | `docker service ps` |
| `getConfig(containerId, serverId?)` | 获取容器详细配置 | `docker inspect` |
| `containerRestart(containerId)` | 重启容器 | `docker container restart` |
| `getSwarmNodes(serverId?)` | 列出 Swarm 节点 | `docker node ls` |
| `getNodeInfo(nodeId, serverId?)` | 获取节点详情 | `docker node inspect` |
| `getNodeApplications(serverId?)` | 列出 Swarm 服务 | `docker service ls` |

这些函数统一支持 `serverId` 参数：有值时通过 `execAsyncRemote()` 远程执行，否则通过 `execAsync()` 本地执行。

## 3. 源文件清单

```
apps/monitoring/
├── main.go                              -- 应用入口、Fiber 路由注册、采集循环启动
├── go.mod                               -- Go 模块定义 (go 1.20)
├── go.sum                               -- 依赖校验
├── config/
│   └── metrics.go                       -- METRICS_CONFIG 环境变量解析（单例模式）
├── middleware/
│   └── auth.go                          -- Bearer Token 认证中间件
├── database/
│   ├── db.go                            -- SQLite 初始化、server_metrics 建表
│   ├── server.go                        -- ServerMetric 结构体与 CRUD 操作
│   ├── containers.go                    -- ContainerMetric 结构体与 CRUD、建表建索引
│   └── cleanup.go                       -- 指标数据定期清理（robfig/cron）
├── monitoring/
│   └── monitor.go                       -- 服务器指标采集（gopsutil）、阈值告警、格式转换
└── containers/
    ├── types.go                         -- Container、MonitoringConfig 等类型定义
    ├── config.go                        -- 容器过滤逻辑（Include/Exclude）、服务名提取
    └── monitor.go                       -- 容器指标采集（docker stats CLI）、指标解析

packages/server/src/services/
└── docker.ts                            -- getContainers 等容器列表/管理函数（TypeScript）
```

## 4. 对外接口

### HTTP API

| 方法 | 路径 | 认证 | 参数 | 响应 |
|------|------|------|------|------|
| GET | `/health` | 无 | 无 | `{"status": "ok"}` |
| GET | `/metrics` | Bearer Token | `?limit=N\|all`（默认 50） | `SystemMetrics[]` |
| GET | `/metrics/containers` | Bearer Token | `?appName=xxx&limit=N\|all`（默认 50） | `ContainerMetric[]`（无 appName 返回空数组） |

### 告警回调（出站 HTTP POST）

目标：`config.Server.UrlCallback`

```json
{
    "json": {
        "ServerType": "...",
        "Type": "CPU",
        "Value": 85.5,
        "Threshold": 80.0,
        "Message": "CPU usage (85.50%) exceeded threshold (80.00%)",
        "Timestamp": "2024-01-01T00:00:00.000000000Z",
        "Token": "..."
    }
}
```

### Go 包级公开函数

| 包 | 函数/方法 | 签名 |
|------|---------|------|
| `config` | `GetMetricsConfig()` | `func GetMetricsConfig() *Config` |
| `database` | `InitDB()` | `func InitDB() (*DB, error)` |
| `database` | `DB.SaveMetric()` | `func (db *DB) SaveMetric(metric ServerMetric) error` |
| `database` | `DB.GetLastNMetrics()` | `func (db *DB) GetLastNMetrics(n int) ([]ServerMetric, error)` |
| `database` | `DB.GetAllMetrics()` | `func (db *DB) GetAllMetrics() ([]ServerMetric, error)` |
| `database` | `DB.GetMetricsInRange()` | `func (db *DB) GetMetricsInRange(start, end time.Time) ([]ServerMetric, error)` |
| `database` | `DB.InitContainerMetricsTable()` | `func (db *DB) InitContainerMetricsTable() error` |
| `database` | `DB.SaveContainerMetric()` | `func (db *DB) SaveContainerMetric(metric *ContainerMetric) error` |
| `database` | `DB.GetLastNContainerMetrics()` | `func (db *DB) GetLastNContainerMetrics(containerName string, limit int) ([]ContainerMetric, error)` |
| `database` | `DB.GetAllMetricsContainer()` | `func (db *DB) GetAllMetricsContainer(containerName string) ([]ContainerMetric, error)` |
| `database` | `CleanupMetrics()` | `func CleanupMetrics(db *sql.DB, retentionDays int) error` |
| `database` | `StartMetricsCleanup()` | `func StartMetricsCleanup(db *sql.DB, retentionDays int, cronExpression string) (*cron.Cron, error)` |
| `monitoring` | `GetServerMetrics()` | `func GetServerMetrics() database.ServerMetric` |
| `monitoring` | `CheckThresholds()` | `func CheckThresholds(metrics database.ServerMetric) error` |
| `monitoring` | `ConvertToSystemMetrics()` | `func ConvertToSystemMetrics(metric database.ServerMetric) SystemMetrics` |
| `containers` | `NewContainerMonitor()` | `func NewContainerMonitor(db *database.DB) (*ContainerMonitor, error)` |
| `containers` | `ContainerMonitor.Start()` | `func (cm *ContainerMonitor) Start() error` |
| `containers` | `ContainerMonitor.Stop()` | `func (cm *ContainerMonitor) Stop()` |
| `containers` | `ShouldMonitorContainer()` | `func ShouldMonitorContainer(containerName string) bool` |
| `containers` | `GetServiceName()` | `func GetServiceName(containerName string) string` |
| `middleware` | `AuthMiddleware()` | `func AuthMiddleware() fiber.Handler` |

## 5. 依赖关系

### 本模块依赖

```
监控服务依赖（Go）：
├── github.com/gofiber/fiber/v2          -- HTTP 框架
├── github.com/mattn/go-sqlite3          -- SQLite 驱动（CGO）
├── github.com/shirou/gopsutil/v3        -- 系统指标采集
│   ├── cpu, mem, disk, net, host 子包
├── github.com/robfig/cron/v3            -- Cron 调度器
├── github.com/joho/godotenv             -- .env 文件加载
└── docker CLI                           -- docker stats 命令（外部依赖）
```

### 被谁依赖

```
├── setup/monitoring-setup.ts            -- 部署 dokploy-monitoring 容器时传入配置
├── Dokploy 主服务                        -- 通过 HTTP API 代理查询指标
├── Dokploy 前端                          -- 通过主服务代理间接查询
└── docker.ts (getContainers 等)         -- 提供容器列表信息，与监控指标数据互补
```

## 6. Go 重写注意事项

### 可直接复用 (Direct Reuse)

此模块**已经是 Go 实现**，在整体项目 Go 重写时可以直接复用。以下部分可原样迁移：

- **所有 Go 源代码** - 整个 `apps/monitoring/` 目录
- **SQLite 表结构** - `server_metrics` 和 `container_metrics` 建表 SQL
- **docker stats 命令** - `docker stats --no-stream --format '{...}'` 命令及 JSON 格式 [可直接复用]
- **gopsutil 指标采集逻辑** - CPU/内存/磁盘/网络采集代码
- **cron 表达式** - 清理任务使用的 cron 表达式格式（robfig/cron 标准）
- **告警回调 HTTP POST 格式** - `{"json": AlertPayload}` 载荷结构
- **API 响应 JSON 格式** - `SystemMetrics` 和 `ContainerMetric` 的序列化格式

### 需要调整

- **模块路径**: 当前 import 路径为 `github.com/mauriciogm/dokploy/apps/monitoring/...`，需调整为新项目的模块路径
- **Go 版本**: 当前 `go 1.20`，可升级到更新版本
- **gopsutil 版本**: 当前使用 `v3`，可考虑升级到 v4
- **HTTP 框架统一**: 如果主服务使用其他框架（如 Echo/Chi），可考虑统一，也可保持 Fiber（监控服务独立部署）
- **Docker SDK**: 当前通过 `exec.Command("docker", "stats", ...)` 执行 CLI 命令，可考虑改用 Docker Engine API (`github.com/docker/docker/client`) 的 `ContainerStats` 获取更精确的流式数据
- **SQLite 并发优化**: 高频采集场景下可配置 WAL 模式 (`PRAGMA journal_mode=WAL`)
- **容器指标存储优化**: 当前 JSON 文本存储在 `metrics_json` 列中，数据量大时可考虑结构化列存储

### docker.ts 中可复用的 Shell 命令 [可直接复用]

以下 `docker` CLI 命令在 Go 重写时可原样使用：

```bash
# 列出所有容器
docker ps -a --format 'CONTAINER ID : {{.ID}} | Name: {{.Names}} | Image: {{.Image}} | Ports: {{.Ports}} | State: {{.State}} | Status: {{.Status}}'

# 按 compose 标签过滤
docker ps -a --filter='label=com.docker.compose.project=<appName>' --format '...'

# 容器详情
docker inspect <containerId> --format='{{json .}}'

# Swarm 节点
docker node ls --format '{{json .}}'
docker node inspect <nodeId> --format '{{json .}}'

# Swarm 服务
docker service ls --format '{{json .}}'
docker service ps <appName> --no-trunc --format '...'
docker stack ps <appName> --no-trunc --format '...'
```
