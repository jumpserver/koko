package proxy

import "strings"

var keyboardInteractiveMFAQuestionMarkers = []string{
	"otp",
	"one-time",
	"one time",
	"verification code",
	"verify code",
	"auth code",
	"authentication code",
	"passcode",
	"token",
	"dynamic code",
	"dynamic password",
	"secondary authentication",
	"secondary password",
	"second-factor",
	"second factor",
	"two-factor",
	"two factor",
	"2fa",
	"mfa",
	"验证码",
	"认证码",
	"校验码",
	"动态码",
	"动态密码",
	"动态口令",
	"令牌",
	"二次认证",
	"二次验证",
	"辅助认证",
	"辅助验证",
	"双因子",
}

var keyboardInteractivePasswordQuestionMarkers = []string{
	"password",
	"passwd",
	"密码",
}

var keyboardInteractiveCombinedQuestionMarkers = []string{
	"password + otp",
	"password+otp",
	"password and otp",
	"password followed by the otp",
	"密码 + 动态",
	"密码+动态",
	"密码和动态",
	"密码 + 验证码",
	"密码+验证码",
	"密码和验证码",
}

func isKeyboardInteractiveMFAQuestion(question string) bool {
	normalized := strings.ToLower(strings.TrimSpace(question))
	for _, marker := range keyboardInteractiveMFAQuestionMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func isKeyboardInteractiveCombinedPasswordMFAQuestion(question string) bool {
	normalized := strings.ToLower(strings.TrimSpace(question))
	for _, marker := range keyboardInteractiveCombinedQuestionMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func isKeyboardInteractivePasswordQuestion(question string) bool {
	normalized := strings.ToLower(strings.TrimSpace(question))
	if normalized == "" || isKeyboardInteractiveMFAQuestion(normalized) {
		return false
	}
	for _, marker := range keyboardInteractivePasswordQuestionMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func keyboardInteractiveEcho(echos []bool, index int) bool {
	return index >= 0 && index < len(echos) && echos[index]
}
func keyboardInteractivePrompt(question string) string {
	prompt := strings.TrimSpace(question)
	if prompt == "" {
		return "Authentication response: "
	}
	return prompt + " "
}

func keyboardInteractiveInstruction(instruction string) string {
	normalized := strings.ReplaceAll(instruction, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}
