# backup 模块 Gap 分析

## Go 当前实现 (2 文件, 487 行)
- `backup.go`: 数据库备份（5 种数据库 dump 命令 + RClone 上传）
- `restore.go`: 备份恢复（RClone 下载 + restore 命令）+ 文件列表

## TS 原版实现 (8 个文件)
- `index.ts` - 入口
- `mysql.ts` / `postgres.ts` / `mariadb.ts` / `mongo.ts` - 各数据库备份/恢复
- `utils.ts` - RClone 配置生成
- `destination.ts` - 目的地连接测试
- `volume.ts` - 卷备份

## Gap 详情

### 已实现 ✅
1. 5 种数据库的 dump 命令生成
2. RClone 上传到 S3
3. 备份文件列表查询
4. 备份恢复

### 需验证 ⚠️
1. RClone 配置文件生成格式是否与 TS 版一致
2. 远程服务器上的备份（通过 SSH 执行）
3. 卷备份逻辑是否完整

## 影响评估
- **严重度**: 低。核心备份/恢复功能完整。
