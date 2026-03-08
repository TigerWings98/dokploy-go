# git 模块 Gap 分析

## Go 当前实现 (1 文件, 335 行)
- CloneWithAuth: 多提供商认证克隆

## TS 原版实现 (providers/ 目录)
- GitHub/GitLab/Bitbucket/Gitea 各自的 clone 函数
- SSH key 克隆
- 通用 git clone

## Gap 详情

### 已实现 ✅
1. GitHub OAuth token 注入克隆
2. GitLab OAuth token 注入克隆
3. Bitbucket OAuth token 注入克隆
4. Gitea OAuth token 注入克隆
5. SSH key 认证克隆
6. 分支/提交检出

### 需验证 ⚠️
1. Drop 模式（直接上传代码）的处理
2. 子模块递归克隆
3. 浅克隆 (--depth 1) 的支持

## 影响评估
- **严重度**: 低。
