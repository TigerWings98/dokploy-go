# config

环境变量配置加载和文件路径常量定义。
为整个系统提供统一的配置入口，兼容原 TS 版的 `/etc/dokploy/` 路径体系和环境变量命名。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| config.go | 输入: 环境变量 (DATABASE_URL, DOCKER_*, BETTER_AUTH_SECRET 等) / 输出: Config struct | 解析环境变量并构建配置对象，定义 Paths 路径体系 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
