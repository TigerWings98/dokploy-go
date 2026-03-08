// Input: sso.Service, SSOProvider 配置, Echo HTTP context
// Output: SSO 认证端点 (sign-in/sso, callback/oidc, callback/saml)
// Role: SSO 认证流程 HTTP handler，实现 email→domain→IdP 重定向 + OIDC/SAML 回调处理
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/sso"
	"github.com/labstack/echo/v4"
)

// ssoState 存储 SSO 认证流程中间态 (state → context)
// 生产环境应使用 Redis，这里用内存 map + 过期清理
type ssoStateEntry struct {
	ProviderID   string
	CallbackURL  string // 用户指定的成功后跳转 URL
	ACSCallback  string // 我们的 OIDC/SAML callback URL
	CodeVerifier string // PKCE verifier
}

var (
	ssoStates   = make(map[string]ssoStateEntry)
	ssoStatesMu sync.RWMutex
)

// registerSSOAuthRoutes 注册 SSO 认证相关的公开端点
// 挂载在 /api/auth/ 下
func (h *Handler) registerSSOAuthRoutes(g *echo.Group) {
	g.POST("/sign-in/sso", h.SSOSignIn)
	g.GET("/sso/callback/:providerId", h.SSOCallbackOIDC)
	g.POST("/sso/callback/:providerId", h.SSOCallbackOIDC) // 某些 IdP 用 POST
	g.POST("/sso/saml2/callback/:providerId", h.SSOCallbackSAML)
}

// SSOSignIn 处理 POST /api/auth/sign-in/sso
// 前端传入 { email, callbackURL }
// 返回 { url: "https://idp.example.com/authorize?..." } 或错误
func (h *Handler) SSOSignIn(c echo.Context) error {
	var req struct {
		Email       string `json:"email"`
		CallbackURL string `json:"callbackURL"`
	}
	if err := c.Bind(&req); err != nil {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid request")
	}

	if req.Email == "" {
		return h.betterAuthError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Email is required")
	}

	ssoSvc := sso.NewService(h.DB)

	// 通过 email domain 查找 SSO 提供商
	provider, err := ssoSvc.FindProviderByEmail(req.Email)
	if err != nil {
		return h.betterAuthError(c, http.StatusBadRequest, "SSO_PROVIDER_NOT_FOUND", err.Error())
	}

	// 构建 callback URL
	scheme := "https"
	if c.Request().TLS == nil {
		scheme = "http"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, c.Request().Host)

	// 判断 OIDC 还是 SAML
	if provider.OIDCConfig != nil && *provider.OIDCConfig != "" {
		return h.ssoSignInOIDC(c, provider, baseURL, req.CallbackURL)
	}
	if provider.SAMLConfig != nil && *provider.SAMLConfig != "" {
		return h.ssoSignInSAML(c, provider, baseURL, req.CallbackURL)
	}

	return h.betterAuthError(c, http.StatusBadRequest, "SSO_CONFIG_ERROR", "SSO provider has no OIDC or SAML configuration")
}

// ssoSignInOIDC 处理 OIDC SSO 登录
func (h *Handler) ssoSignInOIDC(c echo.Context, provider *schema.SSOProvider, baseURL, callbackURL string) error {
	cfg, err := sso.ParseOIDCConfig(provider)
	if err != nil {
		return h.betterAuthError(c, http.StatusInternalServerError, "SSO_CONFIG_ERROR", err.Error())
	}

	oidcProvider := sso.NewOIDCProvider(provider.ProviderID, provider.Issuer, *cfg)
	acsURL := fmt.Sprintf("%s/api/auth/sso/callback/%s", baseURL, provider.ProviderID)

	result, err := oidcProvider.BuildAuthURL(acsURL)
	if err != nil {
		return h.betterAuthError(c, http.StatusInternalServerError, "SSO_ERROR", err.Error())
	}

	// 存储 state
	ssoStatesMu.Lock()
	ssoStates[result.State] = ssoStateEntry{
		ProviderID:   provider.ProviderID,
		CallbackURL:  callbackURL,
		ACSCallback:  acsURL,
		CodeVerifier: result.CodeVerifier,
	}
	ssoStatesMu.Unlock()

	return c.JSON(http.StatusOK, map[string]interface{}{
		"url": result.URL,
	})
}

// ssoSignInSAML 处理 SAML SSO 登录
func (h *Handler) ssoSignInSAML(c echo.Context, provider *schema.SSOProvider, baseURL, callbackURL string) error {
	cfg, err := sso.ParseSAMLConfig(provider)
	if err != nil {
		return h.betterAuthError(c, http.StatusInternalServerError, "SSO_CONFIG_ERROR", err.Error())
	}

	samlProvider := sso.NewSAMLProvider(provider.ProviderID, *cfg)
	acsURL := fmt.Sprintf("%s/api/auth/sso/saml2/callback/%s", baseURL, provider.ProviderID)

	// 用 callbackURL 作为 RelayState 传递
	relayState := callbackURL

	redirectURL, err := samlProvider.BuildAuthURL(acsURL, relayState)
	if err != nil {
		return h.betterAuthError(c, http.StatusInternalServerError, "SSO_ERROR", err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"url": redirectURL,
	})
}

// SSOCallbackOIDC 处理 GET/POST /api/auth/sso/callback/:providerId
func (h *Handler) SSOCallbackOIDC(c echo.Context) error {
	providerID := c.Param("providerId")
	code := c.QueryParam("code")
	state := c.QueryParam("state")

	if code == "" || state == "" {
		// 检查 POST body
		code = c.FormValue("code")
		state = c.FormValue("state")
	}

	if code == "" {
		return c.String(http.StatusBadRequest, "Missing authorization code")
	}

	// 从 state 恢复上下文
	ssoStatesMu.Lock()
	entry, ok := ssoStates[state]
	if ok {
		delete(ssoStates, state)
	}
	ssoStatesMu.Unlock()

	if !ok {
		return c.String(http.StatusBadRequest, "Invalid or expired state parameter")
	}

	if entry.ProviderID != providerID {
		return c.String(http.StatusBadRequest, "Provider ID mismatch")
	}

	ssoSvc := sso.NewService(h.DB)

	// 查找提供商
	provider, err := ssoSvc.FindProviderByID(providerID)
	if err != nil {
		return c.String(http.StatusBadRequest, "SSO provider not found")
	}

	cfg, err := sso.ParseOIDCConfig(provider)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to parse OIDC config")
	}

	oidcProvider := sso.NewOIDCProvider(provider.ProviderID, provider.Issuer, *cfg)

	// 用 code 换取用户信息
	userInfo, err := oidcProvider.HandleCallback(c.Request().Context(), code, entry.ACSCallback, entry.CodeVerifier)
	if err != nil {
		return c.String(http.StatusInternalServerError, "OIDC callback failed: "+err.Error())
	}

	// 创建/链接用户 + 创建 session
	return h.completeSSO(c, ssoSvc, userInfo, provider.ProviderID, provider.OrganizationID, entry.CallbackURL)
}

// SSOCallbackSAML 处理 POST /api/auth/sso/saml2/callback/:providerId
func (h *Handler) SSOCallbackSAML(c echo.Context) error {
	providerID := c.Param("providerId")
	samlResponse := c.FormValue("SAMLResponse")
	relayState := c.FormValue("RelayState") // callbackURL

	if samlResponse == "" {
		return c.String(http.StatusBadRequest, "Missing SAMLResponse")
	}

	ssoSvc := sso.NewService(h.DB)

	provider, err := ssoSvc.FindProviderByID(providerID)
	if err != nil {
		return c.String(http.StatusBadRequest, "SSO provider not found")
	}

	cfg, err := sso.ParseSAMLConfig(provider)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to parse SAML config")
	}

	samlProvider := sso.NewSAMLProvider(provider.ProviderID, *cfg)
	userInfo, err := samlProvider.HandleCallback(samlResponse)
	if err != nil {
		return c.String(http.StatusInternalServerError, "SAML callback failed: "+err.Error())
	}

	return h.completeSSO(c, ssoSvc, userInfo, provider.ProviderID, provider.OrganizationID, relayState)
}

// completeSSO 完成 SSO 流程：创建/链接用户、建立 session、设置 cookie、重定向
func (h *Handler) completeSSO(c echo.Context, ssoSvc *sso.Service, userInfo *sso.UserInfo, providerID string, orgID *string, callbackURL string) error {
	// 创建或链接用户
	user, _, err := ssoSvc.CreateOrLinkUser(userInfo, providerID, orgID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to create user: "+err.Error())
	}

	// 创建 session
	session, token, err := ssoSvc.CreateSession(
		user.ID,
		c.RealIP(),
		c.Request().UserAgent(),
		orgID,
	)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to create session: "+err.Error())
	}

	// 设置 session cookie
	h.setSessionCookie(c, token, session.ExpiresAt)

	// 重定向到前端 callbackURL
	if callbackURL != "" {
		return c.Redirect(http.StatusFound, callbackURL)
	}

	// 没有 callbackURL 时返回 JSON（API 调用场景）
	return c.JSON(http.StatusOK, map[string]interface{}{
		"user":    h.buildUserResponse(user),
		"session": h.buildSessionResponse(session),
		"token":   token,
	})
}

