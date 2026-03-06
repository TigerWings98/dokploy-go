# 25. 安全与访问控制

## 1. 模块概述

安全与访问控制模块提供应用级别的 HTTP 安全中间件管理功能，主要包括两个子系统：

1. **BasicAuth 安全认证**：为应用添加 HTTP Basic Authentication 保护，通过 Traefik 中间件实现
2. **重定向规则（Redirect）**：为应用配置 URL 重定向规则，通过 Traefik RedirectRegex 中间件实现

两个子系统共享相同的架构模式：数据库 CRUD 操作与 Traefik 中间件配置文件的同步更新，且都支持本地和远程服务器两种部署场景。

**在系统中的角色：** 该模块是应用对外访问的安全网关配置层，直接操作 Traefik 反向代理的配置文件（YAML），为每个应用提供独立的安全策略和重定向规则。

## 2. 设计详解

### 2.1 BasicAuth 安全认证

#### 数据结构

```typescript
export const security = pgTable("security", {
  securityId: text("securityId").notNull().primaryKey(),
  username: text("username").notNull(),
  password: text("password").notNull(),  // 数据库中明文存储，写入 Traefik 时 bcrypt 加密
  createdAt: text("createdAt").notNull(),
  applicationId: text("applicationId").notNull()
    .references(() => applications.applicationId, { onDelete: "cascade" }),
}, (t) => ({
  unq: unique().on(t.username, t.applicationId),  // 同一应用下用户名唯一约束
}));
```

API Schema：

```typescript
export const apiCreateSecurity = {
  applicationId: z.string().min(1),
  username: z.string().min(1),
  password: z.string().min(1),
};

export const apiUpdateSecurity = {
  securityId: z.string().min(1),
  username: z.string().min(1),
  password: z.string().min(1),
};
```

#### 创建安全规则完整流程

```
createSecurity(data)
    |
    v
DB Transaction 开始:
    1. findApplicationById(data.applicationId)  -- 验证应用存在，获取 appName 和 serverId
    2. INSERT INTO security (username, password, applicationId) -- 写入数据库
    3. createSecurityMiddleware(application, securityResponse)
        |
        a. 加载全局 middlewares 配置文件
        |   - 本地: loadMiddlewares<FileConfig>()
        |   - 远程: await loadRemoteMiddlewares(serverId)
        |
        b. 构造中间件名称: "auth-{appName}"
        |
        c. bcrypt 加密密码: `${username}:${await bcrypt.hash(password, 10)}`
        |
        d. 更新中间件配置:
        |   - 如已有 auth-{appName} 中间件 -> 追加新用户到 basicAuth.users 数组
        |   - 如无该中间件 -> 创建新的 basicAuth 中间件:
        |     { basicAuth: { removeHeader: true, users: [user] } }
        |
        e. 加载应用 Traefik 配置文件
        |   - 本地: loadOrCreateConfig(appName)
        |   - 远程: await loadOrCreateConfigRemote(serverId, appName)
        |
        f. addMiddleware(appConfig, "auth-{appName}")
        |   -> 将中间件名添加到路由的 middlewares 列表
        |
        g. 写入两个配置文件（middlewares + app config）
            - 本地: writeMiddleware(config) + writeTraefikConfig(appConfig, appName)
            - 远程: writeTraefikConfigRemote(config, "middlewares", serverId)
                  + writeTraefikConfigRemote(appConfig, appName, serverId)
    |
DB Transaction 结束
```

#### Traefik BasicAuth 中间件生成的 YAML 结构

```yaml
# middlewares.yml
http:
  middlewares:
    auth-myapp:
      basicAuth:
        removeHeader: true    # 移除 Authorization 头，不传递给后端服务
        users:
          - "user1:$2b$10$hashedpassword1..."
          - "user2:$2b$10$hashedpassword2..."
```

#### 删除安全规则流程

```typescript
export const removeSecurityMiddleware = async (application, data) => {
  const middlewareName = `auth-${appName}`;
  // 从 middlewares 配置中查找
  const currentMiddleware = config.http.middlewares[middlewareName];

  if (isBasicAuthMiddleware(currentMiddleware)) {
    // 从 users 数组中过滤掉匹配 username 的用户
    const filteredUsers = users.filter(user => {
      const [username] = user.split(":");  // 按 : 分割取用户名
      return username !== data.username;
    });
    currentMiddleware.basicAuth.users = filteredUsers;

    // 如果移除后 users 为空，删除整个中间件
    if (filteredUsers.length === 0) {
      delete config.http.middlewares[middlewareName];
      deleteMiddleware(appConfig, middlewareName);  // 从路由中移除
      // 写入应用配置
    }
  }
  // 写入 middlewares 配置
};
```

#### 更新安全规则流程

更新操作采用"先删后加"策略，确保配置一致性：

```typescript
export const updateSecurityById = async (securityId, data) => {
  await db.transaction(async (tx) => {
    const securityResponse = await findSecurityById(securityId);
    const application = await findApplicationById(securityResponse.applicationId);
    // 1. 移除旧的 Traefik 中间件配置
    await removeSecurityMiddleware(application, securityResponse);
    // 2. 更新数据库记录
    const response = await tx.update(security).set(data).where(...).returning();
    // 3. 创建新的 Traefik 中间件配置
    await createSecurityMiddleware(application, response);
  });
};
```

#### 类型守卫

```typescript
const isBasicAuthMiddleware = (
  middleware: HttpMiddleware | undefined,
): middleware is { basicAuth: BasicAuthMiddleware } => {
  return !!middleware && "basicAuth" in middleware;
};
```

### 2.2 重定向规则（Redirect）

#### 数据结构

```typescript
export const redirects = pgTable("redirect", {
  redirectId: text("redirectId").notNull().primaryKey(),
  regex: text("regex").notNull(),          // 正则匹配模式，如 "^https?://www\\.(.+)"
  replacement: text("replacement").notNull(), // 替换目标，如 "https://${1}"
  permanent: boolean("permanent").notNull().default(false), // true=301永久重定向, false=302临时
  uniqueConfigKey: serial("uniqueConfigKey"), // 自增序号，确保中间件名称唯一
  createdAt: text("createdAt").notNull(),
  applicationId: text("applicationId").notNull()
    .references(() => applications.applicationId, { onDelete: "cascade" }),
});
```

API Schema：

```typescript
export const apiCreateRedirect = {
  regex: z.string().min(1),
  replacement: z.string().min(1),
  permanent: z.boolean().optional(),
  applicationId: z.string().min(1),
};

export const apiUpdateRedirect = {
  redirectId: z.string().min(1),
  regex: z.string().min(1),
  replacement: z.string().min(1),
  permanent: z.boolean().optional(),
};
```

#### Traefik RedirectRegex 中间件 YAML 结构

```yaml
# middlewares.yml
http:
  middlewares:
    redirect-myapp-1:       # 命名格式: redirect-{appName}-{uniqueConfigKey}
      redirectRegex:
        regex: "^https?://www\\.(.+)"
        replacement: "https://${1}"
        permanent: true     # true=301, false=302
    redirect-myapp-2:
      redirectRegex:
        regex: "^http://(.*)"
        replacement: "https://${1}"
        permanent: false
```

#### 创建重定向中间件

```typescript
export const createRedirectMiddleware = async (application, data) => {
  const { appName, serverId } = application;
  const config = serverId
    ? await loadRemoteMiddlewares(serverId)
    : loadMiddlewares<FileConfig>();

  const middlewareName = `redirect-${appName}-${data.uniqueConfigKey}`;
  const newMiddleware = {
    [middlewareName]: {
      redirectRegex: {
        regex: data.regex,
        replacement: data.replacement,
        permanent: data.permanent,
      },
    },
  };

  // 合并到现有中间件配置（使用 spread operator）
  if (config?.http) {
    config.http.middlewares = { ...config.http.middlewares, ...newMiddleware };
  }

  // 将中间件挂载到应用路由
  const appConfig = serverId
    ? await loadOrCreateConfigRemote(serverId, appName)
    : loadOrCreateConfig(appName);
  addMiddleware(appConfig, middlewareName);

  // 写入两个配置文件
};
```

#### 更新重定向中间件

更新操作直接修改现有中间件内容（不需要从路由中卸载再挂载）：

```typescript
export const updateRedirectMiddleware = async (application, data) => {
  const middlewareName = `redirect-${appName}-${data.uniqueConfigKey}`;
  // 仅当中间件存在时更新
  if (config?.http?.middlewares?.[middlewareName]) {
    config.http.middlewares[middlewareName] = {
      redirectRegex: {
        regex: data.regex,
        replacement: data.replacement,
        permanent: data.permanent,
      },
    };
  }
  // 仅写入 middlewares 文件，不需要修改应用配置
};
```

#### 删除重定向中间件

```typescript
export const removeRedirectMiddleware = async (application, data) => {
  const middlewareName = `redirect-${appName}-${data.uniqueConfigKey}`;
  // 从全局中间件配置中删除
  if (config?.http?.middlewares?.[middlewareName]) {
    delete config.http.middlewares[middlewareName];
  }
  // 从应用路由的 middlewares 列表中移除
  deleteMiddleware(appConfig, middlewareName);
  // 写入两个配置文件
};
```

### 2.3 本地与远程服务器配置管理

两个子系统都需要处理本地和远程服务器两种场景。核心模式如下：

```typescript
// 加载配置
if (serverId) {
    config = await loadRemoteMiddlewares(serverId);        // SSH 读取远程配置
    appConfig = await loadOrCreateConfigRemote(serverId, appName);
} else {
    config = loadMiddlewares<FileConfig>();                 // 本地文件系统读取
    appConfig = loadOrCreateConfig(appName);
}

// 修改配置...

// 写入配置
if (serverId) {
    await writeTraefikConfigRemote(config, "middlewares", serverId);
    await writeTraefikConfigRemote(appConfig, appName, serverId);
} else {
    writeMiddleware(config);
    writeTraefikConfig(appConfig, appName);
}
```

涉及两类配置文件：
1. **全局 middlewares 配置**：存放所有中间件定义（`middlewares.yml`）
2. **应用配置**：存放该应用的路由、服务和中间件引用列表（`{appName}.yml`）

### 2.4 中间件挂载/卸载

通过 `addMiddleware` 和 `deleteMiddleware` 函数管理应用路由配置中的 middleware 列表：

- `addMiddleware(appConfig, middlewareName)` — 将中间件名添加到路由的 `middlewares` 数组中
- `deleteMiddleware(appConfig, middlewareName)` — 从路由的 `middlewares` 数组中移除指定中间件名

这两个函数操作的是应用配置文件中 HTTP Router 的 `middlewares` 字段。

## 3. 源文件清单

### 数据库 Schema
- `dokploy/packages/server/src/db/schema/security.ts` — Security 表结构（`security` pgTable）、关系映射（`securityRelations`）、API Schema（`apiFindOneSecurity`、`apiCreateSecurity`、`apiUpdateSecurity`）
- `dokploy/packages/server/src/db/schema/redirects.ts` — Redirect 表结构（`redirects` pgTable）、关系映射（`redirectRelations`）、API Schema（`apiFindOneRedirect`、`apiCreateRedirect`、`apiUpdateRedirect`）

### 服务层（CRUD + Traefik 同步）
- `dokploy/packages/server/src/services/security.ts` — Security CRUD（`findSecurityById`、`createSecurity`、`deleteSecurityById`、`updateSecurityById`）
- `dokploy/packages/server/src/services/redirect.ts` — Redirect CRUD（`findRedirectById`、`createRedirect`、`removeRedirectById`、`updateRedirectById`）

### Traefik 中间件管理
- `dokploy/packages/server/src/utils/traefik/security.ts` — BasicAuth 中间件操作（`createSecurityMiddleware`、`removeSecurityMiddleware`、`isBasicAuthMiddleware`）
- `dokploy/packages/server/src/utils/traefik/redirect.ts` — RedirectRegex 中间件操作（`createRedirectMiddleware`、`updateRedirectMiddleware`、`removeRedirectMiddleware`）
- `dokploy/packages/server/src/utils/traefik/middleware.ts` — 通用中间件工具（`loadMiddlewares`、`loadRemoteMiddlewares`、`writeMiddleware`、`addMiddleware`、`deleteMiddleware`）
- `dokploy/packages/server/src/utils/traefik/application.ts` — 应用 Traefik 配置工具（`loadOrCreateConfig`、`loadOrCreateConfigRemote`、`writeTraefikConfig`、`writeTraefikConfigRemote`）
- `dokploy/packages/server/src/utils/traefik/file-types.ts` — TypeScript 类型定义（`FileConfig`、`HttpMiddleware`、`BasicAuthMiddleware`）

### tRPC 路由
- `dokploy/apps/dokploy/server/api/routers/security.ts` — Security tRPC 路由（CRUD + Traefik 同步）
- `dokploy/apps/dokploy/server/api/routers/redirects.ts` — Redirect tRPC 路由（CRUD + Traefik 同步）

## 4. 对外接口

### Security 服务

```typescript
findSecurityById(securityId: string): Promise<Security>

createSecurity(data: {
  applicationId: string;
  username: string;
  password: string;
}): Promise<boolean>

deleteSecurityById(securityId: string): Promise<Security>

updateSecurityById(securityId: string, data: Partial<{
  username: string;
  password: string;
}>): Promise<Security>
```

### Redirect 服务

```typescript
findRedirectById(redirectId: string): Promise<Redirect>

createRedirect(data: {
  regex: string;
  replacement: string;
  permanent?: boolean;
  applicationId: string;
}): Promise<boolean>

removeRedirectById(redirectId: string): Promise<Redirect>

updateRedirectById(redirectId: string, data: Partial<{
  regex: string;
  replacement: string;
  permanent: boolean;
}>): Promise<Redirect>
```

### Traefik 中间件操作

```typescript
// Security 中间件
createSecurityMiddleware(application: ApplicationNested, data: Security): Promise<void>
removeSecurityMiddleware(application: ApplicationNested, data: Security): Promise<void>

// Redirect 中间件
createRedirectMiddleware(application: ApplicationNested, data: Redirect): Promise<void>
updateRedirectMiddleware(application: ApplicationNested, data: Redirect): Promise<void>
removeRedirectMiddleware(application: ApplicationNested, data: Redirect): Promise<void>
```

`ApplicationNested` 类型包含关键字段：`appName: string`、`serverId: string | null`。

## 5. 依赖关系

### 上游依赖
- `bcrypt`（Node.js binding） — 密码 bcrypt 哈希（salt rounds = 10）
- Traefik 配置管理工具（`middleware.ts`、`application.ts`）：
  - `loadMiddlewares` / `loadRemoteMiddlewares` — YAML 配置文件加载解析
  - `loadOrCreateConfig` / `loadOrCreateConfigRemote` — 应用路由配置加载（不存在时创建空配置）
  - `writeTraefikConfig` / `writeTraefikConfigRemote` — YAML 配置文件序列化写入
  - `writeMiddleware` — 全局中间件文件写入
  - `addMiddleware` / `deleteMiddleware` — Router middlewares 数组操作
- Application 服务（`findApplicationById`）— 获取应用的 appName 和 serverId
- `drizzle-orm` — 数据库操作
- `@trpc/server`（`TRPCError`）— 结构化错误

### 下游被依赖
- tRPC Router — 前端调用的安全规则和重定向规则管理 API
- 应用部署/域名管理 — 部署时 Traefik 配置需要包含已有的中间件引用

## 6. Go 重写注意事项

### 可直接复用的部分

1. **Traefik 配置文件格式**：YAML 中间件结构是 Traefik 标准配置，与语言无关：
   - BasicAuth: `{ basicAuth: { removeHeader: true, users: ["user:bcrypt_hash"] } }`
   - RedirectRegex: `{ redirectRegex: { regex: "...", replacement: "...", permanent: bool } }`

2. **中间件命名约定**：
   - BasicAuth: `auth-{appName}`
   - Redirect: `redirect-{appName}-{uniqueConfigKey}`

3. **bcrypt 用户格式**：`{username}:{bcrypt_hash}` 是 Traefik 标准 BasicAuth 格式，兼容所有 bcrypt 实现

4. **配置文件读-改-写模式**：加载 YAML -> 修改内存结构 -> 序列化写回的模式

5. **唯一性约束**：`(username, applicationId)` 复合唯一索引

### 需要重新实现的部分

1. **bcrypt 库**：TypeScript 使用 `bcrypt`（Node.js C++ binding），Go 使用 `golang.org/x/crypto/bcrypt`：
   ```go
   hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
   user := fmt.Sprintf("%s:%s", username, string(hash))
   ```

2. **YAML 操作**：Go 使用 `gopkg.in/yaml.v3`

3. **远程文件操作**：SSH 远程读写需要 Go SSH 客户端（`golang.org/x/crypto/ssh`）

4. **数据库事务**：Drizzle ORM 事务映射为 Go ORM 事务（GORM `db.Transaction` 或 sqlc）

### 架构优化建议

1. **配置文件并发安全**：当前代码对 Traefik 配置文件的"读-改-写"操作没有加锁保护。如果两个并发请求同时修改同一个应用的安全规则，可能导致配置丢失。Go 版本建议：
   - 使用文件锁（`flock`）保护配置文件操作
   - 或使用 per-application 的互斥锁（`sync.Mutex` map）
   - 或考虑使用 Traefik 的 HTTP/Redis Provider 替代文件 Provider

2. **密码存储安全**：当前数据库中存储明文密码，仅在写入 Traefik 配置时 bcrypt 加密。建议：
   - 数据库中存储 bcrypt hash
   - 更新密码时重新生成 hash
   - 避免明文密码在日志/API 响应中泄露

3. **配置操作原子性**：当前创建安全规则需要写入两个配置文件（middlewares + app config）。如果第一个写入成功但第二个失败，会导致配置不一致。建议使用临时文件 + rename 的原子写入方式

4. **中间件注册表**：考虑在数据库中维护中间件注册表，追踪所有已创建的中间件及其关联应用，而非每次操作都重新解析 YAML 配置文件

```go
// 建议的 Go 接口设计
type TraefikConfigurator interface {
    LoadMiddlewares(serverId *string) (*FileConfig, error)
    WriteMiddlewares(config *FileConfig, serverId *string) error
    LoadAppConfig(appName string, serverId *string) (*FileConfig, error)
    WriteAppConfig(config *FileConfig, appName string, serverId *string) error
}

type SecurityService struct {
    db          *gorm.DB
    traefik     TraefikConfigurator
    appService  ApplicationService
    mu          sync.Map  // key: appName, value: *sync.Mutex (per-app lock)
}

func (s *SecurityService) Create(ctx context.Context, input CreateSecurityInput) error {
    app, err := s.appService.FindByID(ctx, input.ApplicationID)
    if err != nil {
        return err
    }
    // 获取 per-app 锁
    lock := s.getAppLock(app.AppName)
    lock.Lock()
    defer lock.Unlock()

    return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        sec := &Security{Username: input.Username, Password: input.Password, ApplicationID: input.ApplicationID}
        if err := tx.Create(sec).Error; err != nil {
            return err
        }
        return s.createSecurityMiddleware(app, sec)
    })
}
```
