// Input: procedureRegistry, db (SSHKey 表)
// Output: registerSSHKeyTRPC - SSH Key 领域的 tRPC procedure 注册
// Role: SSH Key tRPC 路由注册，将 sshKey.* procedure 绑定到具体实现
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"crypto/x509"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/ssh"
)

func (h *Handler) registerSSHKeyTRPC(r procedureRegistry) {
	r["sshKey.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var keys []schema.SSHKey
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&keys)
		if keys == nil {
			keys = []schema.SSHKey{}
		}
		return keys, nil
	}

	r["sshKey.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			SSHKeyID string `json:"sshKeyId"`
		}
		json.Unmarshal(input, &in)
		var key schema.SSHKey
		if err := h.DB.First(&key, "\"sshKeyId\" = ?", in.SSHKeyID).Error; err != nil {
			return nil, &trpcErr{"SSH Key not found", "NOT_FOUND", 404}
		}
		return key, nil
	}

	r["sshKey.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var key schema.SSHKey
		json.Unmarshal(input, &key)
		key.OrganizationID = member.OrganizationID
		if err := h.DB.Create(&key).Error; err != nil {
			return nil, err
		}
		return key, nil
	}

	r["sshKey.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			SSHKeyID string `json:"sshKeyId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.SSHKey{}, "\"sshKeyId\" = ?", in.SSHKeyID)
		return true, nil
	}

	// sshKey.generate: 仅生成密钥对并返回，不保存到数据库
	// 与 TS 版一致：前端拿到 privateKey/publicKey 填入表单，用户点保存时走 sshKey.create
	r["sshKey.generate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Type string `json:"type"` // "rsa" 或 "ed25519"
		}
		json.Unmarshal(input, &in)

		var pubKeyStr, privKeyStr string

		switch in.Type {
		case "rsa":
			// 生成 4096 位 RSA 密钥（与 TS 版 ssh2 默认一致）
			rsaKey, err := rsa.GenerateKey(rand.Reader, 4096)
			if err != nil {
				return nil, &trpcErr{"Failed to generate RSA key: " + err.Error(), "INTERNAL_SERVER_ERROR", 500}
			}
			sshPub, err := ssh.NewPublicKey(&rsaKey.PublicKey)
			if err != nil {
				return nil, &trpcErr{"Failed to convert public key: " + err.Error(), "INTERNAL_SERVER_ERROR", 500}
			}
			pubKeyStr = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
			privPEM := pem.EncodeToMemory(&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
			})
			privKeyStr = string(privPEM)

		default:
			// 默认 ed25519（与 TS 版默认一致）
			pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return nil, &trpcErr{"Failed to generate ED25519 key: " + err.Error(), "INTERNAL_SERVER_ERROR", 500}
			}
			sshPub, err := ssh.NewPublicKey(pubKey)
			if err != nil {
				return nil, &trpcErr{"Failed to convert public key: " + err.Error(), "INTERNAL_SERVER_ERROR", 500}
			}
			pubKeyStr = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
			privKeyStr = string(marshalED25519PrivateKey(privKey))
		}

		// 只返回密钥对，不保存到数据库
		return map[string]string{
			"privateKey": privKeyStr,
			"publicKey":  pubKeyStr,
		}, nil
	}

	r["sshKey.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["sshKeyId"].(string)
		delete(in, "sshKeyId")

		var key schema.SSHKey
		if err := h.DB.First(&key, "\"sshKeyId\" = ?", id).Error; err != nil {
			return nil, &trpcErr{"SSH Key not found", "NOT_FOUND", 404}
		}
		h.DB.Model(&key).Updates(in)
		return key, nil
	}
}

// marshalED25519PrivateKey encodes an ed25519 private key in OpenSSH PEM format.
func marshalED25519PrivateKey(key ed25519.PrivateKey) []byte {
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: marshalOpenSSHED25519(key),
	})
	return privPEM
}

func marshalOpenSSHED25519(key ed25519.PrivateKey) []byte {
	pub := key.Public().(ed25519.PublicKey)
	var buf []byte

	buf = append(buf, []byte("openssh-key-v1\x00")...)
	buf = appendSSHString(buf, "none")
	buf = appendSSHString(buf, "none")
	buf = appendSSHString(buf, "")
	buf = appendUint32(buf, 1)

	pubBytes := marshalSSHED25519PubKey(pub)
	buf = appendSSHBytes(buf, pubBytes)

	checkInt := uint32(0x12345678)
	var privSection []byte
	privSection = appendUint32(privSection, checkInt)
	privSection = appendUint32(privSection, checkInt)
	privSection = appendSSHString(privSection, "ssh-ed25519")
	privSection = appendSSHBytes(privSection, pub)
	privSection = appendSSHBytes(privSection, key)
	privSection = appendSSHString(privSection, "")

	for i := 0; len(privSection)%8 != 0; i++ {
		privSection = append(privSection, byte(i+1))
	}

	buf = appendSSHBytes(buf, privSection)
	return buf
}

func marshalSSHED25519PubKey(pub ed25519.PublicKey) []byte {
	var buf []byte
	buf = appendSSHString(buf, "ssh-ed25519")
	buf = appendSSHBytes(buf, pub)
	return buf
}

func appendSSHString(buf []byte, s string) []byte {
	return appendSSHBytes(buf, []byte(s))
}

func appendSSHBytes(buf []byte, data []byte) []byte {
	buf = appendUint32(buf, uint32(len(data)))
	return append(buf, data...)
}

func appendUint32(buf []byte, v uint32) []byte {
	return append(buf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
