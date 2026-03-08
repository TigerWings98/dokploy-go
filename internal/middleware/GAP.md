# middleware 模块 Gap 分析

## Go 当前实现 (1 文件, 179 行)
- `auth.go`: Echo 中间件，实现认证和权限控制
  - AuthMiddleware: 标准 HTTP 401 错误
  - TRPCAuthMiddleware: tRPC 格式的 401 错误（code: -32001, UNAUTHORIZED）
  - AdminMiddleware: admin role 检查
  - 辅助函数: GetUser/GetSession/GetMember
  - publicProcedures 白名单机制（如 settings.health）
  - 批量请求支持（逗号分隔的 procedure names）

## TS 原版实现 (trpc.ts, 238 行)
- tRPC 原生中间件（procedure middleware），非 HTTP 中间件
  - publicProcedure: 无认证
  - protectedProcedure: 要求 session + user
  - adminProcedure: 要求 admin/owner role
  - cliProcedure: 同 adminProcedure（admin/owner）
  - enterpriseProcedure: 要求 admin/owner + 有效企业许可证
- createTRPCContext: 从请求中验证 session，注入 user（含 role/ownerId/enableEnterpriseFeatures）

## Gap 详情

### 已实现 ✅
1. Session token 认证（Cookie + Bearer）
2. API Key 认证（x-api-key header）
3. tRPC 格式错误响应（兼容前端 tRPC 客户端的 401 处理）
4. Admin/Owner 权限检查
5. Public procedure 白名单
6. 批量请求（comma-separated）的权限检查

### 缺失功能 ❌
1. **enterpriseProcedure**: TS 版有企业许可证验证中间件，Go 版在 handler 的 stub 中直接返回
   - 影响：企业功能本身是 stub，此中间件无实际需求
2. **cliProcedure**: TS 版有专用 CLI procedure 权限级别，Go 版用 AdminMiddleware 替代
   - 影响：功能等价

### 设计差异 ⚠️
1. TS 版是 tRPC procedure 级中间件（per-procedure），Go 版是 HTTP 路由级中间件（per-route-group）
   - Go 通过 publicProcedures 白名单模拟 publicProcedure 行为
   - 权限控制在 handler 层的具体 procedure 中补充检查
2. TS 版 context 包含 enableEnterpriseFeatures / isValidEnterpriseLicense 字段，Go 版未注入
   - 影响：企业功能为 stub，无实际影响
3. TS 版 user context 包含 ownerId 字段（通过 member 表查询），Go 版在 handler/trpc_routes.go 的 getDefaultMember 中补偿

## 影响评估
- **严重度**: 低。核心认证和权限控制已完整实现，差异均为设计层面的等价替代。
