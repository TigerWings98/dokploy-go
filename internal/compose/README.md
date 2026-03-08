# compose

Docker Compose 文件转换工具。
负责为 Compose 文件中的服务名添加 suffix 和命名空间隔离，确保多实例部署不冲突。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| transform.go | 输入: 原始 Compose 文件内容 + appName / 输出: 转换后的 Compose 文件 | 服务名 suffix 注入、网络命名空间隔离、卷名前缀添加 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
