package srvconn

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSuUsernameIsSingleLiteralOperand(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"su", "sudo"} {
		body := "#!/bin/sh\nprintf '%s\\n' \"$@\"\n"
		if name == "sudo" {
			body = "#!/bin/sh\nexec \"$@\"\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, method := range []SUMethodType{SuMethodSu, SuMethodSudo, SuMethodOnlySu, SuMethodOnlySudo} {
		for _, username := range []string{"root", "a;b", "a'b", "$(printf injected)", "-ccommand"} {
			cfg := SuConfig{MethodType: method, SudoUsername: username}
			cmd := exec.Command("bash", "-c", cfg.SuCommand())
			cmd.Env = append(os.Environ(), "PATH="+dir)
			out, err := cmd.Output()
			prefix := "--\n"
			if method == SuMethodSu || method == SuMethodSudo {
				prefix = "-\n--\n"
			}
			if err != nil || string(out) != prefix+username+"\n" {
				t.Fatalf("%s/%q: %q %v", method, username, out, err)
			}
		}
	}
	for method, expected := range map[SUMethodType]string{SuMethodEnable: "enable", SuMethodSuper: "super 15", SuMethodSuperLevel: "super level-15"} {
		cfg := SuConfig{MethodType: method, SudoUsername: "ignored"}
		if cfg.SuCommand() != expected {
			t.Fatal("network device command changed")
		}
	}
}
