# docker 模块 Gap 分析

## Go 当前实现 (2 文件, 347 行)
- `client.go`: Docker SDK 封装，支持 24 个方法
  - 客户端：NewClient, WithAPIVersion, WithHost, Close
  - 容器：ListContainers, ContainerLogs, ContainerStats, GetContainerByName
  - 服务：ListServices, GetService, RemoveService, ScaleService, RestartService, GetServiceLogs
  - 网络：NetworkExists, CreateNetwork
  - 清理：PruneSystem, CleanupImages, CleanupVolumes, CleanupContainers, CleanupBuildCache
  - Registry：TestRegistryLogin
  - 卷：RemoveVolume
- `registry.go`: Registry 认证和镜像操作

## TS 原版实现
- `Dockerode` SDK 封装 + Docker CLI 混合使用
- 支持本地和远程 Docker (通过 SSH 协议)
- 核心函数：pullImage, containerExists, stopService, startService, removeService, createService, updateService, getContainersByAppName, getServiceStats 等

## Gap 详情

### 已实现 ✅
1. 客户端创建和连接配置
2. 容器列表/日志/统计
3. Swarm 服务 CRUD (列表/获取/删除/扩缩容/重启)
4. 网络管理
5. 系统清理 (镜像/卷/容器/构建缓存)
6. Registry 登录测试

### 缺失功能 ❌
1. **远程 Docker 客户端**: TS 版通过 `getRemoteDocker(serverId)` 支持 SSH 协议连接远程 Docker，Go 版缺少此功能
   - 影响：远程服务器的 Docker 操作无法通过 SDK 完成，需通过 SSH 命令行替代
2. **镜像拉取 (pullImage)**: TS 版有 `pullImage` + `pullRemoteImage`，Go 版缺失
3. **Docker Service 创建/更新**: TS 版 `mechanizeDockerContainer` 是核心函数，负责创建完整的 Docker Swarm Service（含资源限制/挂载/网络/环境变量），Go 版 client 层缺少此功能（可能在 service 层实现）
4. **getContainersByAppName**: 按 appName 过滤容器，TS 版常用
5. **getServiceStats**: 获取服务实例状态
6. **容器日志搜索**: TS 版支持 `search` 参数过滤日志

### 设计差异 ⚠️
1. TS 版大量使用 Docker CLI 命令（通过 `execAsync`），Go 版也采用类似策略（通过 process.ExecAsync），部分 Docker 操作不走 SDK
2. Go 版 Docker Service 创建逻辑主要在 `service/application.go` 和 `ws/drawer_logs.go` 中，而非 docker 模块

## 影响评估
- **严重度**: 中等。核心功能可用，但远程 Docker 和完整的 Service 创建在模块层缺失。
- **建议**: 远程 Docker 操作可继续通过 SSH+CLI 方式实现，无需强制使用 SDK SSH 协议。
