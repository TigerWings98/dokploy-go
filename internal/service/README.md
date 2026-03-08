# service

业务逻辑层，封装应用/Compose/数据库/预览部署的部署流水线。
当前仅包含需要复杂编排逻辑的 4 个服务，简单 CRUD 操作直接在 handler 层的 tRPC procedure 中完成。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| application.go | 输入: 应用 ID + 配置 / 输出: 部署结果 | 应用部署流水线：FindByID → 克隆代码 → 构建镜像 → 创建 Docker Service → 配置 Traefik |
| compose.go | 输入: Compose ID + 配置 / 输出: 部署结果 | Compose 部署流水线：拉取代码 → 转换 Compose 文件 → docker stack deploy |
| database.go | 输入: 数据库类型 + ID / 输出: 部署结果 | 数据库服务部署：生成 Docker Service 配置 → 创建服务 → 配置网络 |
| preview.go | 输入: PR 信息 / 输出: 预览部署实例 | PR 预览部署生命周期：创建隔离环境 → 部署 → PR 关闭时清理 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
