# 进程执行（本地/远程 SSH）

## 1. 模块概述

进程执行模块是 Dokploy 的基础设施层，提供本地命令执行和远程 SSH 命令执行的统一抽象。几乎所有的 Docker 操作、构建命令、Git 操作、备份命令等都通过此模块执行。

在系统架构中的位置：
```
服务层 → 构建器/Docker/Traefik/备份 等工具层 → 进程执行层 → 操作系统/SSH
```

## 2. 核心组件

### 2.1 ExecError — 错误类型

自定义错误类，携带命令执行的完整上下文信息。

**源文件**: `packages/server/src/utils/process/ExecError.ts`

```typescript
interface ExecErrorDetails {
    command: string;          // 执行的命令
    stdout?: string;          // 标准输出
    stderr?: string;          // 标准错误
    exitCode?: number;        // 退出码
    originalError?: Error;    // 原始错误
    serverId?: string | null; // 远程服务器 ID（null 表示本地）
}

class ExecError extends Error {
    // 所有 ExecErrorDetails 字段作为只读属性
    getDetailedMessage(): string;  // 格式化完整错误信息
    isRemote(): boolean;           // 判断是否远程执行错误
}
```

### 2.2 execAsync — 本地命令执行

基于 `child_process.exec` 的 Promise 封装。适用于输出量不大的短命令。

**源文件**: `packages/server/src/utils/process/execAsync.ts`

```typescript
execAsync(
    command: string,
    options?: { cwd?: string; env?: NodeJS.ProcessEnv; shell?: string }
): Promise<{ stdout: string; stderr: string }>
```

**特点**:
- 使用 `util.promisify(exec)` 封装
- 失败时抛出 `ExecError`，携带 stdout/stderr/exitCode
- 缓冲全部输出后返回

### 2.3 execAsyncStream — 本地流式命令执行

与 `execAsync` 类似，但支持实时数据回调。用于构建、部署等长时间运行的操作，将输出实时推送给前端。

```typescript
execAsyncStream(
    command: string,
    onData?: (data: string) => void,  // 实时数据回调
    options?: { cwd?: string; env?: NodeJS.ProcessEnv }
): Promise<{ stdout: string; stderr: string }>
```

**特点**:
- stdout 和 stderr 都会触发 `onData` 回调
- 同时累积完整输出用于最终返回
- 适用于需要实时显示进度的场景（部署日志、构建日志）

### 2.4 execFileAsync — 文件命令执行

基于 `child_process.execFile`，更安全（不经过 shell），支持 stdin 输入。

```typescript
execFileAsync(
    command: string,
    args: string[],
    options?: { input?: string }
): Promise<{ stdout: string; stderr: string }>
```

**特点**:
- 不经过 shell 解析，避免命令注入
- 支持通过 stdin 传递输入数据
- 参数以数组形式传递

### 2.5 execAsyncRemote — SSH 远程命令执行

通过 SSH 连接远程服务器执行命令。是多服务器架构的核心。

```typescript
execAsyncRemote(
    serverId: string | null,   // 服务器 ID，null 则返回空结果
    command: string,
    onData?: (data: string) => void  // 实时数据回调
): Promise<{ stdout: string; stderr: string }>
```

**执行流程**:
1. 如果 `serverId` 为 null，直接返回空结果
2. 通过 `findServerById(serverId)` 从数据库获取服务器信息
3. 验证服务器有 SSH 密钥（`sshKeyId`）
4. 创建 SSH 连接（ssh2 Client）:
   - host: `server.ipAddress`
   - port: `server.port`
   - username: `server.username`
   - privateKey: `server.sshKey.privateKey`
   - timeout: 99999ms
5. 连接就绪后执行命令
6. 实时收集 stdout/stderr，触发 `onData` 回调
7. 命令结束后关闭连接，返回结果

**错误处理**:
- SSH 认证失败: 抛出友好错误信息，提示检查 SSH 密钥
- 命令执行失败: 抛出 `ExecError`，包含退出码和输出

### 2.6 spawnAsync — 子进程生成

基于 `child_process.spawn`，返回 BufferList 和子进程引用。

**源文件**: `packages/server/src/utils/process/spawnAsync.ts`

```typescript
spawnAsync(
    command: string,
    args?: string[],
    onData?: (data: string) => void,
    options?: SpawnOptions
): Promise<BufferList> & { child: ChildProcess }
```

**特点**:
- 返回值同时是 Promise 和包含 `child` 属性的对象
- 使用 BufferList（`bl` 库）累积输出
- 返回子进程引用 `child`，可用于取消操作（kill）
- 适用于需要控制子进程生命周期的场景

### 2.7 sleep — 延时函数

```typescript
sleep(ms: number): Promise<void>
```

## 3. 文件系统操作

### 3.1 目录管理

**源文件**: `packages/server/src/utils/filesystem/directory.ts`

```typescript
// 本地目录重建（删除后重新创建）
recreateDirectory(pathFolder: string): Promise<void>

// 远程目录重建（通过 SSH）
recreateDirectoryRemote(pathFolder: string, serverId: string | null): Promise<void>

// 删除非空目录
removeDirectoryIfExistsContent(path: string): Promise<void>

// 删除文件或目录
removeFileOrDirectory(path: string): Promise<void>

// 删除应用代码目录（本地或远程）
removeDirectoryCode(appName: string, serverId?: string | null): Promise<void>

// 删除 Compose 目录（本地或远程）
removeComposeDirectory(appName: string, serverId?: string | null): Promise<void>

// 删除监控目录（本地或远程）
removeMonitoringDirectory(appName: string, serverId?: string | null): Promise<void>

// 获取应用构建目录路径
getBuildAppDirectory(application: Application): string

// 获取 Docker 上下文路径
getDockerContextPath(application: Application): string | null
```

**设计模式**: 几乎所有目录操作都有本地和远程两个版本。远程版本通过 `execAsyncRemote` + `rm -rf` / `mkdir -p` 实现。

**构建路径计算逻辑** (`getBuildAppDirectory`):
- 根据 `sourceType` 确定 `buildPath`（github/gitlab/bitbucket/gitea/drop/git 各有不同字段）
- 如果 `buildType` 是 `dockerfile`，路径包含 Dockerfile 文件名
- 否则返回代码目录路径

### 3.2 SSH 密钥管理

**源文件**: `packages/server/src/utils/filesystem/ssh.ts`

```typescript
generateSSHKey(type: "rsa" | "ed25519" = "rsa"): Promise<{
    privateKey: string;
    publicKey: string;
}>
```

- 使用 `ssh2.utils.generateKeyPairSync` 生成密钥对
- RSA 密钥使用 4096 位
- 密钥注释为 "dokploy"

## 4. 远程 Docker 连接

**源文件**: `packages/server/src/utils/servers/remote-docker.ts`

```typescript
getRemoteDocker(serverId?: string | null): Promise<Dockerode>
```

**逻辑**:
1. 如果没有 `serverId`，返回本地 Docker 客户端（`docker` 常量）
2. 从数据库查找服务器信息
3. 如果服务器没有 SSH 密钥，返回本地 Docker 客户端
4. 创建通过 SSH 协议连接的远程 Dockerode 实例:
   - protocol: `ssh`
   - host: `server.ipAddress`
   - port: `server.port`
   - username: `server.username`
   - sshOptions.privateKey: `server.sshKey.privateKey`

**使用场景**: 所有需要操作远程 Docker 的地方（容器管理、服务创建、镜像拉取等）。

## 5. 依赖关系

### 本模块依赖
- `ssh2` — SSH 客户端库
- `bl` (BufferList) — 缓冲区列表
- `child_process` — Node.js 内置模块
- `dockerode` — Docker API 客户端
- `packages/server/src/services/server.ts` — `findServerById`（获取服务器信息）
- `packages/server/src/constants/index.ts` — `docker`（本地 Docker 客户端）、`paths`（路径函数）

### 被以下模块依赖
- 构建器模块 (`utils/builders/`)
- Docker 工具 (`utils/docker/`)
- Traefik 工具 (`utils/traefik/`)
- 备份工具 (`utils/backups/`)
- 数据库工具 (`utils/databases/`)
- Git 提供商 (`utils/providers/`)
- 服务器设置 (`setup/`)
- 集群上传 (`utils/cluster/`)
- 几乎所有服务层代码

## 6. 源文件清单

| 文件 | 说明 |
|------|------|
| `packages/server/src/utils/process/ExecError.ts` | 自定义错误类型 |
| `packages/server/src/utils/process/execAsync.ts` | 本地/远程命令执行 |
| `packages/server/src/utils/process/spawnAsync.ts` | 子进程生成 |
| `packages/server/src/utils/filesystem/directory.ts` | 目录管理操作 |
| `packages/server/src/utils/filesystem/ssh.ts` | SSH 密钥生成 |
| `packages/server/src/utils/servers/remote-docker.ts` | 远程 Docker 连接 |

## 7. Go 重写注意事项

### 可直接对应的 Go 实现
- `execAsync` → `os/exec` 包的 `exec.Command().Output()`
- `execAsyncStream` → `exec.Command()` + `StdoutPipe()` + `StderrPipe()` + goroutine 读取
- `spawnAsync` → `exec.Command().Start()` + 返回 `*exec.Cmd` 引用
- `execFileAsync` → `exec.Command(name, args...)` （Go 默认不经过 shell）
- `sleep` → `time.Sleep()`

### SSH 远程执行
- Go 中使用 `golang.org/x/crypto/ssh` 包
- 创建 SSH 客户端 → 打开 Session → 执行命令
- 注意 SSH 密钥格式兼容（PEM 格式）

### 远程 Docker
- Go 中使用 `github.com/docker/docker/client` 包
- SSH 连接: 使用 Docker 的 SSH 传输或通过 SSH 隧道转发 Docker Socket
- 也可使用 `docker/cli` 的 SSH 辅助函数

### 文件系统操作
- `os.MkdirAll` = `mkdir -p`
- `os.RemoveAll` = `rm -rf`
- SSH 密钥生成: `crypto/ed25519` 或 `crypto/rsa`

### 关键设计决策
- 保持本地/远程执行的统一接口模式
- 流式输出回调在 Go 中可用 `io.Reader` + channel 实现
- `ExecError` 在 Go 中用自定义 error 类型实现，携带相同上下文信息
