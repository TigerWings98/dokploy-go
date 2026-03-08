# builder

构建命令生成器，根据应用配置生成对应构建类型的 shell 命令字符串。
支持 6 种构建类型，生成的命令通过 process 模块执行，CLI 命令格式与原 TS 版完全一致。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| builder.go | 输入: Application 配置 / 输出: shell 命令字符串 | 6 种构建器命令生成：nixpacks/heroku/paketo/dockerfile/static/railpack + Registry 推送命令拼接 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
