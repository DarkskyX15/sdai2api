package config

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// Identifier 返回账号的唯一标识。
// SDAI 上游以 Bearer token 为唯一凭据：标识优先级为
// email/mobile（历史兼容）→ name → token 摘要前缀。
func (a Account) Identifier() string {
	if strings.TrimSpace(a.Email) != "" {
		return strings.TrimSpace(a.Email)
	}
	if mobile := NormalizeMobileForStorage(a.Mobile); mobile != "" {
		return mobile
	}
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	if token := strings.TrimSpace(a.Token); token != "" {
		sum := sha1.Sum([]byte(token))
		return "token:" + hex.EncodeToString(sum[:6])
	}
	return ""
}
