# db

GORM PostgreSQL 数据库连接管理和数据表模型定义。
兼容原 TS 版 Drizzle ORM 的表结构（camelCase 列名），通过 GORM struct tag 精确映射。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| db.go | 输入: DATABASE_URL / 输出: *DB (GORM 连接包装) | 建立 PostgreSQL 连接，提供 GORM 操作入口 |
| migrate.go | 输入: GORM DB / 输出: 迁移结果 | 数据库迁移工具（开发环境用） |
| schema/*.go | 输入: 无 / 输出: 全部数据表 struct 定义 (~65 struct) | 见 schema/ 子目录 README |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
