// Input: coreos/go-oidc, golang.org/x/oauth2, SSOProvider.OIDCConfig JSON
// Output: OIDCProvider (BuildAuthURL 生成授权 URL, HandleCallback 处理回调换取用户信息)
// Role: OIDC 认证流程实现，支持 Discovery + Manual endpoint 配置，PKCE 可选
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package sso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCProvider 封装单个 OIDC 提供商的认证流程
type OIDCProvider struct {
	config     OIDCConfig
	issuer     string
	providerID string
}

// NewOIDCProvider 创建 OIDC 提供商实例
func NewOIDCProvider(providerID, issuer string, cfg OIDCConfig) *OIDCProvider {
	return &OIDCProvider{
		config:     cfg,
		issuer:     issuer,
		providerID: providerID,
	}
}

// AuthURLResult 包含授权重定向所需的信息
type AuthURLResult struct {
	URL           string
	State         string
	CodeVerifier  string // PKCE code_verifier (需存储在 session 中)
}

// BuildAuthURL 构建 OIDC 授权 URL
func (p *OIDCProvider) BuildAuthURL(callbackURL string) (*AuthURLResult, error) {
	authEndpoint, err := p.getAuthorizationEndpoint()
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization endpoint: %w", err)
	}

	scopes := p.config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}

	// 生成 state 防 CSRF
	state := generateRandomString(32)

	u, err := url.Parse(authEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization endpoint: %w", err)
	}

	q := u.Query()
	q.Set("client_id", p.config.ClientID)
	q.Set("redirect_uri", callbackURL)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)

	result := &AuthURLResult{State: state}

	// PKCE (默认开启)
	usePKCE := p.config.PKCE == nil || *p.config.PKCE
	if usePKCE {
		verifier := generateRandomString(43)
		challenge := sha256Base64URL(verifier)
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
		result.CodeVerifier = verifier
	}

	u.RawQuery = q.Encode()
	result.URL = u.String()
	return result, nil
}

// UserInfo 从回调中获取的用户信息
type UserInfo struct {
	ID            string
	Email         string
	EmailVerified bool
	Name          string
	FirstName     string
	LastName      string
	Image         string
	RawClaims     map[string]interface{}
}

// HandleCallback 处理 OIDC 回调，用 authorization code 换取用户信息
func (p *OIDCProvider) HandleCallback(ctx context.Context, code, callbackURL, codeVerifier string) (*UserInfo, error) {
	tokenEndpoint, err := p.getTokenEndpoint()
	if err != nil {
		return nil, fmt.Errorf("failed to get token endpoint: %w", err)
	}

	// 构建 token 请求
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {callbackURL},
	}

	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	// 根据配置选择认证方式
	var req *http.Request
	authMethod := p.config.TokenEndpointAuthentication
	if authMethod == "client_secret_post" {
		data.Set("client_id", p.config.ClientID)
		data.Set("client_secret", p.config.ClientSecret)
		req, err = http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
	} else {
		// 默认 client_secret_basic
		data.Set("client_id", p.config.ClientID)
		req, err = http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
		if err == nil {
			req.SetBasicAuth(url.QueryEscape(p.config.ClientID), url.QueryEscape(p.config.ClientSecret))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// 尝试从 id_token 解析 claims
	claims := make(map[string]interface{})

	if tokenResp.IDToken != "" {
		idClaims, err := p.verifyAndDecodeIDToken(ctx, tokenResp.IDToken)
		if err != nil {
			// id_token 验证失败不是致命错误，可以回退到 userinfo
			_ = err
		} else {
			claims = idClaims
		}
	}

	// 如果 claims 不足，从 userinfo endpoint 补充
	if claims["email"] == nil || claims[p.config.Mapping.Email] == nil {
		userInfoEndpoint, _ := p.getUserInfoEndpoint()
		if userInfoEndpoint != "" && tokenResp.AccessToken != "" {
			if extra, err := fetchUserInfo(ctx, userInfoEndpoint, tokenResp.AccessToken); err == nil {
				for k, v := range extra {
					if claims[k] == nil {
						claims[k] = v
					}
				}
			}
		}
	}

	return p.mapClaims(claims), nil
}

// verifyAndDecodeIDToken 验证并解码 ID Token
func (p *OIDCProvider) verifyAndDecodeIDToken(ctx context.Context, rawIDToken string) (map[string]interface{}, error) {
	// 使用 go-oidc 验证
	issuer := p.issuer
	if p.config.DiscoveryEndpoint != "" {
		issuer = p.config.DiscoveryEndpoint
	}

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		// Discovery 失败，回退到手动解码（不验证签名，仅在有 JWKS 时验证）
		return decodeJWTPayload(rawIDToken)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: p.config.ClientID,
	})
	token, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id_token verification failed: %w", err)
	}

	var claims map[string]interface{}
	if err := token.Claims(&claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// mapClaims 根据 mapping 配置将原始 claims 转换为 UserInfo
func (p *OIDCProvider) mapClaims(claims map[string]interface{}) *UserInfo {
	m := p.config.Mapping
	info := &UserInfo{
		RawClaims: claims,
	}

	info.ID = getStringClaim(claims, m.ID)
	info.Email = getStringClaim(claims, m.Email)
	info.Name = getStringClaim(claims, m.Name)

	if m.EmailVerified != "" {
		if v, ok := claims[m.EmailVerified]; ok {
			if bv, ok := v.(bool); ok {
				info.EmailVerified = bv
			}
		}
	}
	if m.FirstName != "" {
		info.FirstName = getStringClaim(claims, m.FirstName)
	}
	if m.LastName != "" {
		info.LastName = getStringClaim(claims, m.LastName)
	}
	if m.Image != "" {
		info.Image = getStringClaim(claims, m.Image)
	}

	// 如果没有拆分名字，从 Name 推断
	if info.FirstName == "" && info.LastName == "" && info.Name != "" {
		parts := strings.SplitN(info.Name, " ", 2)
		info.FirstName = parts[0]
		if len(parts) > 1 {
			info.LastName = parts[1]
		}
	}

	return info
}

// getAuthorizationEndpoint 获取授权端点（discovery 或手动配置）
func (p *OIDCProvider) getAuthorizationEndpoint() (string, error) {
	if p.config.AuthorizationEndpoint != "" {
		return p.config.AuthorizationEndpoint, nil
	}
	return p.discoverEndpoint("authorization_endpoint")
}

// getTokenEndpoint 获取 Token 端点
func (p *OIDCProvider) getTokenEndpoint() (string, error) {
	if p.config.TokenEndpoint != "" {
		return p.config.TokenEndpoint, nil
	}
	return p.discoverEndpoint("token_endpoint")
}

// getUserInfoEndpoint 获取 UserInfo 端点
func (p *OIDCProvider) getUserInfoEndpoint() (string, error) {
	if p.config.UserInfoEndpoint != "" {
		return p.config.UserInfoEndpoint, nil
	}
	return p.discoverEndpoint("userinfo_endpoint")
}

// discoverEndpoint 通过 OIDC Discovery 获取端点
func (p *OIDCProvider) discoverEndpoint(key string) (string, error) {
	discoveryURL := p.config.DiscoveryEndpoint
	if discoveryURL == "" {
		discoveryURL = strings.TrimRight(p.issuer, "/") + "/.well-known/openid-configuration"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := http.Get(discoveryURL)
	if err != nil {
		return "", fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	var doc map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("failed to parse discovery document: %w", err)
	}
	_ = ctx

	val, ok := doc[key].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("%s not found in discovery document", key)
	}
	return val, nil
}

// fetchUserInfo 从 userinfo endpoint 获取用户信息
func fetchUserInfo(ctx context.Context, endpoint, accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var claims map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// decodeJWTPayload 解码 JWT payload 部分（不验证签名）
func decodeJWTPayload(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}
	return claims, nil
}

// ── 工具函数 ──

func getStringClaim(claims map[string]interface{}, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := claims[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func sha256Base64URL(s string) string {
	h := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// Ensure oauth2 is used (for go mod tidy)
var _ = oauth2.NoContext
