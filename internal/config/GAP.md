# config 模块 Gap 分析

## Go 当前实现 (1 文件, 143 行)
- `config.go`: Config struct + Paths struct，从环境变量加载配置
- 支持 DATABASE_URL / POSTGRES_PASSWORD_FILE(Docker Secrets) / 传统默认值三级优先
- 路径系统：生产环境 `/etc/dokploy`，开发环境 `./docker`
- 14 个标准目录路径
- CleanupCronJob 常量

## TS 原版实现 (constants/index.ts, 52 行)
- `paths(isServer)` 函数：同样的 14 个路径，支持 isServer 参数切换远程/本地路径
- IS_CLOUD / DOCKER_API_VERSION / DOCKER_HOST / DOCKER_PORT 常量
- BETTER_AUTH_SECRET 默认值
- CLEANUP_CRON_JOB 常量
- 全局 Docker 客户端实例

## Gap 详情

### 已实现 ✅
1. 完整的 14 个目录路径，与 TS 版一一对应
2. DATABASE_URL 构建（含 Docker Secrets 支持）
3. 环境检测（容器/开发环境）
4. BETTER_AUTH_SECRET 默认值
5. CleanupCronJob 常量
6. Docker API Version / Host / Port 配置

### 设计差异 ⚠️
1. TS 版 `paths(isServer)` 接受参数切换远程服务器路径（远程服务器始终用 `/etc/dokploy`），Go 版在 Load() 时固定路径，无动态切换
   - 影响：远程服务器操作通过 SSH 脚本硬编码 `/etc/dokploy`，功能等价
2. TS 版在 constants 中创建全局 Docker 客户端实例，Go 版 Docker 客户端在 cmd/server/main.go 中创建
   - 影响：无功能差异，仅组织方式不同

## 影响评估
- **严重度**: 低。配置模块完全对齐，无功能缺失。
