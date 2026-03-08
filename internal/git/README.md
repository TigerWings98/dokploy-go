# git

Git 代码克隆，支持多提供商认证方式。
根据应用的 sourceType 自动选择认证方式（OAuth token/SSH key），通过 git CLI 执行克隆操作。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| clone.go | 输入: 应用配置 (仓库URL + 分支 + 认证信息) / 输出: 克隆到本地的代码目录 | 多提供商认证克隆：GitHub/GitLab/Bitbucket/Gitea OAuth token 注入、SSH key 认证、自定义 Git 仓库 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
