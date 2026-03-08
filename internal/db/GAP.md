# db 模块 Gap 分析

## Go 当前实现 (2 文件, 53 行)
- `db.go`: GORM PostgreSQL 连接 (Connect/Close)
- `migrate.go`: IsAdminPresent 检查 (查询 member 表 owner 角色)

## TS 原版实现 (db/index.ts + db/constants.ts)
- Drizzle ORM + postgres.js 驱动
- 开发环境全局缓存连接（防止 HMR 重复连接）
- 生产环境直接创建连接
- 导出 db 实例 + schema + 查询工具 (and, eq)

## Gap 详情

### 已实现 ✅
1. PostgreSQL 连接建立
2. 连接关闭
3. IsAdminPresent 检查（对应 TS 版首次启动判断）

### 缺失功能 ❌
1. **数据库迁移**: TS 版使用 Drizzle Kit 进行 schema 迁移（drizzle-kit push），Go 版无迁移工具
   - 影响：Go 版依赖已有数据库（由 TS 版创建的 schema），无法独立创建表
   - AutoMigrate 曾存在但已被移除（见 git log）
2. **连接池配置**: Go 版未配置连接池参数 (MaxOpenConns/MaxIdleConns/ConnMaxLifetime)
   - 影响：生产环境可能需要调优

### 设计差异 ⚠️
1. Go 用 GORM（全功能 ORM），TS 用 Drizzle（轻量 query builder + codegen schema）
   - GORM 提供更多运行时功能（hooks, preload, auto-migration）
   - Drizzle 依赖编译时 schema 生成
2. Go 版日志级别硬编码为 Info，TS 版无显式日志配置

## 影响评估
- **严重度**: 中等。连接功能完整，但缺少独立迁移能力，需依赖已有数据库 schema。
