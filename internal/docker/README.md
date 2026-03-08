# docker

Docker SDK 封装，提供容器/服务/Registry 操作的统一接口。
使用官方 docker/docker/client SDK 替代原 TS 版的 Dockerode，支持本地和远程 Docker 引擎。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| client.go | 输入: Docker 连接配置 / 输出: Client struct | Docker 客户端封装：Service CRUD、容器查询、日志流、网络管理、镜像清理 |
| registry.go | 输入: Registry 配置 / 输出: 认证/推送结果 | Docker Registry 认证 + 镜像推送/拉取 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
