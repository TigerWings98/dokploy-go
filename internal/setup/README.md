# setup

服务器初始化逻辑，负责系统启动时的基础设施配置和远程服务器部署。
覆盖目录创建、Docker Swarm 初始化、Traefik 部署、网络配置等。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| setup.go | 输入: Config / 输出: 初始化结果 | 本地服务器初始化：创建 /etc/dokploy/ 目录结构、生成默认 Traefik 配置、初始化 Docker 网络 |
| swarm.go | 输入: Docker Client / 输出: Swarm 初始化结果 | Docker Swarm 初始化：init/join Swarm、部署 Traefik 服务 |
| remote.go | 输入: Server 配置 + SSH 连接 / 输出: 远程服务器配置结果 | 远程服务器设置：通过 SSH 安装 Docker、配置 Traefik、加入 Swarm 集群 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
