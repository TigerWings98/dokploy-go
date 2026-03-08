# process 模块 Gap 分析

## Go 当前实现 (2 文件, 366 行)
- `exec.go`: ExecAsync（同步执行）、SpawnAsync（实时流式回调）
- `ssh.go`: SSH 远程命令执行

## TS 原版实现 (process/ 目录)
- `execAsync`: 执行命令并返回输出
- `execAsyncRemote`: 通过 SSH 在远程服务器执行
- `spawnAsync`: 实时流式执行 + 回调
- `spawnAsyncRemote`: 远程流式执行

## Gap 详情

### 已实现 ✅
1. ExecAsync - 本地同步执行
2. SpawnAsync - 本地流式执行
3. SSH 远程执行

### 需验证 ⚠️
1. SSH 密钥认证方式覆盖度（密码/密钥/密码保护的密钥）
2. 远程流式执行（SpawnAsync 的远程版本）
3. 错误处理格式是否与 TS 版 ExecError 兼容

## 影响评估
- **严重度**: 低。核心功能完整。
