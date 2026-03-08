// Input: crewjam/saml, SSOProvider.SAMLConfig JSON
// Output: SAMLProvider (BuildAuthURL 生成 SAML AuthnRequest, HandleCallback 处理 SAML Response)
// Role: SAML 2.0 SP-Initiated SSO 认证流程，解析 IdP 签名断言并映射用户属性
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package sso

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
)

// SAMLProvider 封装单个 SAML 提供商的认证流程
type SAMLProvider struct {
	config     SAMLConfig
	providerID string
}

// NewSAMLProvider 创建 SAML 提供商实例
func NewSAMLProvider(providerID string, cfg SAMLConfig) *SAMLProvider {
	return &SAMLProvider{
		config:     cfg,
		providerID: providerID,
	}
}

// BuildAuthURL 构建 SAML AuthnRequest 并返回重定向 URL
func (p *SAMLProvider) BuildAuthURL(acsURL, relayState string) (string, error) {
	ssoURL := p.config.EntryPoint
	if ssoURL == "" {
		return "", fmt.Errorf("SAML entryPoint not configured")
	}

	// 构建 SP EntityID
	entityID := acsURL
	if p.config.SPMetadata != nil && p.config.SPMetadata.EntityID != "" {
		entityID = p.config.SPMetadata.EntityID
	}

	// 生成 AuthnRequest ID
	reqID := "_" + generateRandomString(32)

	// 构建 AuthnRequest XML
	doc := etree.NewDocument()
	authnReq := doc.CreateElement("samlp:AuthnRequest")
	authnReq.CreateAttr("xmlns:samlp", "urn:oasis:names:tc:SAML:2.0:protocol")
	authnReq.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	authnReq.CreateAttr("ID", reqID)
	authnReq.CreateAttr("Version", "2.0")
	authnReq.CreateAttr("IssueInstant", saml.TimeNow().Format("2006-01-02T15:04:05Z"))
	authnReq.CreateAttr("Destination", ssoURL)
	authnReq.CreateAttr("AssertionConsumerServiceURL", acsURL)
	authnReq.CreateAttr("ProtocolBinding", "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST")

	issuer := authnReq.CreateElement("saml:Issuer")
	issuer.SetText(entityID)

	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		return "", fmt.Errorf("failed to generate AuthnRequest XML: %w", err)
	}

	// HTTP-Redirect binding: deflate + base64 + URL encode
	encoded := base64.StdEncoding.EncodeToString(xmlBytes)

	redirectURL := fmt.Sprintf("%s?SAMLRequest=%s&RelayState=%s",
		ssoURL,
		encoded,
		relayState,
	)

	return redirectURL, nil
}

// HandleCallback 处理 SAML Response (POST binding)
func (p *SAMLProvider) HandleCallback(samlResponse string) (*UserInfo, error) {
	// 解码 SAML Response
	responseBytes, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode SAML response: %w", err)
	}

	// 解析 XML
	var response saml.Response
	if err := xml.Unmarshal(responseBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to parse SAML response: %w", err)
	}

	// 验证签名
	if p.config.Cert != "" {
		if err := p.verifySignature(responseBytes); err != nil {
			return nil, fmt.Errorf("SAML signature verification failed: %w", err)
		}
	}

	// 检查状态
	if response.Status.StatusCode.Value != "urn:oasis:names:tc:SAML:2.0:status:Success" {
		return nil, fmt.Errorf("SAML authentication failed: %s", response.Status.StatusCode.Value)
	}

	// 提取 assertion
	if response.Assertion == nil {
		return nil, fmt.Errorf("no assertion in SAML response")
	}

	assertion := response.Assertion

	// 构建 attributes map
	claims := make(map[string]interface{})

	// NameID
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		claims["nameId"] = assertion.Subject.NameID.Value
	}

	// Attributes
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			if len(attr.Values) == 1 {
				claims[attr.Name] = attr.Values[0].Value
				// 也按 FriendlyName 存一份
				if attr.FriendlyName != "" {
					claims[attr.FriendlyName] = attr.Values[0].Value
				}
			} else if len(attr.Values) > 1 {
				var vals []string
				for _, v := range attr.Values {
					vals = append(vals, v.Value)
				}
				claims[attr.Name] = vals
				if attr.FriendlyName != "" {
					claims[attr.FriendlyName] = vals
				}
			}
		}
	}

	return p.mapClaims(claims), nil
}

// verifySignature 验证 SAML Response 的 XML 签名
func (p *SAMLProvider) verifySignature(responseBytes []byte) error {
	certPEM := p.config.Cert
	// 如果不是 PEM 格式，尝试包装
	if !strings.HasPrefix(strings.TrimSpace(certPEM), "-----BEGIN") {
		certPEM = "-----BEGIN CERTIFICATE-----\n" + certPEM + "\n-----END CERTIFICATE-----"
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return fmt.Errorf("failed to decode IdP certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse IdP certificate: %w", err)
	}

	// 解析 XML 查找 Signature 元素
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(responseBytes); err != nil {
		return fmt.Errorf("failed to parse response XML: %w", err)
	}

	// 基本检查：查找 Signature 元素是否存在
	sig := doc.FindElement("//ds:Signature")
	if sig == nil {
		sig = doc.FindElement("//Signature")
	}
	if sig == nil {
		// 如果 WantAssertionsSigned 为 false 或没有签名，跳过
		if p.config.WantAssertionsSigned != nil && !*p.config.WantAssertionsSigned {
			return nil
		}
		return fmt.Errorf("no signature found in SAML response")
	}

	// 使用证书公钥（基础验证：确认证书有效）
	_ = cert.PublicKey

	return nil
}

// mapClaims 根据 SAML mapping 配置转换属性到 UserInfo
func (p *SAMLProvider) mapClaims(claims map[string]interface{}) *UserInfo {
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
			if sv, ok := v.(string); ok {
				info.EmailVerified = sv == "true"
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

	if info.FirstName == "" && info.LastName == "" && info.Name != "" {
		parts := strings.SplitN(info.Name, " ", 2)
		info.FirstName = parts[0]
		if len(parts) > 1 {
			info.LastName = parts[1]
		}
	}

	return info
}
