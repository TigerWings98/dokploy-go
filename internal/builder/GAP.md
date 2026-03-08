# builder 模块 Gap 分析

## Go 当前实现 (1 文件, 150 行)
- GenerateBuildCommand: 根据 BuildType 分发到 6 种构建器
- 6 种构建器：nixpacks, dockerfile, heroku_buildpacks, paketo_buildpacks, railpack, static

## TS 原版实现 (10 文件)
- `index.ts` - 入口 getBuildCommand + mechanizeDockerContainer
- `nixpacks.ts` - Nixpacks 命令生成
- `dockerfile.ts` - Dockerfile 命令生成
- `heroku.ts` - Heroku Buildpacks 命令生成
- `paketo.ts` - Paketo Buildpacks 命令生成
- `railpack.ts` - Railpack 命令生成 (railpack prepare + docker buildx)
- `static.ts` - 静态站点 (生成 Dockerfile + nginx 配置)
- `compose.ts` - Compose 构建命令
- `drop.ts` - Drop 模式构建
- `upload-image.ts` - Registry 上传命令

## Gap 详情

### 已实现 ✅
1. 6 种构建类型的命令生成框架
2. 构建参数 (build-args) 支持
3. 构建密钥 (build-secrets) 支持 (dockerfile)
4. 清除缓存 (--no-cache/--clear-cache) 支持
5. Dockerfile 路径/上下文/多阶段构建支持

### 缺失功能 ❌
1. **Nixpacks 高级选项**: TS 版支持 `--install-cmd`、`--build-cmd`、`--start-cmd`、`--pkgs`、`--libs`、`--apt` 等参数，Go 版只支持 `--env`
2. **Railpack 完整流程**: TS 版是 `railpack prepare` + `docker buildx build`，Go 版只有 `docker buildx build`，缺少 `railpack prepare` 步骤
3. **Static 站点 Dockerfile 生成**: TS 版会动态生成 nginx Dockerfile (含 SPA 路由支持)，Go 版只有简单的 `docker build`
4. **Registry 上传命令**: TS 版 `uploadImageRemoteCommand` 生成 `docker tag && docker push` 命令，Go 版缺失
5. **Compose 构建**: TS 版有 compose 专用构建逻辑，Go 版缺失 (可能在 compose 模块)
6. **Drop 模式**: 拖拽上传代码的构建模式，Go 版未实现

## 影响评估
- **严重度**: 中等。基本构建可用，但 Nixpacks 高级选项和 Static 站点缺失会影响部分用户场景。
- **建议**: 优先补全 Nixpacks 高级选项和 Static Dockerfile 生成。
