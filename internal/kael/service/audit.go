package service

import (
	"regexp"
	"strings"
)

var auditSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bAKIA[A-Z0-9]{16}\b`),
	regexp.MustCompile(`(?i)\b(password|passphrase|secret|token|api[ _-]?key|authorization|cookie)\b\s*(?:is|[:=])\s*[^\s,;]+`),
	regexp.MustCompile(`(密码|口令|密钥|令牌|私钥)\s*(?:是|为|[:：=])\s*[^\s,，；;]+`),
}

func sanitizeAuditText(value string) string {
	result := value
	for _, pattern := range auditSecretPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return strings.TrimSpace(bounded(result, 32*1024))
}
