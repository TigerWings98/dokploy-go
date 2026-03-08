# provider

Git 提供商 API 客户端，封装 GitHub/GitLab/Gitea/Bitbucket 的 REST API 调用。
提供仓库列表、分支列表查询，以及 Webhook 事件解析能力。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| github.go | 输入: OAuth token / 输出: 仓库/分支列表 | GitHub API v3 调用：列出用户仓库、获取分支列表 |
| gitlab.go | 输入: OAuth token / 输出: 仓库/分支列表 | GitLab API v4 调用：列出用户项目、获取分支列表 |
| gitea.go | 输入: OAuth token + 实例 URL / 输出: 仓库/分支列表 | Gitea API 调用：列出用户仓库、获取分支列表 |
| bitbucket.go | 输入: OAuth token / 输出: 仓库/分支列表 | Bitbucket API v2 调用：列出用户仓库、获取分支列表 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
