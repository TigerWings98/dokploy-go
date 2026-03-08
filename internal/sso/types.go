// Input: 无外部依赖
// Output: OIDCConfig/SAMLConfig 类型定义 + ClaimMapping
// Role: SSO 配置结构体定义，解析存储在 sso_provider 表中的 JSON 配置
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package sso

// OIDCConfig 对应前端传入的 OIDC 配置 JSON
type OIDCConfig struct {
	ClientID                    string        `json:"clientId"`
	ClientSecret                string        `json:"clientSecret"`
	DiscoveryEndpoint           string        `json:"discoveryEndpoint,omitempty"`
	SkipDiscovery               bool          `json:"skipDiscovery,omitempty"`
	AuthorizationEndpoint       string        `json:"authorizationEndpoint,omitempty"`
	TokenEndpoint               string        `json:"tokenEndpoint,omitempty"`
	UserInfoEndpoint            string        `json:"userInfoEndpoint,omitempty"`
	JWKSEndpoint                string        `json:"jwksEndpoint,omitempty"`
	TokenEndpointAuthentication string        `json:"tokenEndpointAuthentication,omitempty"` // "client_secret_post" | "client_secret_basic"
	Scopes                      []string      `json:"scopes,omitempty"`
	PKCE                        *bool         `json:"pkce,omitempty"`
	Mapping                     ClaimMapping  `json:"mapping"`
}

// SAMLConfig 对应前端传入的 SAML 配置 JSON
type SAMLConfig struct {
	EntryPoint            string          `json:"entryPoint"`
	Cert                  string          `json:"cert"`
	IDPMetadata           *IDPMetadata    `json:"idpMetadata,omitempty"`
	SPMetadata            *SPMetadata     `json:"spMetadata,omitempty"`
	WantAssertionsSigned  *bool           `json:"wantAssertionsSigned,omitempty"`
	AuthnRequestsSigned   *bool           `json:"authnRequestsSigned,omitempty"`
	SignatureAlgorithm    string          `json:"signatureAlgorithm,omitempty"`
	Mapping               ClaimMapping    `json:"mapping"`
}

// IDPMetadata 描述 SAML IdP 元数据
type IDPMetadata struct {
	Metadata             string              `json:"metadata,omitempty"`
	EntityID             string              `json:"entityID,omitempty"`
	SingleSignOnService  []SSOServiceBinding `json:"singleSignOnService,omitempty"`
}

// SPMetadata 描述 SAML SP 元数据
type SPMetadata struct {
	Metadata              string `json:"metadata,omitempty"`
	EntityID              string `json:"entityID,omitempty"`
	Binding               string `json:"binding,omitempty"`
	PrivateKey            string `json:"privateKey,omitempty"`
	IsAssertionEncrypted  bool   `json:"isAssertionEncrypted,omitempty"`
	EncPrivateKey         string `json:"encPrivateKey,omitempty"`
}

// SSOServiceBinding 描述 SAML SSO 服务绑定
type SSOServiceBinding struct {
	Binding  string `json:"Binding"`
	Location string `json:"Location"`
}

// ClaimMapping 配置 IdP claims/attributes 到用户字段的映射
type ClaimMapping struct {
	ID            string                 `json:"id"`
	Email         string                 `json:"email"`
	EmailVerified string                 `json:"emailVerified,omitempty"`
	Name          string                 `json:"name"`
	FirstName     string                 `json:"firstName,omitempty"`
	LastName      string                 `json:"lastName,omitempty"`
	Image         string                 `json:"image,omitempty"`
	ExtraFields   map[string]interface{} `json:"extraFields,omitempty"`
}
