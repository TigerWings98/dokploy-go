# setup 模块 Gap 分析

## Go 当前实现 (3 文件, 619 行)
- `setup.go`: 本地初始化 (目录创建 → Swarm → 网络 → Traefik 配置 → 中间件 → Traefik 服务部署 → LetsEncrypt)
- `swarm.go`: SwarmManager (GetSwarmInfo/GetJoinTokens/ListNodes/RemoveNode/LeaveSwarm/UpdateNodeAvailability)
- `remote.go`: 远程服务器设置脚本生成 (12 步 bash 脚本 + 验证脚本)

## TS 原版实现 (4 文件)
- `setup.ts`: Swarm 初始化 + 网络初始化
- `server-setup.ts`: 远程服务器 SSH 设置 (通过 ssh2 库连接，安装 Docker/Traefik/监控)
- `traefik-setup.ts`: Traefik 配置模板和部署
- `config-paths.ts`: 配置路径 (远程目录创建)
- `monitoring-setup.ts`: 监控组件设置

## Gap 详情

### 已实现 ✅
1. 目录结构创建（14 个标准目录）
2. Docker Swarm 初始化 (init + inspect)
3. dokploy-network overlay 网络创建
4. Traefik 静态配置生成（traefik.yml）
5. 默认中间件创建（redirect-to-https）
6. Traefik 服务部署（docker service create）
7. Let's Encrypt ACME 配置注入
8. Swarm 管理操作（节点列表/移除/可用性更新/离开/Join Token）
9. 远程服务器设置脚本（Docker/Swarm/网络/目录/Traefik/Nixpacks/Buildpacks）
10. 服务器组件验证脚本

### 缺失功能 ❌
1. **监控组件设置**: TS 版 `setupMonitoring` 在远程服务器安装监控代理，Go 版缺失
   - 影响：远程服务器无法自动配置监控回调
2. **SSH 实时连接部署**: TS 版通过 ssh2 库实时 SSH 连接执行脚本并回传日志，Go 版生成静态 bash 脚本
   - 影响：Go 通过 handler 层的 SSH 执行补偿，功能等价但实现路径不同
3. **部署记录**: TS 版在设置过程中创建 serverDeployment 记录并更新状态，Go 版未创建部署记录
   - 影响：设置过程无审计记录

### 设计差异 ⚠️
1. TS 版远程 Traefik 用 `docker run` (独立容器)，Go 版本地 Traefik 用 `docker service create` (Swarm 服务)
   - 远程服务器脚本中也用 `docker run`，与 TS 一致
2. Go 版 remote.go 采用脚本生成方式（GenerateServerSetupScript），而非直接 SSH 执行
   - 更灵活，可用于离线场景，但需调用方负责执行

## 影响评估
- **严重度**: 低。核心初始化逻辑完整，监控设置缺失但为独立功能。
