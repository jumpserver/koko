package srvconn

import (
	"bytes"
	"testing"
)

func TestPasswordMatchPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prompt string
		match  bool
	}{
		{name: "su english", prompt: "Password:", match: true},
		{name: "sudo ubuntu", prompt: "[sudo] password for ubuntu: ", match: true},
		{name: "password for user", prompt: "password for root:", match: true},
		{name: "possessive password", prompt: "ubuntu's Password:", match: true},
		{name: "chinese", prompt: "密码：", match: true},
		{name: "sudo chinese", prompt: "[sudo] ubuntu 的密码： ", match: true},
		{name: "ansi decorated", prompt: "\x1b[31m[sudo] password for ubuntu: \x1b[0m", match: true},
		{name: "embedded prompt", prompt: "PAM authentication: Password: ", match: true},
		{name: "password expired", prompt: "password has expired:", match: false},
		{name: "password required", prompt: "password is required:", match: false},
		{name: "authentication failed", prompt: "password authentication failed:", match: false},
	}

	config := SuConfig{}
	pattern := config.PasswordMatchPattern()
	service, err := NewSuService(&config, &switchUserTestConn{})
	if err != nil {
		t.Fatalf("compile password pattern %q: %v", pattern, err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.passwordRegexp.MatchString(tt.prompt); got != tt.match {
				t.Errorf("password prompt match = %t, want %t; prompt %q, pattern %q",
					got, tt.match, tt.prompt, pattern)
			}
		})
	}
}

func TestSuSwitchServiceInputsPasswordOnlyOnce(t *testing.T) {
	t.Parallel()

	conn := &switchUserTestConn{}
	config := SuConfig{
		MethodType:   SuMethodSudo,
		SudoUsername: "root",
		SudoPassword: "source-password",
	}
	service, err := NewSuService(&config, conn)
	if err != nil {
		t.Fatalf("create switch user service: %v", err)
	}

	prompt := []byte("[sudo] password for ubuntu: ")
	if status := service.handleResult(prompt); status != StatusMatch {
		t.Fatalf("first password prompt status = %v, want %v", status, StatusMatch)
	}
	if got, want := conn.String(), "source-password\r"; got != want {
		t.Fatalf("password input = %q, want %q", got, want)
	}
	if status := service.handleResult(prompt); status != StatusFailed {
		t.Fatalf("second password prompt status = %v, want %v", status, StatusFailed)
	}
	if got, want := conn.String(), "source-password\r"; got != want {
		t.Fatalf("password was input more than once: got %q, want %q", got, want)
	}
}

func TestSUMethodTypeIsSudo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method SUMethodType
		want   bool
	}{
		{method: SuMethodSudo, want: true},
		{method: SuMethodOnlySudo, want: true},
		{method: SuMethodSu, want: false},
		{method: SuMethodOnlySu, want: false},
	}
	for _, tt := range tests {
		if got := tt.method.IsSudo(); got != tt.want {
			t.Errorf("%s.IsSudo() = %t, want %t", tt.method, got, tt.want)
		}
	}
}

type switchUserTestConn struct {
	bytes.Buffer
}

func (c *switchUserTestConn) Close() error {
	return nil
}
