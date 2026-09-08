package proxy

import "testing"

func TestIsKeyboardInteractivePasswordQuestion(t *testing.T) {
	tests := []struct {
		name     string
		question string
		want     bool
	}{
		{name: "english password", question: "Password: ", want: true},
		{name: "english passwd", question: "Enter passwd", want: true},
		{name: "chinese password", question: "请输入密码：", want: true},
		{name: "otp", question: "OTP Code: ", want: false},
		{name: "combined password otp", question: "Password + OTP: ", want: false},
		{name: "combined chinese", question: "请输入密码和动态口令：", want: false},
		{name: "verification code", question: "Verification code: ", want: false},
		{name: "sangfor secondary password", question: "Secondary Authentication Password: ", want: false},
		{name: "chinese secondary verification", question: "请输入辅助验证密码：", want: false},
		{name: "token", question: "Token: ", want: false},
		{name: "empty", question: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isKeyboardInteractivePasswordQuestion(tt.question); got != tt.want {
				t.Fatalf("isKeyboardInteractivePasswordQuestion(%q) = %v, want %v",
					tt.question, got, tt.want)
			}
		})
	}
}

func TestIsKeyboardInteractiveCombinedPasswordMFAQuestion(t *testing.T) {
	tests := []struct {
		question string
		want     bool
	}{
		{question: "Password + OTP: ", want: true},
		{question: "请输入密码和动态口令：", want: true},
		{question: "Secondary Authentication Password: ", want: false},
		{question: "请输入辅助验证密码：", want: false},
		{question: "Password: ", want: false},
		{question: "OTP Code: ", want: false},
	}
	for _, tt := range tests {
		if got := isKeyboardInteractiveCombinedPasswordMFAQuestion(tt.question); got != tt.want {
			t.Fatalf("isKeyboardInteractiveCombinedPasswordMFAQuestion(%q) = %v, want %v",
				tt.question, got, tt.want)
		}
	}
}

func TestKeyboardInteractiveEcho(t *testing.T) {
	echos := []bool{true, false}
	if !keyboardInteractiveEcho(echos, 0) {
		t.Fatal("expected first question to echo")
	}
	if keyboardInteractiveEcho(echos, 1) {
		t.Fatal("expected second question not to echo")
	}
	if keyboardInteractiveEcho(echos, 2) {
		t.Fatal("missing echo value must default to false")
	}
}
func TestKeyboardInteractivePrompt(t *testing.T) {
	tests := []struct {
		question string
		want     string
	}{
		{question: "  Secondary Authentication Password: \r\n", want: "Secondary Authentication Password: "},
		{question: "OTP Code:", want: "OTP Code: "},
		{question: "  Verification code:  ", want: "Verification code: "},
		{question: "", want: "Authentication response: "},
	}
	for _, tt := range tests {
		if got := keyboardInteractivePrompt(tt.question); got != tt.want {
			t.Fatalf("keyboardInteractivePrompt(%q) = %q, want %q", tt.question, got, tt.want)
		}
	}
}
func TestKeyboardInteractiveInstruction(t *testing.T) {
	tests := []struct {
		instruction string
		want        string
	}{
		{instruction: "  Verify identity  ", want: "Verify identity"},
		{instruction: "First line\r\nSecond line\r\n", want: "First line\nSecond line"},
		{instruction: "First line\rSecond line", want: "First line\nSecond line"},
		{instruction: "\r\n\t", want: ""},
	}
	for _, tt := range tests {
		if got := keyboardInteractiveInstruction(tt.instruction); got != tt.want {
			t.Fatalf("keyboardInteractiveInstruction(%q) = %q, want %q", tt.instruction, got, tt.want)
		}
	}
}
