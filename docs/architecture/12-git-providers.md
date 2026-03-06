# Git 提供商集成

## 1. 模块概述

Git 提供商集成模块负责 Dokploy 与外部 Git 平台的对接，支持四种 Git 提供商（GitHub、GitLab、Gitea、Bitbucket）以及通用 Git 仓库（通过 SSH 密钥）。模块涵盖 OAuth 认证流程、仓库/分支列表查询、代码克隆、Webhook 自动部署触发、以及 PR 预览部署集成。

每个提供商包含两层：
- **Service 层** (`services/`) - CRUD 操作、PR 评论管理、数据库交互
- **Provider/Utils 层** (`utils/providers/`) - 认证、仓库克隆命令生成、API 调用（列出仓库/分支）

此外还有 `raw.ts`（Compose 编辑器直接生成文件）和 `docker.ts`（Docker 镜像拉取命令）两个辅助模块。

在系统架构中的位置：
```
tRPC Router -> 服务层(services/) -> 工具层(utils/providers/) -> Git 平台 API / git CLI
                                                             |
                                                      SSH 密钥(ssh-key.ts)
```

## 2. 数据模型

### 2.1 git_provider 表（统一入口）

```typescript
// db/schema/git-provider.ts
export const gitProvider = pgTable("git_provider", {
    gitProviderId: text("gitProviderId").primaryKey(),  // nanoid
    name: text("name").notNull(),                      // 提供商名称
    providerType: gitProviderType("providerType"),     // "github" | "gitlab" | "bitbucket" | "gitea"
    createdAt: text("createdAt"),
    organizationId: text("organizationId"),            // 所属组织
    userId: text("userId"),                            // 创建者
});
```

`gitProvider` 是统一的父表，通过一对一关系关联到具体的提供商子表（`github`、`gitlab`、`bitbucket`、`gitea`）。

### 2.2 GitHub 子表

```typescript
// db/schema/github.ts
export const github = pgTable("github", {
    githubId: text("githubId").primaryKey(),
    githubAppName: text("githubAppName"),           // GitHub App 名称
    githubAppId: integer("githubAppId"),             // GitHub App ID
    githubClientId: text("githubClientId"),          // OAuth Client ID
    githubClientSecret: text("githubClientSecret"),  // OAuth Client Secret
    githubInstallationId: text("githubInstallationId"), // Installation ID
    githubPrivateKey: text("githubPrivateKey"),       // App 私钥
    githubWebhookSecret: text("githubWebhookSecret"), // Webhook 密钥
    gitProviderId: text("gitProviderId"),             // 关联 git_provider
});
```

### 2.3 GitLab 子表

```typescript
// db/schema/gitlab.ts
export const gitlab = pgTable("gitlab", {
    gitlabId: text("gitlabId").primaryKey(),
    gitlabUrl: text("gitlabUrl").default("https://gitlab.com"),  // 支持自托管
    gitlabInternalUrl: text("gitlabInternalUrl"),     // 内部 URL（同实例时使用）
    applicationId: text("application_id"),            // OAuth Application ID
    redirectUri: text("redirect_uri"),
    secret: text("secret"),                           // OAuth Secret
    accessToken: text("access_token"),                // OAuth Access Token
    refreshToken: text("refresh_token"),              // OAuth Refresh Token
    groupName: text("group_name"),                    // 过滤仓库的组名（逗号分隔）
    expiresAt: integer("expires_at"),                 // Token 过期时间（Unix 秒）
    gitProviderId: text("gitProviderId"),
});
```

### 2.4 Gitea 子表

Gitea 结构与 GitLab 类似，包含 `giteaUrl`、`giteaInternalUrl`、`clientId`、`clientSecret`、`accessToken`、`refreshToken`、`expiresAt` 等字段。

### 2.5 Bitbucket 子表

Bitbucket 支持两种认证方式：
- **API Token** + Atlassian 邮箱
- **App Password** + 用户名

包含 `bitbucketUsername`、`bitbucketEmail`、`appPassword`、`apiToken`、`bitbucketWorkspaceName` 等字段。

### 2.6 SSH Key 表

```typescript
// db/schema/ssh-key.ts
export const sshKeys = pgTable("ssh-key", {
    sshKeyId: text("sshKeyId").primaryKey(),
    privateKey: text("privateKey").default(""),    // 私钥
    publicKey: text("publicKey").notNull(),         // 公钥
    name: text("name").notNull(),
    description: text("description"),
    lastUsedAt: text("lastUsedAt"),                // 最后使用时间
    organizationId: text("organizationId"),
});
```

SSH 密钥关联到 `applications`、`compose`、`server`，用于通用 Git 仓库克隆和服务器连接。

## 3. 认证机制

### 3.1 GitHub - GitHub App 认证

GitHub 使用 GitHub App 模式（非传统 OAuth），通过 `@octokit/auth-app` 库：

```typescript
// utils/providers/github.ts
export const authGithub = (githubProvider: Github): Octokit => {
    return new Octokit({
        authStrategy: createAppAuth,
        auth: {
            appId: githubProvider.githubAppId,
            privateKey: githubProvider.githubPrivateKey,
            installationId: githubProvider.githubInstallationId,
        },
    });
};

export const getGithubToken = async (octokit) => {
    const installation = await octokit.auth({ type: "installation" });
    return installation.token;  // 短期 Installation Token
};
```

需要三个凭据：`githubAppId`、`githubPrivateKey`、`githubInstallationId`。每次克隆操作都会获取新的 Installation Token，自动轮换无需手动刷新。

### 3.2 GitLab - OAuth2 + Token 刷新

```typescript
// utils/providers/gitlab.ts
export const refreshGitlabToken = async (gitlabProviderId) => {
    const currentTime = Math.floor(Date.now() / 1000);
    const safetyMargin = 60; // 60秒安全边际
    if (gitlabProvider.expiresAt && currentTime + safetyMargin < gitlabProvider.expiresAt) {
        return; // Token 仍有效
    }
    // 使用 gitlabInternalUrl 或 gitlabUrl 刷新 Token
    const baseUrl = gitlabProvider.gitlabInternalUrl || gitlabProvider.gitlabUrl;
    const response = await fetch(`${baseUrl}/oauth/token`, {
        method: "POST",
        body: new URLSearchParams({
            grant_type: "refresh_token",
            refresh_token: gitlabProvider.refreshToken,
            client_id: gitlabProvider.applicationId,
            client_secret: gitlabProvider.secret,
        }),
    });
    // 更新数据库中的 accessToken、refreshToken、expiresAt
};
```

**关键设计：** `gitlabInternalUrl` 支持 GitLab 自托管在 Dokploy 同一实例的场景，避免通过公网绕行。

### 3.3 Gitea - OAuth2 + Token 刷新

```typescript
// utils/providers/gitea.ts
export const refreshGiteaToken = async (giteaProviderId) => {
    const bufferTimeSeconds = 300; // 5分钟缓冲
    if (giteaProvider.expiresAt > currentTimeSeconds + bufferTimeSeconds) {
        return giteaProvider.accessToken;  // Token 仍有效
    }
    // POST {giteaUrl}/login/oauth/access_token
};
```

与 GitLab 类似，支持自托管和内部 URL。如果 `clientId`/`clientSecret`/`refreshToken` 任一缺失，直接返回现有 `accessToken`（兼容仅使用 Personal Access Token 的场景）。Token 刷新失败时优雅降级，返回现有 Token。

### 3.4 Bitbucket - HTTP Basic Auth

```typescript
// utils/providers/bitbucket.ts
export const getBitbucketHeaders = (bitbucketProvider) => {
    if (bitbucketProvider.apiToken) {
        // API Token: Base64(email:apiToken)
        return { Authorization: `Basic ${Buffer.from(`${email}:${apiToken}`).toString("base64")}` };
    }
    // App Password: Base64(username:appPassword)
    return { Authorization: `Basic ${Buffer.from(`${username}:${appPassword}`).toString("base64")}` };
};
```

## 4. 仓库与分支查询

### 4.1 各提供商 API 对比

| 提供商 | 仓库列表 API | 分页方式 | 分支列表 API |
|--------|-------------|---------|-------------|
| GitHub | `octokit.rest.apps.listReposAccessibleToInstallation` | Octokit 自动分页 (`paginate`) | `octokit.rest.repos.listBranches` |
| GitLab | `GET /api/v4/projects?membership=true` | 手动分页（page + per_page=100） | `GET /api/v4/projects/{id}/repository/branches` |
| Gitea | `GET /api/v1/user/repos` | 手动分页（page + limit=50） | `GET /api/v1/repos/{owner}/{repo}/branches` |
| Bitbucket | `GET /2.0/repositories/{workspace}` | 自动翻页（data.next URL） | `GET /2.0/repositories/{owner}/{repo}/refs/branches` |

### 4.2 GitLab 仓库过滤

GitLab 支持按 `groupName` 过滤仓库（逗号分隔的多个组名）：
```typescript
const filteredRepos = allProjects.filter((repo) => {
    if (groupName) {
        return groupName.split(",").some((name) =>
            full_path.toLowerCase().startsWith(name.trim().toLowerCase()),
        );
    }
    return kind === "user";  // 无 groupName 时只显示用户仓库
});
```

### 4.3 连接测试

每个提供商都实现了 `testConnection` 函数，验证凭据有效性并返回可访问的仓库数量：
- `testGitlabConnection` - 刷新 Token + 获取仓库列表 + 支持 groupName 过滤
- `testGiteaConnection` - 刷新 Token + 获取仓库列表 + 更新 `lastAuthenticatedAt`
- `testBitbucketConnection` - 获取 workspace/username 仓库列表

## 5. 代码克隆

### 5.1 克隆命令生成模式

所有提供商的 `clone*Repository` 函数都遵循相同模式：生成一个 shell 命令字符串，而非直接执行操作。这使得同一命令可在本地或通过 SSH 在远程服务器上执行。

```typescript
// 通用流程（以 GitHub 为例）
export const cloneGithubRepository = async ({ appName, owner, branch, githubId, ... }) => {
    let command = "set -e;";
    // 1. 验证前置条件（provider 存在、参数完整）
    // 2. 获取 Token
    const token = await getGithubToken(octokit);
    // 3. 构建输出路径
    const outputPath = join(basePath, appName, "code");
    // 4. 生成克隆命令
    command += `rm -rf ${outputPath};`;
    command += `mkdir -p ${outputPath};`;
    command += `git clone --branch ${branch} --depth 1 ${enableSubmodules ? "--recurse-submodules" : ""} ${cloneUrl} ${outputPath} --progress;`;
    return command;
};
```

### 5.2 各提供商克隆 URL 格式

| 提供商 | 克隆 URL 格式 |
|--------|-------------|
| GitHub | `https://oauth2:{token}@github.com/{owner}/{repo}.git` |
| GitLab | `https://oauth2:{accessToken}@{gitlabUrl_without_protocol}/{pathNamespace}.git` |
| Gitea | `{protocol}://oauth2:{accessToken}@{giteaUrl_without_protocol}/{owner}/{repo}.git` |
| Bitbucket (API Token) | `https://x-bitbucket-api-token-auth:{apiToken}@bitbucket.org/{owner}/{repo}.git` |
| Bitbucket (App Password) | `https://{username}:{appPassword}@bitbucket.org/{owner}/{repo}.git` |
| Git (SSH) | `ssh://{user}@{domain}:{port}/{owner}/{repo}.git` |
| Git (HTTP/S) | 直接使用原始 URL |

注意事项：
- GitLab 使用 `gitlabPathNamespace`（如 `group/subgroup/repo`）而非简单的 `owner/repo`
- Bitbucket 优先使用 `repositorySlug`（URL 友好名称）

### 5.3 通用 Git 仓库（SSH 密钥）

```typescript
// utils/providers/git.ts
export const cloneGitRepository = async ({ customGitUrl, customGitSSHKeyId, ... }) => {
    if (customGitSSHKeyId) {
        // 1. 将私钥写入临时文件
        command += `echo "${sshKey.privateKey}" > /tmp/id_rsa; chmod 600 /tmp/id_rsa;`;
        // 2. 添加主机到 known_hosts
        command += `ssh-keyscan -p ${port} ${domain} >> ${knownHostsPath};`;
        // 3. 设置 GIT_SSH_COMMAND
        command += `export GIT_SSH_COMMAND="ssh -i /tmp/id_rsa -p ${port} -o UserKnownHostsFile=${knownHostsPath}";`;
    }
    // 4. git clone
    command += `git clone --branch ${branch} --depth 1 ${enableSubmodules ? "--recurse-submodules" : ""} --progress ${url} ${outputPath};`;
};
```

SSH URL 解析使用复杂正则表达式（`sanitizeRepoPathSSH`），从 URL 中提取 `user`、`domain`、`port`、`owner`、`repo` 等字段，支持多种 SSH URL 格式。

### 5.4 Git 提交信息获取

```typescript
// utils/providers/git.ts
export const getGitCommitInfo = async ({ appName, serverId, ... }) => {
    const gitCommand = `git -C ${outputPath} log -1 --pretty=format:"%H---DELIMITER---%B"`;
    // 本地: execAsync(command)
    // 远程: execAsyncRemote(serverId, command)
    // 返回 { hash, message }
};
```

### 5.5 Docker 镜像拉取

```typescript
// utils/providers/docker.ts
export const buildRemoteDocker = async (application) => {
    // 生成 docker pull 命令，支持私有仓库登录
    // docker login + docker pull
};
```

### 5.6 Raw Compose 文件生成

```typescript
// utils/providers/raw.ts
export const getCreateComposeFileCommand = (compose) => {
    const encodedContent = encodeBase64(composeFile);
    return `rm -rf ${outputPath}; mkdir -p ${outputPath}; echo "${encodedContent}" | base64 -d > "${filePath}";`;
};
```

## 6. 服务层 CRUD

### 6.1 统一的 Git Provider 管理

```typescript
// services/git-provider.ts
export const removeGitProvider = async (gitProviderId) => { ... };
export const findGitProviderById = async (gitProviderId) => { ... };
export const updateGitProvider = async (gitProviderId, input) => { ... };
```

### 6.2 各提供商 CRUD

所有提供商的创建操作在事务中同时创建 `gitProvider` 父记录和子记录：

```typescript
export const createGithub = async (input, organizationId, userId) => {
    return db.transaction(async (tx) => {
        const newGitProvider = await tx.insert(gitProvider).values({
            providerType: "github", organizationId, name: input.name, userId,
        });
        return await tx.insert(github).values({
            ...input, gitProviderId: newGitProvider.gitProviderId,
        });
    });
};
```

删除时通过 `gitProvider` 的级联删除自动清理子表记录。

## 7. GitHub 预览部署集成

### 7.1 PR 评论管理

```typescript
// services/github.ts
export const createPreviewDeploymentComment = async ({ owner, repo, issue_number, ... }) => {
    // 在 PR 上创建部署状态评论（初始状态：Building）
    // 评论格式为 Markdown 表格：Name | Status | Preview URL | Updated
};

export const updateIssueComment = async ({ comment_id, body, ... }) => {
    // 更新评论状态（Done / Failed）
};
```

### 7.2 安全验证

```typescript
// utils/providers/github.ts
export const checkUserRepositoryPermissions = async (githubProvider, owner, repo, username) => {
    // 检查 PR 作者是否有 write/admin/maintain 权限
    // 防止非授权用户通过 PR 触发预览部署执行恶意代码
};

// services/github.ts
export const createSecurityBlockedComment = async ({ prNumber, prAuthor, ... }) => {
    // 在 PR 上创建安全阻止通知
    // 自动检测是否已有安全评论，防止重复
};
```

## 8. SSH 密钥服务

```typescript
// services/ssh-key.ts
export const createSshKey = async (input) => { ... };
export const removeSSHKeyById = async (sshKeyId) => { ... };
export const updateSSHKeyById = async ({ sshKeyId, ... }) => { ... };
export const findSSHKeyById = async (sshKeyId) => { ... };
```

SSH 密钥用于两个场景：
1. **服务器连接** - 通过 `server.sshKeyId` 关联
2. **Git 仓库克隆** - 通过 `application.customGitSSHKeyId` 或 `compose.customGitSSHKeyId` 关联

## 9. 依赖关系

```
Git 提供商模块依赖：
├── octokit (GitHub API SDK)
├── @octokit/auth-app (GitHub App 认证)
├── drizzle-orm (数据库操作)
├── services/ssh-key.ts (SSH 密钥管理)
├── services/preview-deployment.ts (预览部署更新)
├── utils/process/execAsync (命令执行)
├── utils/docker/utils.ts (base64 编码)
└── constants (路径配置)
```

被依赖：
```
├── utils/builders/*.ts (构建时克隆代码)
├── services/application.ts (应用部署)
├── services/compose.ts (Compose 部署)
├── utils/docker/domain.ts (Compose 克隆)
├── webhooks/ (Webhook 处理)
└── preview-deployment (预览部署)
```

## 10. 源文件清单

```
packages/server/src/
├── db/schema/
│   ├── git-provider.ts                           <- Git Provider 统一表
│   ├── github.ts                                 <- GitHub 子表
│   ├── gitlab.ts                                 <- GitLab 子表
│   ├── gitea.ts                                  <- Gitea 子表
│   ├── bitbucket.ts                              <- Bitbucket 子表
│   └── ssh-key.ts                                <- SSH 密钥表
├── services/
│   ├── git-provider.ts                           <- Git Provider CRUD
│   ├── github.ts                                 <- GitHub CRUD + PR 评论管理
│   ├── gitlab.ts                                 <- GitLab CRUD
│   ├── gitea.ts                                  <- Gitea CRUD
│   ├── bitbucket.ts                              <- Bitbucket CRUD（含事务更新）
│   └── ssh-key.ts                                <- SSH 密钥 CRUD
├── utils/providers/
│   ├── github.ts                                 <- GitHub App 认证、克隆、仓库/分支查询、权限检查
│   ├── gitlab.ts                                 <- GitLab OAuth 刷新、克隆、仓库/分支查询、连接测试
│   ├── gitea.ts                                  <- Gitea OAuth 刷新、克隆、仓库/分支查询、连接测试
│   ├── bitbucket.ts                              <- Bitbucket 双认证、克隆、仓库/分支查询、连接测试
│   ├── git.ts                                    <- 通用 Git SSH/HTTPS 克隆、SSH URL 解析、提交信息获取
│   ├── raw.ts                                    <- Compose 文件内容直接写入
│   └── docker.ts                                 <- Docker 镜像拉取命令生成
```

## 11. Go 重写注意事项

- **GitHub SDK**: 使用 `github.com/google/go-github/v60` 替代 Octokit，GitHub App 认证使用 `github.com/bradleyfalzon/ghinstallation/v2`
- **OAuth Token 刷新**: GitLab/Gitea 的 OAuth2 Token 刷新逻辑可使用 `golang.org/x/oauth2` 标准库简化实现；注意保留安全边际（GitLab 60s，Gitea 300s）和 `internalUrl` 优先逻辑
- **HTTP API 调用**: GitLab/Gitea/Bitbucket 的 REST API 调用使用 `net/http` 标准库即可
- **克隆命令生成**: 所有 `clone*Repository` 函数生成的是 shell 命令字符串，语言无关，可直接复用
- **SSH 密钥处理**: `ssh-keyscan` 命令和 `GIT_SSH_COMMAND` 环境变量设置是 shell 操作，可直接复用
- **Base64 编码**: Go 标准库 `encoding/base64` 替代 `Buffer.from().toString("base64")`
- **URL 解析**: SSH URL 的复杂正则解析可使用 Go 的 `regexp` 包重新实现
- **分页查询**: 各平台的手动分页逻辑需在 Go 中重新实现；建议封装通用分页迭代器
- **Provider 接口统一**: 建议在 Go 中定义 `GitProvider` 接口（`CloneCommand`、`ListRepositories`、`ListBranches`、`TestConnection`），各提供商实现该接口
- **Provider 事务创建**: Go 中使用 `database/sql` 事务确保 `gitProvider` 和子表的原子性创建
