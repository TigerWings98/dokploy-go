# 16 - 域名与证书管理

## 1. 模块概述

域名与证书管理模块负责 Dokploy 中域名的 CRUD、Traefik 路由配置生成、TLS 证书管理（Let's Encrypt 自动签发和自定义证书）、DNS 验证以及 Traefik 反向代理的初始化和配置。

**核心职责：**
- 域名记录的增删改查与关联 Traefik 路由自动生成
- Let's Encrypt 证书解析器配置
- 自定义 TLS 证书文件管理
- Traefik 主配置与动态配置文件管理
- 国际化域名（IDN）到 Punycode 的转换
- DNS 解析验证与 CDN 检测
- Traefik Service/Container 的初始化与更新

## 2. 设计详解

### 2.1 域名 CRUD（Service 层）

#### 创建域名

```typescript
export const createDomain = async (input: z.infer<typeof apiCreateDomain>) => {
    const result = await db.transaction(async (tx) => {
        const domain = await tx.insert(domains).values({
            ...input,
            host: input.host?.trim(),
        }).returning();

        // 创建后立即生成 Traefik 路由
        if (domain.applicationId) {
            const application = await findApplicationById(domain.applicationId);
            await manageDomain(application, domain);
        }
        return domain;
    });
    return result;
};
```

域名创建使用事务：先入库，再调用 `manageDomain()` 生成 Traefik 配置。`host` 字段会自动 `trim()` 去除前后空白。

#### 生成 traefik.me 域名

```typescript
export const generateTraefikMeDomain = async (
    appName: string, userId: string, serverId?: string,
) => {
    if (serverId) {
        const server = await findServerById(serverId);
        return generateRandomDomain({ serverIp: server.ipAddress, projectName: appName });
    }
    const settings = await getWebServerSettings();
    return generateRandomDomain({ serverIp: settings?.serverIp || "", projectName: appName });
};
```

`traefik.me` 是一个通配符 DNS 服务，将 `*.{ip}.traefik.me` 解析到对应 IP。

#### DNS 验证

```typescript
export const validateDomain = async (
    domain: string, expectedIp?: string,
): Promise<{
    isValid: boolean;
    resolvedIp?: string;
    error?: string;
    isCloudflare?: boolean;
    cdnProvider?: string;
}> => {
    const cleanDomain = domain.replace(/^https?:\/\//, "").split("/")[0];
    const ips = await resolveDns(cleanDomain);

    // CDN 检测
    const cdnProvider = ips.map(ip => detectCDNProvider(ip)).find(p => p !== null);
    if (cdnProvider) {
        return { isValid: true, resolvedIp: ..., cdnProvider: cdnProvider.displayName, error: cdnProvider.warningMessage };
    }

    // IP 匹配验证
    if (expectedIp) {
        return { isValid: resolvedIps.includes(expectedIp), ... };
    }
    return { isValid: true, resolvedIp: ... };
};
```

支持 CDN 检测（Cloudflare 等），在 CDN 代理模式下 IP 不会直接匹配但仍认为有效。

### 2.2 Traefik 路由管理

#### manageDomain — 域名关联路由

```typescript
export const manageDomain = async (app: ApplicationNested, domain: Domain) => {
    const { appName } = app;
    // 加载或创建 Traefik 配置文件
    let config: FileConfig = app.serverId
        ? await loadOrCreateConfigRemote(app.serverId, appName)
        : loadOrCreateConfig(appName);

    // 命名约定
    const serviceName = `${appName}-service-${domain.uniqueConfigKey}`;
    const routerName = `${appName}-router-${domain.uniqueConfigKey}`;
    const routerNameSecure = `${appName}-router-websecure-${domain.uniqueConfigKey}`;

    // HTTP 路由（总是创建）
    config.http.routers[routerName] = await createRouterConfig(app, domain, "web");

    // HTTPS 路由（仅 https 启用时创建）
    if (domain.https) {
        config.http.routers[routerNameSecure] = await createRouterConfig(app, domain, "websecure");
    } else {
        delete config.http.routers[routerNameSecure];
    }

    // Service 配置
    config.http.services[serviceName] = createServiceConfig(appName, domain);

    // 路径中间件
    await createPathMiddlewares(app, domain);

    // 写入配置文件
    if (app.serverId) {
        await writeTraefikConfigRemote(config, appName, app.serverId);
    } else {
        writeTraefikConfig(config, appName);
    }
};
```

**命名约定：**
- Router: `{appName}-router-{uniqueConfigKey}`（HTTP）、`{appName}-router-websecure-{uniqueConfigKey}`（HTTPS）
- Service: `{appName}-service-{uniqueConfigKey}`
- `uniqueConfigKey` 是域名记录的自增数字标识

#### removeDomain — 删除路由

```typescript
export const removeDomain = async (application: ApplicationNested, uniqueKey: number) => {
    // 从配置中删除 router 和 service
    delete config.http.routers[`${appName}-router-${uniqueKey}`];
    delete config.http.routers[`${appName}-router-websecure-${uniqueKey}`];
    delete config.http.services[`${appName}-service-${uniqueKey}`];

    // 删除路径中间件
    await removePathMiddlewares(application, uniqueKey);

    // 如果没有剩余 router，删除整个配置文件
    if (Object.keys(config.http.routers).length === 0) {
        removeTraefikConfig(appName);
    } else {
        writeTraefikConfig(config, appName);
    }
};
```

### 2.3 Router 配置生成

```typescript
export const createRouterConfig = async (
    app: ApplicationNested, domain: Domain, entryPoint: "web" | "websecure",
) => {
    const { host, path, https, uniqueConfigKey, internalPath, stripPath } = domain;

    // Punycode 转换（IDN 支持）
    const punycodeHost = toPunycode(host);

    const routerConfig: HttpRouter = {
        rule: `Host(\`${punycodeHost}\`)${path !== null && path !== "/" ? ` && PathPrefix(\`${path}\`)` : ""}`,
        service: `${appName}-service-${uniqueConfigKey}`,
        middlewares: [],
        entryPoints: [entryPoint],
    };

    // 路径重写中间件
    if (internalPath && internalPath !== "/" && internalPath !== path) {
        routerConfig.middlewares.push(`addprefix-${appName}-${uniqueConfigKey}`);
    }

    // 路径剥离中间件
    if (stripPath && path && path !== "/") {
        routerConfig.middlewares.push(`stripprefix-${appName}-${uniqueConfigKey}`);
    }

    // HTTP -> HTTPS 重定向
    if (entryPoint === "web" && https) {
        routerConfig.middlewares = ["redirect-to-https"];
    }

    // 重定向规则和安全中间件（跳过 preview 类型域名）
    if ((entryPoint === "websecure" && https) || !https) {
        if (domain.domainType !== "preview") {
            for (const redirect of redirects) {
                routerConfig.middlewares.push(`redirect-${appName}-${redirect.uniqueConfigKey}`);
            }
        }
        if (security.length > 0) {
            routerConfig.middlewares.push(`auth-${appName}`);
        }
    }

    // TLS 配置
    if (entryPoint === "websecure") {
        if (certificateType === "letsencrypt") {
            routerConfig.tls = { certResolver: "letsencrypt" };
        } else if (certificateType === "custom" && domain.customCertResolver) {
            routerConfig.tls = { certResolver: domain.customCertResolver };
        } else if (certificateType === "none") {
            routerConfig.tls = undefined;
        }
    }

    return routerConfig;
};
```

#### Punycode 转换

```typescript
const toPunycode = (host: string): string => {
    try {
        return new URL(`http://${host}`).hostname;
    } catch {
        return host;
    }
};
```

利用 `URL` 构造函数自动将国际化域名转换为 ASCII（如 `xn--e1aybc.xn--p1ai`）。

> **[可复用]** Go 中可使用 `golang.org/x/net/idna` 包的 `idna.Lookup.ToASCII()` 实现。

### 2.4 证书管理

#### 创建自定义证书

```typescript
export const createCertificate = async (certificateData, organizationId) => {
    const certificate = await db.insert(certificates).values({
        ...certificateData, organizationId,
    }).returning();

    // 写入证书文件
    createCertificateFiles(certificate);
    return certificate;
};
```

#### 证书文件写入

```typescript
const createCertificateFiles = async (certificate: Certificate) => {
    const certDir = path.join(CERTIFICATES_PATH, certificate.certificatePath);
    const crtPath = path.join(certDir, "chain.crt");
    const keyPath = path.join(certDir, "privkey.key");

    // 生成 Traefik 配置
    const traefikConfig = {
        tls: {
            certificates: [{
                certFile: path.join(certDir, "chain.crt"),
                keyFile: path.join(certDir, "privkey.key"),
            }],
        },
    };
    const yamlConfig = stringify(traefikConfig);
    const configFile = path.join(certDir, "certificate.yml");

    if (certificate.serverId) {
        // 远程服务器：Base64 编码后通过 SSH 写入
        const command = `
            mkdir -p ${certDir};
            echo "${encodeBase64(certificate.certificateData)}" | base64 -d > "${crtPath}";
            echo "${encodeBase64(certificate.privateKey)}" | base64 -d > "${keyPath}";
            echo "${yamlConfig}" > "${configFile}";
        `;
        await execAsyncRemote(certificate.serverId, command);
    } else {
        // 本地：直接文件系统写入
        fs.mkdirSync(certDir, { recursive: true });
        fs.writeFileSync(crtPath, certificate.certificateData);
        fs.writeFileSync(keyPath, certificate.privateKey);
        fs.writeFileSync(configFile, yamlConfig);
    }
};
```

> **[可复用]** 远程证书写入的 Shell 命令（`mkdir -p`、`base64 -d`）可直接在 Go 中使用。

#### 删除证书

```typescript
export const removeCertificateById = async (certificateId: string) => {
    const certificate = await findCertificateById(certificateId);
    const certDir = path.join(CERTIFICATES_PATH, certificate.certificatePath);

    if (certificate.serverId) {
        await execAsyncRemote(certificate.serverId, `rm -rf ${certDir}`);
    } else {
        await removeDirectoryIfExistsContent(certDir);
    }

    await db.delete(certificates).where(eq(certificates.certificateId, certificateId));
};
```

### 2.5 Web Server Traefik 配置

#### 更新面板域名路由

```typescript
export const updateServerTraefik = (settings, newHost: string | null) => {
    const config = loadOrCreateConfig("dokploy");

    // 设置 router rule
    config.http.routers["dokploy-router-app"].rule = `Host(\`${newHost}\`)`;

    // 设置 service
    config.http.services["dokploy-service-app"] = {
        loadBalancer: {
            servers: [{ url: `http://dokploy:${process.env.PORT || 3000}` }],
            passHostHeader: true,
        },
    };

    if (https) {
        // 添加 HTTPS 重定向中间件和 websecure router
        if (certificateType === "letsencrypt") {
            config.http.routers["dokploy-router-app-secure"].tls = { certResolver: "letsencrypt" };
        }
    }

    if (newHost) {
        writeTraefikConfig(config, "dokploy");
    } else {
        removeTraefikConfig("dokploy");
    }
};
```

#### Let's Encrypt 邮箱更新

```typescript
export const updateLetsEncryptEmail = (newEmail: string | null) => {
    const configPath = join(MAIN_TRAEFIK_PATH, "traefik.yml");
    const config = parse(readFileSync(configPath, "utf8"));
    config.certificatesResolvers.letsencrypt.acme.email = newEmail;
    writeFileSync(configPath, stringify(config), "utf8");
};
```

### 2.6 Traefik 初始化

#### 默认 Traefik 配置

```typescript
export const getDefaultTraefikConfig = () => ({
    global: { sendAnonymousUsage: false },
    providers: {
        // 生产环境: swarm + docker + file
        swarm: { exposedByDefault: false, watch: true },
        docker: { exposedByDefault: false, watch: true, network: "dokploy-network" },
        file: { directory: "/etc/dokploy/traefik/dynamic", watch: true },
    },
    entryPoints: {
        web: { address: `:${TRAEFIK_PORT}` },       // 默认 80
        websecure: {
            address: `:${TRAEFIK_SSL_PORT}`,          // 默认 443
            http3: { advertisedPort: TRAEFIK_HTTP3_PORT },
            http: { tls: { certResolver: "letsencrypt" } },  // 生产环境
        },
    },
    api: { insecure: true },
    certificatesResolvers: {
        letsencrypt: {
            acme: {
                email: "test@localhost.com",
                storage: "/etc/dokploy/traefik/dynamic/acme.json",
                httpChallenge: { entryPoint: "web" },
            },
        },
    },
});
```

**端口配置（可通过环境变量覆盖）：**
- `TRAEFIK_PORT` — HTTP 端口，默认 80
- `TRAEFIK_SSL_PORT` — HTTPS 端口，默认 443
- `TRAEFIK_HTTP3_PORT` — HTTP/3 (QUIC) 端口，默认 443
- `TRAEFIK_VERSION` — Traefik 版本，默认 `3.6.7`

#### Standalone Traefik（容器模式）

```typescript
export const initializeStandaloneTraefik = async ({ env, serverId, additionalPorts }) => {
    // 使用 docker.createContainer 创建 Traefik 容器
    // 绑定: traefik.yml, dynamic 目录, docker.sock
    // 支持额外端口映射和 Dashboard (8080)
};
```

#### Swarm Traefik（Service 模式）

```typescript
export const initializeTraefikService = async ({ env, additionalPorts, serverId }) => {
    // 使用 docker.createService / service.update
    // 约束: node.role==manager
    // 端口: 80/tcp, 443/tcp, 443/udp (HTTP/3)
};
```

#### 默认中间件

```typescript
export const getDefaultMiddlewares = () => ({
    http: {
        middlewares: {
            "redirect-to-https": {
                redirectScheme: { scheme: "https", permanent: true },
            },
        },
    },
});
```

> **[可复用]** 所有 Traefik YAML 配置结构可直接在 Go 中使用相同的 YAML 输出。

## 3. 源文件清单

| 文件路径 | 说明 |
|----------|------|
| `packages/server/src/services/domain.ts` | 域名 CRUD、traefik.me 生成、DNS 验证 |
| `packages/server/src/services/certificate.ts` | 证书 CRUD、文件写入、远程部署 |
| `packages/server/src/utils/traefik/domain.ts` | Traefik 路由管理（manageDomain, removeDomain）、Router 配置生成、Punycode |
| `packages/server/src/utils/traefik/web-server.ts` | Web Server 面板路由、Let's Encrypt 邮箱、主配置读写 |
| `packages/server/src/setup/traefik-setup.ts` | Traefik 初始化（Standalone/Swarm）、默认配置、中间件、端口常量 |
| `packages/server/src/utils/traefik/application.ts` | 应用 Traefik 配置文件 CRUD（loadOrCreateConfig, writeTraefikConfig, removeTraefikConfig） |
| `packages/server/src/utils/traefik/middleware.ts` | 通用中间件工具（loadMiddlewares, writeMiddleware, addMiddleware, deleteMiddleware） |
| `packages/server/src/db/schema/domain.ts` | 域名表定义（domain pgTable）、API Schema |
| `packages/server/src/db/schema/certificate.ts` | 证书表定义（certificate pgTable）、API Schema |

## 4. 对外接口

### Domain Service

```typescript
export type Domain = typeof domains.$inferSelect

export const createDomain: (input: z.infer<typeof apiCreateDomain>) => Promise<Domain>
export const findDomainById: (domainId: string) => Promise<Domain>
export const findDomainsByApplicationId: (applicationId: string) => Promise<Domain[]>
export const findDomainsByComposeId: (composeId: string) => Promise<Domain[]>
export const updateDomainById: (domainId: string, data: Partial<Domain>) => Promise<Domain>
export const removeDomainById: (domainId: string) => Promise<Domain>
export const getDomainHost: (domain: Domain) => string
export const generateTraefikMeDomain: (appName: string, userId: string, serverId?: string) => Promise<string>
export const generateWildcardDomain: (appName: string, serverDomain: string) => string
export const validateDomain: (domain: string, expectedIp?: string) => Promise<ValidationResult>
```

### Certificate Service

```typescript
export type Certificate = typeof certificates.$inferSelect

export const findCertificateById: (certificateId: string) => Promise<Certificate>
export const createCertificate: (data, organizationId: string) => Promise<Certificate>
export const removeCertificateById: (certificateId: string) => Promise<Certificate[]>
```

### Traefik Domain Utils

```typescript
export const manageDomain: (app: ApplicationNested, domain: Domain) => Promise<void>
export const removeDomain: (application: ApplicationNested, uniqueKey: number) => Promise<void>
export const createRouterConfig: (app: ApplicationNested, domain: Domain, entryPoint: "web" | "websecure") => Promise<HttpRouter>
```

### Web Server Utils

```typescript
export const updateServerTraefik: (settings, newHost: string | null) => void
export const updateLetsEncryptEmail: (newEmail: string | null) => void
export const readMainConfig: () => string | null
export const writeMainConfig: (traefikConfig: string) => void
```

### Traefik Setup

```typescript
export const TRAEFIK_SSL_PORT: number
export const TRAEFIK_PORT: number
export const TRAEFIK_HTTP3_PORT: number
export const TRAEFIK_VERSION: string

export const initializeStandaloneTraefik: (options?: TraefikOptions) => Promise<void>
export const initializeTraefikService: (options: TraefikOptions) => Promise<void>
export const createDefaultServerTraefikConfig: () => void
export const createDefaultTraefikConfig: () => void
export const createDefaultMiddlewares: () => void
export const getDefaultTraefikConfig: () => string
export const getDefaultServerTraefikConfig: () => string
export const getDefaultMiddlewares: () => string
```

## 5. 依赖关系

### 上游依赖

| 依赖 | 用途 |
|------|------|
| `drizzle-orm` + 数据库 | 域名和证书 CRUD |
| `yaml` (npm 包) | Traefik YAML 配置解析/生成 |
| `dockerode` | Traefik 容器/Service 创建与更新 |
| `node:dns` | DNS 域名解析验证 |
| `node:fs` | 本地配置文件读写 |
| `services/application` | findApplicationById（域名关联应用查询） |
| `services/server` | findServerById（远程服务器 IP 获取） |
| `services/cdn` | detectCDNProvider（CDN 检测） |
| `services/web-server-settings` | getWebServerSettings（面板设置） |
| `utils/traefik/application` | loadOrCreateConfig, writeTraefikConfig, removeTraefikConfig 等 |
| `utils/traefik/middleware` | createPathMiddlewares, removePathMiddlewares |
| `utils/process/execAsync` | execAsyncRemote（远程命令执行） |
| `utils/docker/utils` | encodeBase64 |
| `@dokploy/server/constants` | paths（路径常量） |
| `@dokploy/server/templates` | generateRandomDomain |

### 下游消费者

- **tRPC API 路由** — 域名/证书管理接口
- **应用部署模块** — 部署完成后自动更新 Traefik 路由
- **系统初始化** — 启动时调用 Traefik 初始化函数
- **Web 服务器设置** — 面板域名和 HTTPS 配置

## 6. Go 重写注意事项

### 可复用的 Traefik 配置结构

Traefik 配置文件为标准 YAML，Go 中可定义对应的 struct 并使用 `gopkg.in/yaml.v3` 序列化：

```go
type TraefikConfig struct {
    Global              *GlobalConfig              `yaml:"global,omitempty"`
    Providers           ProvidersConfig            `yaml:"providers"`
    EntryPoints         map[string]EntryPoint      `yaml:"entryPoints"`
    API                 *APIConfig                 `yaml:"api,omitempty"`
    CertificatesResolvers map[string]CertResolver  `yaml:"certificatesResolvers,omitempty"`
}

type FileConfig struct {
    HTTP *HTTPConfig `yaml:"http,omitempty"`
    TLS  *TLSConfig  `yaml:"tls,omitempty"`
}
```

### 可复用的 Shell 命令

```bash
# 远程证书部署
mkdir -p {certDir}
echo "{base64cert}" | base64 -d > "{certDir}/chain.crt"
echo "{base64key}" | base64 -d > "{certDir}/privkey.key"

# 远程证书删除
rm -rf {certDir}
```

### Punycode / IDN

Go 中使用 `golang.org/x/net/idna` 包：

```go
import "golang.org/x/net/idna"

func toPunycode(host string) string {
    ascii, err := idna.Lookup.ToASCII(host)
    if err != nil {
        return host
    }
    return ascii
}
```

### DNS 验证

Go 的 `net` 包提供了 `net.LookupHost()` / `net.LookupIP()` 函数，可直接替代 Node.js 的 `dns.resolve4`。

### Traefik 路由规则

Traefik 的路由规则字符串（如 `` Host(`example.com`) && PathPrefix(`/api`) ``）是 Traefik 专有语法，Go 版本需要生成相同格式的字符串。

### 证书文件路径约定

```
{CERTIFICATES_PATH}/{certificatePath}/
    chain.crt          # 证书链
    privkey.key        # 私钥
    certificate.yml    # Traefik TLS 配置
```

```
{DYNAMIC_TRAEFIK_PATH}/
    {appName}.yml      # 每个应用的路由配置
    middlewares.yml     # 全局中间件
    acme.json           # Let's Encrypt 自动证书存储
```

### Traefik 初始化两种模式

- **Standalone（容器）** — 适用于远程服务器，使用 `docker.createContainer`
- **Swarm（Service）** — 适用于主节点，使用 `docker.createService`，约束到 manager 节点

Go 中两种模式均可通过 Docker SDK 实现，核心差异在于 `ContainerCreate` vs `ServiceCreate` API。

### Let's Encrypt 配置

Let's Encrypt 使用 HTTP-01 验证（通过 `web` entrypoint），acme.json 文件权限需为 600。这些都是 Traefik 内部处理的，Go 版本只需正确生成配置即可。
