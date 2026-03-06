# 17. 预览部署

## 1. 模块概述

预览部署（Preview Deployments）模块实现了基于 Pull Request 的自动化预览环境功能。当 GitHub PR 被创建或更新时，系统自动为该 PR 创建一个隔离的部署实例，生成唯一的预览域名，并通过 GitHub Issue Comment 实时更新部署状态。PR 关闭时自动清理相关资源。

**在系统中的角色：** 预览部署模块衔接 Git 提供者（GitHub Webhook）、构建部署系统、域名管理和 Traefik 路由，提供端到端的 PR 预览体验。它还包含安全验证机制，确保只有受信任的协作者才能触发预览部署。

## 2. 设计详解

### 2.1 数据结构

#### 预览部署表 `preview_deployments`

```typescript
export const previewDeployments = pgTable("preview_deployments", {
  previewDeploymentId: text("previewDeploymentId").notNull().primaryKey(),
  branch: text("branch").notNull(),
  pullRequestId: text("pullRequestId").notNull(),
  pullRequestNumber: text("pullRequestNumber").notNull(),
  pullRequestURL: text("pullRequestURL").notNull(),
  pullRequestTitle: text("pullRequestTitle").notNull(),
  pullRequestCommentId: text("pullRequestCommentId").notNull(),
  previewStatus: applicationStatus("previewStatus").notNull().default("idle"),
  appName: text("appName").notNull().unique(), // 如 "preview-myapp-abc123"
  applicationId: text("applicationId").notNull(), // 关联父应用
  domainId: text("domainId"),       // 关联预览域名
  createdAt: text("createdAt").notNull(),
  expiresAt: text("expiresAt"),     // 可选过期时间
});
```

**关系映射：**
- 一个 PreviewDeployment 属于一个 Application（多对一）
- 一个 PreviewDeployment 有一个 Domain（一对一）
- 一个 PreviewDeployment 有多个 Deployment 记录（一对多）

#### API 创建 Schema

```typescript
export const apiCreatePreviewDeployment = z.object({
  applicationId: z.string().min(1),
  domainId: z.string().optional(),
  branch: z.string().min(1),
  pullRequestId: z.string().min(1),
  pullRequestNumber: z.string().min(1),
  pullRequestURL: z.string().min(1),
  pullRequestTitle: z.string().min(1),
});
```

### 2.2 预览部署生命周期

```
PR 创建/更新 (GitHub Webhook)
    |
    v
createPreviewDeployment(schema)
    |-- 生成唯一 appName: "preview-{parentAppName}-{random6}"
    |-- 生成通配符域名 (generateWildcardDomain)
    |-- 通过 GitHub API 在 PR 上创建初始 Comment（状态：Building）
    |-- INSERT preview_deployments 记录
    |-- 创建 Domain 记录 (createDomain)
    |-- 配置 Traefik 路由 (manageDomain)
    |-- 更新 domainId 到 preview_deployments
    |
    v
deployPreviewApplication(...)
    |-- 创建 Deployment 记录 (createDeploymentPreview)
    |-- 检查 PR Comment 是否存在 (issueCommentExists)
    |   |-- 如不存在：创建新 Comment (createPreviewDeploymentComment)
    |-- 更新 PR Comment（状态：Running/Building）
    |-- 注入预览环境变量 (previewEnv + DOKPLOY_DEPLOY_URL)
    |-- 克隆 GitHub 仓库（指定 PR 分支）
    |-- 执行构建命令 (getBuildCommand)
    |-- mechanizeDockerContainer（启动容器）
    |-- 更新 PR Comment（状态：Done）
    |-- 更新 Deployment 状态 + PreviewDeployment 状态
    |
    v  (PR 再次推送代码时)
rebuildPreviewApplication(...)
    |-- 类似 deployPreviewApplication 但不重新克隆仓库
    |-- 直接执行 getBuildCommand
    |
    v  (PR 关闭时)
removePreviewDeployment(previewDeploymentId)
    |-- removeService（删除 Docker 服务）
    |-- removeDeploymentsByPreviewDeploymentId（删除部署记录及日志）
    |-- removeDirectoryCode（删除源代码目录）
    |-- removeTraefikConfig（删除 Traefik 路由配置）
    |-- DELETE FROM preview_deployments
    |   (注: 每步 try-catch，某步失败不影响后续清理)
```

### 2.3 通配符域名生成

`generateWildcardDomain` 函数根据应用配置的 `previewWildcard` 基础域名生成唯一子域名：

```typescript
const generateWildcardDomain = async (
  baseDomain: string,    // 如 "*.traefik.me" 或 "*.preview.example.com"
  appName: string,       // 如 "preview-myapp-abc123"
  serverIp: string,
  userId: string,
): Promise<string> => {
  if (!baseDomain.startsWith("*.")) {
    throw new Error('The base domain must start with "*."');
  }

  if (baseDomain.includes("traefik.me")) {
    // traefik.me 模式：将 IP 嵌入域名以实现 DNS 解析
    // 开发模式用 127.0.0.1，生产模式用服务器 IP 或 Web 设置中的 IP
    const slugIp = ip.replaceAll(".", "-");
    // 结果如: preview-myapp-abc123-192-168-1-1.traefik.me
    return baseDomain.replace("*", `${appName}${slugIp === "" ? "" : `-${slugIp}`}`);
  }

  // 自定义域名模式：直接替换通配符
  // 结果如: preview-myapp-abc123.preview.example.com
  return baseDomain.replace("*", appName);
};
```

### 2.4 PR Comment 状态更新

系统通过 GitHub Issue Comment API 实时反馈部署状态。Comment 使用 Markdown 表格格式：

```typescript
export const getIssueComment = (
  appName: string,
  status: "success" | "error" | "running" | "initializing",
  previewDomain: string,
) => {
  let statusMessage = "";
  if (status === "success") statusMessage = "Done";
  else if (status === "error") statusMessage = "Failed";
  else statusMessage = "Building";       // initializing 和 running 都显示 Building

  // 返回 Markdown 表格
  // | Name | Status | Preview | Updated (UTC) |
  // | {appName} | {statusEmoji} {statusMessage} | [Preview URL]({domain}) | {ISO timestamp} |
};
```

状态流转：`initializing` -> `running` -> `success` / `error`

**Comment 幂等性处理：** 系统记录 `pullRequestCommentId`，后续更新使用 `updateComment` 而非创建新 comment。如果 comment 被删除（用户或 GitHub 操作），通过 `issueCommentExists` 检测并调用 `createPreviewDeploymentComment` 重新创建，同时更新数据库中的 `pullRequestCommentId`。

### 2.5 安全验证

系统提供 PR 作者权限验证机制，防止未授权用户通过 PR 触发预览部署：

```typescript
// 生成安全阻止通知消息（Markdown 格式）
export const getSecurityBlockedMessage = (
  prAuthor: string, repositoryName: string, permission: string | null,
) => {
  // 返回详细的 Markdown 消息，包含：
  // - 阻止原因（用户权限不足）
  // - 解决方案（获取协作者权限 或 管理员关闭安全检查）
  // - 安全说明（折叠面板）
};

// 检查 PR 是否已有安全通知 comment（防重复）
export const hasExistingSecurityComment = async ({...}) => {
  const comments = await octokit.rest.issues.listComments({...});
  return comments.some(c => c.body?.includes("Preview Deployment Blocked - Security Protection"));
};

// 创建安全阻止 comment
export const createSecurityBlockedComment = async ({...}) => {
  if (await hasExistingSecurityComment({...})) return null;  // 防重复
  const message = getSecurityBlockedMessage(prAuthor, repository, permission);
  return await octokit.rest.issues.createComment({...});
};
```

所需权限级别：`write`、`maintain` 或 `admin`。

### 2.6 环境隔离

预览部署使用独立的环境变量和构建参数，与主应用隔离：

```typescript
// 在 deployPreviewApplication / rebuildPreviewApplication 中
application.appName = previewDeployment.appName;  // 使用预览专属 appName
application.env = `${application.previewEnv}\nDOKPLOY_DEPLOY_URL=${domain}`;
application.buildArgs = `${application.previewBuildArgs}\nDOKPLOY_DEPLOY_URL=${domain}`;
application.buildSecrets = `${application.previewBuildSecrets}\nDOKPLOY_DEPLOY_URL=${domain}`;
application.rollbackActive = false;      // 禁用回滚
application.buildRegistry = null;        // 不使用构建镜像仓库
application.rollbackRegistry = null;
application.registry = null;
```

每个预览部署自动注入 `DOKPLOY_DEPLOY_URL` 环境变量，值为预览域名 host。

## 3. 源文件清单

### 数据库 Schema
- `dokploy/packages/server/src/db/schema/preview-deployments.ts` — 预览部署表结构、关系映射、API Schema（`apiCreatePreviewDeployment`）

### 服务层
- `dokploy/packages/server/src/services/preview-deployment.ts` — 预览部署 CRUD（`createPreviewDeployment`、`findPreviewDeploymentById`、`updatePreviewDeployment`、`removePreviewDeployment`、`findPreviewDeploymentsByApplicationId`、`findPreviewDeploymentsByPullRequestId`、`findPreviewDeploymentByApplicationId`）、域名生成（`generateWildcardDomain`）
- `dokploy/packages/server/src/services/application.ts`（行 352-551+） — `deployPreviewApplication`、`rebuildPreviewApplication`
- `dokploy/packages/server/src/services/github.ts` — PR Comment 管理（`getIssueComment`、`issueCommentExists`、`updateIssueComment`、`createPreviewDeploymentComment`）、安全验证（`getSecurityBlockedMessage`、`hasExistingSecurityComment`、`createSecurityBlockedComment`）

### 相关工具
- `dokploy/packages/server/src/utils/providers/github.ts` — GitHub API 认证（`authGithub`）
- `dokploy/packages/server/src/utils/traefik/application.ts` — Traefik 配置管理（`removeTraefikConfig`）
- `dokploy/packages/server/src/utils/traefik/domain.ts` — 域名路由管理（`manageDomain`）
- `dokploy/packages/server/src/utils/docker/utils.ts` — Docker 服务管理（`removeService`）
- `dokploy/packages/server/src/utils/filesystem/directory.ts` — 文件系统清理（`removeDirectoryCode`）

## 4. 对外接口

### 预览部署服务（preview-deployment.ts）

```typescript
findPreviewDeploymentById(previewDeploymentId: string): Promise<PreviewDeploymentWithRelations>
  // with: domain, application -> server, environment -> project

createPreviewDeployment(schema: {
  applicationId: string; branch: string;
  pullRequestId: string; pullRequestNumber: string;
  pullRequestURL: string; pullRequestTitle: string;
}): Promise<PreviewDeployment>

updatePreviewDeployment(id: string, data: Partial<PreviewDeployment>): Promise<PreviewDeployment[]>

removePreviewDeployment(previewDeploymentId: string): Promise<PreviewDeployment>

findPreviewDeploymentsByApplicationId(applicationId: string): Promise<PreviewDeploymentList>
  // with: deployments (desc), domain

findPreviewDeploymentsByPullRequestId(pullRequestId: string): Promise<PreviewDeployment[]>

findPreviewDeploymentByApplicationId(applicationId: string, pullRequestId: string): Promise<PreviewDeployment | undefined>
```

### 部署操作（application.ts）

```typescript
deployPreviewApplication(params: {
  applicationId: string;
  titleLog: string;         // 默认 "Preview Deployment"
  descriptionLog: string;
  previewDeploymentId: string;
}): Promise<boolean>

rebuildPreviewApplication(params: {
  applicationId: string;
  titleLog: string;         // 默认 "Rebuild Preview Deployment"
  descriptionLog: string;
  previewDeploymentId: string;
}): Promise<boolean>
```

### GitHub Comment 管理（github.ts）

```typescript
getIssueComment(appName: string, status: "success"|"error"|"running"|"initializing", previewDomain: string): string

issueCommentExists(params: {
  owner: string; repository: string; comment_id: number; githubId: string;
}): Promise<boolean>

updateIssueComment(params: {
  owner: string; repository: string; issue_number: string;
  body: string; comment_id: number; githubId: string;
}): Promise<void>

createPreviewDeploymentComment(params: {
  appName: string; owner: string; repository: string; issue_number: string;
  previewDomain: string; githubId: string; previewDeploymentId: string;
}): Promise<PreviewDeployment | undefined>

getSecurityBlockedMessage(prAuthor: string, repositoryName: string, permission: string | null): string
hasExistingSecurityComment(params: { owner: string; repository: string; prNumber: number; githubId: string; }): Promise<boolean>
createSecurityBlockedComment(params: { owner: string; repository: string; prNumber: number; prAuthor: string; permission: string | null; githubId: string; }): Promise<IssueComment | null>
```

## 5. 依赖关系

### 上游依赖
- `@octokit/rest` — GitHub API（通过 `authGithub` 封装，支持 GitHub App 和 Personal Access Token）
- 域名服务（`createDomain`、`manageDomain`）
- Traefik 配置管理（`removeTraefikConfig`）
- Docker 服务管理（`removeService`）
- 文件系统工具（`removeDirectoryCode`）
- 构建系统（`getBuildCommand`、`cloneGithubRepository`、`mechanizeDockerContainer`）
- 部署记录服务（`createDeploymentPreview`、`updateDeploymentStatus`、`removeDeploymentsByPreviewDeploymentId`）
- 命令执行（`execAsync`、`execAsyncRemote`）
- 密码生成（`generatePassword` — 用于生成 appName 后缀）
- Web 服务器设置（`getWebServerSettings` — 获取服务器 IP）

### 下游被依赖
- GitHub Webhook 处理器 — 接收 PR `opened`/`synchronize`/`closed` 事件
- tRPC Router — 前端手动管理预览部署（查看列表、手动删除等）

## 6. Go 重写注意事项

### 可直接复用的部分

1. **GitHub API 调用格式**：Issue Comment 的 CRUD API path 和请求格式与语言无关
2. **域名生成算法**：通配符替换逻辑和 traefik.me 的 IP 嵌入模式（`IP.replaceAll(".", "-")`）
3. **PR Comment Markdown 模板**：表格格式的状态消息和安全阻止消息的完整 Markdown 文本
4. **Shell 命令**：克隆仓库命令、构建命令、`rm -rf` 清理命令
5. **环境变量注入模式**：`DOKPLOY_DEPLOY_URL` 的注入方式
6. **appName 生成规则**：`preview-{parentAppName}-{random6chars}`

### 需要重新实现的部分

1. **GitHub API 客户端**：TypeScript 使用 `@octokit/rest`，Go 可使用 `google/go-github` 库
2. **数据库事务与查询**：ORM 关联查询（`with`）需映射为 Go ORM 查询
3. **异步命令执行**：`execAsync`/`execAsyncRemote` 需映射为 Go 的 `os/exec` + SSH 客户端

### 架构优化建议

1. **Webhook 事件解耦**：考虑使用消息队列解耦 Webhook 接收和部署执行，避免 Webhook 超时
2. **状态机**：预览部署状态流转（`idle` -> `running` -> `done`/`error`）可以使用显式状态机，确保无效状态转换被拒绝
3. **资源清理容错**：当前清理操作逐步执行并 catch error 继续，Go 可以使用 `defer` + `multierr` 模式确保所有清理步骤都被执行
4. **Comment ID 持久化**：确保 `pullRequestCommentId` 更新的原子性。当前 `createPreviewDeploymentComment` 创建 comment 后更新 DB，如果 DB 更新失败会导致 comment 孤立
5. **过期清理**：`expiresAt` 字段已定义但未见使用，Go 版本可实现定时检查并清理过期的预览部署
