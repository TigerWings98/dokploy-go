# provider 模块 Gap 分析

## Go 当前实现 (4 文件, 477 行)
- github.go, gitlab.go, gitea.go, bitbucket.go
- 每个文件提供 GetRepositories 和 GetBranches

## TS 原版实现 (7 文件)
- github.ts, gitlab.ts, gitea.ts, bitbucket.ts
- github-app.ts (GitHub App 认证)
- oauth.ts (OAuth 回调处理)
- webhook.ts (Webhook 事件处理)

## Gap 详情

### 已实现 ✅
1. GitHub 仓库/分支查询 (API v3)
2. GitLab 仓库/分支查询 (API v4)
3. Gitea 仓库/分支查询
4. Bitbucket 仓库/分支查询 (API v2)

### 缺失 ❌
1. **GitHub App 认证**: TS 版支持 GitHub App (JWT + Installation Token)，Go 版只支持 OAuth token
2. **OAuth 回调处理**: 用于 Git 提供商的 OAuth 授权流程
3. **分页支持**: 部分 API 可能需要分页，需确认 Go 版实现

## 影响评估
- **严重度**: 低。核心功能完整，GitHub App 是进阶功能。
