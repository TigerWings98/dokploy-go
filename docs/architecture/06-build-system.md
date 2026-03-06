# 构建系统

## 1. 模块概述

构建系统负责将用户的源代码或 Docker 镜像转换为可运行的 Docker 镜像。Dokploy 支持 6 种构建类型，每种构建器生成一段 shell 命令字符串，通过 `spawnAsync` 在本地或远程执行。

构建流程在系统架构中的位置：
```
部署触发 → 代码克隆 → 构建器生成命令 → shell 执行构建 → Docker 镜像 → Registry 推送（可选）→ 创建/更新 Docker Service
```

## 2. 构建类型一览

| 构建类型 | 命令行工具 | 构建器镜像/版本 | 适用场景 |
|---------|-----------|---------------|---------|
| `nixpacks` | `nixpacks build` | Nixpacks v1.41.0 | 自动检测语言，零配置部署 |
| `heroku_buildpacks` | `pack build` | `heroku/builder:24` | Heroku 兼容应用 |
| `paketo_buildpacks` | `pack build` | `paketobuildpacks/builder-jammy-full` | Cloud Native Buildpacks |
| `dockerfile` | `docker build` | 用户自定义 Dockerfile | 完全自定义构建 |
| `static` | 生成 Dockerfile → `docker build` | `nginx:alpine` | 静态站点（SPA/MPA） |
| `railpack` | `railpack prepare` + `docker buildx build` | Railpack v0.15.4 | Railway 风格构建 |

## 3. 入口函数

### 3.1 getBuildCommand - 构建命令生成

```typescript
// packages/server/src/utils/builders/index.ts
export const getBuildCommand = async (application: ApplicationNested) => {
    let command = "";
    if (application.sourceType !== "docker") {
        switch (application.buildType) {
            case "nixpacks":    command = getNixpacksCommand(application); break;
            case "heroku_buildpacks": command = getHerokuCommand(application); break;
            case "paketo_buildpacks": command = getPaketoCommand(application); break;
            case "static":      command = getStaticCommand(application); break;
            case "dockerfile":  command = getDockerCommand(application); break;
            case "railpack":    command = getRailpackCommand(application); break;
        }
    }
    // 追加 Registry 推送命令
    if (application.registry || application.buildRegistry || application.rollbackRegistry) {
        command += await uploadImageRemoteCommand(application);
    }
    return command;
};
```

当 `sourceType === "docker"` 时，跳过构建，直接使用 `dockerImage` 拉取镜像。

### 3.2 mechanizeDockerContainer - 创建/更新 Docker Service

```typescript
export const mechanizeDockerContainer = async (application: ApplicationNested) => {
    // 1. 计算资源限制
    const resources = calculateResources({memoryLimit, memoryReservation, cpuLimit, cpuReservation});
    // 2. 生成挂载（volume + bind + file）
    const volumesMount = generateVolumeMounts(mounts);
    const bindsMount = generateBindMounts(mounts);
    const filesMount = generateFileMounts(appName, application);
    // 3. 生成 Swarm 配置
    const { HealthCheck, RestartPolicy, Placement, Labels, Mode, ... } = generateConfigContainer(application);
    // 4. 准备环境变量
    const envVariables = prepareEnvironmentVariables(env, projectEnv, environmentEnv);
    // 5. 确定镜像名称和认证
    const image = getImageName(application);  // appName:latest 或 registry 标签
    const authConfig = getAuthConfig(application);
    // 6. 获取 Docker 客户端（本地或远程）
    const docker = await getRemoteDocker(application.serverId);
    // 7. 构建 CreateServiceOptions
    const settings: CreateServiceOptions = {
        Name: appName,
        TaskTemplate: {
            ContainerSpec: { Image, Env, Mounts, HealthCheck, Command, Args, Ulimits, Labels },
            Networks, RestartPolicy, Placement, Resources,
        },
        Mode, RollbackConfig, EndpointSpec, UpdateConfig,
    };
    // 8. 更新或创建服务
    try {
        const service = docker.getService(appName);
        const inspect = await service.inspect();
        await service.update({ version, ...settings, ForceUpdate: +1 });
    } catch {
        await docker.createService(settings);  // 首次创建
    }
};
```

### 3.3 ApplicationNested 类型

```typescript
export type ApplicationNested = InferResultType<"applications", {
    mounts: true,
    security: true,
    redirects: true,
    ports: true,
    registry: true,
    buildRegistry: true,
    rollbackRegistry: true,
    deployments: true,
    environment: { with: { project: true } },
}>;
```

## 4. 各构建器详解

### 4.1 Nixpacks 构建器

```typescript
// builders/nixpacks.ts
export const getNixpacksCommand = (application: ApplicationNested) => string
```

生成命令：
```bash
nixpacks build {buildAppDirectory} --name {appName} [--no-cache] [--env KEY=VALUE]... [--no-error-without-start]
```

**publishDirectory 支持**：当指定 `publishDirectory` 时，Nixpacks 先构建，然后：
1. `docker create --name {buildContainerId} {appName}` - 创建临时容器
2. `docker cp {container}:/app/{publishDirectory} {localPath}` - 拷贝构建产物
3. `docker rm {buildContainerId}` - 清理临时容器
4. 调用 `getStaticCommand()` 用 nginx 打包静态文件

### 4.2 Heroku Buildpacks 构建器

```typescript
// builders/heroku.ts
export const getHerokuCommand = (application: ApplicationNested) => string
```

生成命令：
```bash
pack build {appName} --path {buildAppDirectory} --builder heroku/builder:{herokuVersion||24} [--clear-cache] [--env KEY=VALUE]...
```

### 4.3 Paketo Buildpacks 构建器

```typescript
// builders/paketo.ts
export const getPaketoCommand = (application: ApplicationNested) => string
```

生成命令：
```bash
pack build {appName} --path {buildAppDirectory} --builder paketobuildpacks/builder-jammy-full [--clear-cache] [--env KEY=VALUE]...
```

### 4.4 Dockerfile 构建器

```typescript
// builders/docker-file.ts
export const getDockerCommand = (application: ApplicationNested) => string
```

生成命令：
```bash
# 可选：创建 .env 文件（当无 publishDirectory 且 createEnvFile 为 true 时）
echo "{base64}" | base64 -d > "{envFilePath}"

cd {dockerContextPath}
{SECRET_KEY=value ...} docker build -t {appName} -f {dockerFilePath} . \
  [--target {dockerBuildStage}] [--no-cache] \
  [--build-arg KEY=VALUE]... \
  [--secret type=env,id=KEY]...
```

特性：
- 支持 `buildArgs` - Docker 构建参数
- 支持 `buildSecrets` - Docker BuildKit 密钥
- 支持 `dockerBuildStage` - 多阶段构建目标
- 支持 `dockerContextPath` - 自定义构建上下文
- 支持 `createEnvFile` - 自动生成 .env 文件

### 4.5 Static 构建器

```typescript
// builders/static.ts
export const getStaticCommand = (application: ApplicationNested) => string
```

动态生成 Dockerfile 和 nginx 配置：

**SPA 模式**（`isStaticSpa = true`）：
```nginx
# 生成的 nginx.conf
server {
    listen 80;
    location / {
        root /usr/share/nginx/html;
        index index.html index.htm;
        try_files $uri $uri/ /index.html;  # SPA fallback
    }
}
```

**生成的 Dockerfile**：
```dockerfile
FROM nginx:alpine
WORKDIR /usr/share/nginx/html/
COPY nginx.conf /etc/nginx/nginx.conf   # 仅 SPA 模式
COPY {publishDirectory || "."} .
CMD ["nginx", "-g", "daemon off;"]
```

然后调用 `getDockerCommand()` 执行 `docker build`。

### 4.6 Railpack 构建器

```typescript
// builders/railpack.ts
export const getRailpackCommand = (application: ApplicationNested) => string
```

两阶段构建：

```bash
# 1. 安装 Railpack
export RAILPACK_VERSION={railpackVersion}
bash -c "$(curl -fsSL https://railpack.com/install.sh)"

# 2. 创建 buildx builder
docker buildx create --use --name builder-containerd --driver docker-container || true

# 3. Prepare 阶段
railpack prepare {buildAppDirectory} --plan-out {planPath} --info-out {infoPath} [--env KEY=VALUE]...

# 4. Build 阶段（使用 BuildKit）
export KEY=value...  # 导出环境变量作为 secrets
docker buildx build \
  --build-arg BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend:v{version} \
  -f {planPath} \
  --output type=docker,name={appName} \
  [--secret id=KEY,env=KEY]... \
  {buildAppDirectory}

# 5. 清理
docker buildx rm builder-containerd
```

特性：
- 使用 `calculateSecretsHash()` 计算密钥哈希用于缓存失效
- 支持 `cleanCache` 时添加随机 `cache-key` 参数

## 5. Compose 构建器

### 5.1 getBuildComposeCommand

```typescript
// builders/compose.ts
export const getBuildComposeCommand = async (compose: ComposeNested) => string
```

生成完整的 Compose 部署命令：

```bash
set -e
{
    # 1. 输出构建信息框
    echo "{logBox}"

    # 2. 注入域名标签到 compose 文件
    {writeDomainsToCompose 生成的命令}

    # 3. 创建 .env 文件（包含 APP_NAME、DOCKER_CONFIG、COMPOSE_PREFIX）
    {envCommand}

    # 4. 进入项目目录
    cd "{COMPOSE_PATH}/{appName}/code"

    # 5. 创建隔离网络（isolatedDeployment 时）
    docker network inspect {appName} >/dev/null 2>&1 || docker network create --attachable {appName}

    # 6. 执行 compose/stack 命令
    env -i PATH="$PATH" {exportEnvCommand} docker {command}

    # 7. 连接 Traefik 到隔离网络（isolatedDeployment 时）
    docker network connect {appName} $(docker ps --filter "name=dokploy-traefik" -q)
}
```

### 5.2 createCommand - 生成 Docker 命令

```typescript
// docker-compose 模式
"compose -p {appName} -f {path} up -d --build --remove-orphans"

// stack 模式
"stack deploy -c {path} {appName} --prune --with-registry-auth"
```

支持自定义命令（`compose.command` 覆盖）。

### 5.3 ComposeNested 类型

```typescript
export type ComposeNested = InferResultType<"compose", {
    environment: { with: { project: true } },
    mounts: true,
    domains: true,
}>;
```

## 6. Drop（ZIP 上传）构建

```typescript
// builders/drop.ts
export const unzipDrop = async (zipFile: File, application: Application) => void
```

处理 ZIP 文件上传部署：
1. 确定目标服务器（优先 buildServerId，其次 serverId）
2. 清空并重建 `APPLICATIONS_PATH/{appName}/code/` 目录
3. 使用 `adm-zip` 解析 ZIP 文件
4. 过滤 `__MACOSX` 目录
5. 检测单根文件夹并自动展开
6. 安全检查：路径遍历检测、危险节点检测（symlink/block device/char device/fifo）
7. 本地：直接写文件；远程：通过 SFTP 上传

## 7. 文件系统工具

### 7.1 getBuildAppDirectory - 构建目录计算

```typescript
// utils/filesystem/directory.ts
export const getBuildAppDirectory = (application: Application) => string
```

根据 `sourceType` 和 `buildType` 计算构建目录：
- 基础路径：`APPLICATIONS_PATH/{appName}/code/{buildPath}`
- `buildPath` 来源：`buildPath`(github)、`gitlabBuildPath`、`bitbucketBuildPath`、`giteaBuildPath`、`dropBuildPath`、`customGitBuildPath`
- Dockerfile 模式额外追加：`/{dockerfile || "Dockerfile"}`

### 7.2 其他文件系统操作

| 函数 | 功能 |
|------|------|
| `recreateDirectory(path)` | 删除并重建目录 |
| `recreateDirectoryRemote(path, serverId)` | 远程删除并重建目录 |
| `removeDirectoryCode(appName, serverId?)` | 删除应用代码目录 |
| `removeComposeDirectory(appName, serverId?)` | 删除 compose 目录 |
| `removeMonitoringDirectory(appName, serverId?)` | 删除监控数据目录 |
| `getDockerContextPath(application)` | 获取 Docker 构建上下文路径 |

## 8. 构建辅助工具

### 8.1 createEnvFileCommand

```typescript
// builders/utils.ts
export const createEnvFileCommand = (directory, env, projectEnv?, environmentEnv?) => string
```

生成在 Dockerfile 同级目录创建 `.env` 文件的 shell 命令。

## 9. 环境变量处理

所有构建器都支持三级环境变量：

1. **服务级** - `application.env`
2. **环境级** - `application.environment.env`
3. **项目级** - `application.environment.project.env`

变量引用语法：
- `${{project.DATABASE_URL}}` - 引用项目变量
- `${{environment.API_KEY}}` - 引用环境变量
- `${{SERVICE_NAME}}` - 引用同级服务变量

## 10. 依赖关系

```
构建系统依赖：
├── utils/docker/utils (环境变量、挂载、资源计算)
├── utils/docker/domain (compose 域名注入)
├── utils/cluster/upload (registry 推送)
├── utils/filesystem/directory (路径计算)
├── utils/servers/remote-docker (远程 Docker)
├── utils/process/ (命令执行)
├── adm-zip (ZIP 解压)
├── boxen (日志格式化)
├── shell-quote (Shell 转义)
└── yaml (YAML 解析)
```

被依赖：
```
├── services/application.ts (deployApplication)
├── services/compose.ts (deployCompose)
└── queues/deployments-queue.ts (部署 Worker)
```

## 11. 源文件清单

```
packages/server/src/utils/builders/
├── index.ts              ← 入口（getBuildCommand, mechanizeDockerContainer）
├── nixpacks.ts           ← Nixpacks 构建器
├── heroku.ts             ← Heroku Buildpacks 构建器
├── paketo.ts             ← Paketo Buildpacks 构建器
├── docker-file.ts        ← Dockerfile 构建器
├── static.ts             ← 静态站点构建器（nginx:alpine）
├── railpack.ts           ← Railpack 构建器
├── compose.ts            ← Compose 构建器
├── drop.ts               ← ZIP 上传处理
└── utils.ts              ← 辅助工具（createEnvFileCommand）

packages/server/src/utils/filesystem/
├── directory.ts          ← 文件系统操作与路径计算
└── ssh.ts                ← SSH 密钥生成（generateSSHKey）
```

## 12. Go 重写注意事项

- **构建命令**: 所有构建器生成的 shell 命令（nixpacks、pack、docker build、railpack）都是语言无关的，可直接复用
- **nginx 配置模板**: `static.ts` 中的 nginx.conf 和 Dockerfile 模板是语言无关的
- **Docker Service API**: 使用 Go Docker SDK 的 `client.ServiceCreate` / `client.ServiceUpdate` 替代 Dockerode
- **环境变量解析**: `${{project.XXX}}` 模板语法需在 Go 中实现正则替换
- **ZIP 解压**: 使用 Go 标准库 `archive/zip` 替代 adm-zip
- **SFTP 上传**: 使用 `github.com/pkg/sftp` 替代 ssh2
- **路径计算**: 注意 `APPLICATIONS_PATH` 和 `COMPOSE_PATH` 的基路径差异（本地 vs 远程）
- **Compose 类型**: 使用 `github.com/compose-spec/compose-go` 处理 compose 文件
- **安全检查**: `isDangerousNode` 的危险文件类型检测逻辑需在 Go 中保留
