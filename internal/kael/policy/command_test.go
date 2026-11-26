package policy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jumpserver/koko/internal/kael/domain"
)

func TestReadOnlyShell(t *testing.T) {
	for _, command := range []string{
		"df -h", "df -hT", "df -hT /", "du -sh /var/log", "du -sh /var/log/*",
		`du -sh "/var/log/my app"`, "/usr/bin/df -i", "'df' -h", "df -h && du -sh /var/log",
		"df -h; free -m", "du -sh /var/log/* 2>/dev/null | sort -hr | head -n 20",
		"du -sh /var/log/* 2>&1 | sort -k 1,1h -r", "df -h | grep -v tmpfs",
		"ls -alh /var/log", "ps aux | head -20", "uname -a; uptime; id; whoami; pwd",
		"cat /etc/os-release", "tail -n 50 /var/log/syslog", "stat /var/log", "readlink /var/log",
		"sort --human-numeric-sort --reverse /var/log/sizes", "sort -t: -k2,2n /etc/passwd",
		"cat /var/log/labels | sort | uniq -c", "hostname -f", "hostname -I",
		"du -sh /var/log || df -h", "du /var/log |& head -n 20",
	} {
		t.Run(command, func(t *testing.T) {
			if !readOnlyShell(command) {
				t.Fatal("read-only diagnostic requires approval")
			}
		})
	}
	for _, command := range []string{
		"", " ", "# df -h", "df -h # ; rm file", "df -h !!", `du "!!"`, "df -h |", `du "unterminated`, "df -h\x00; rm file", "df\nrm file",
		"df -h > /tmp/report", "df -h >> /tmp/report", "df -h &>/tmp/report", "df -h 2>/tmp/errors",
		"df -h >/dev/tcp/host/80", "df -h 3>/dev/null", "df -h {fd}>/dev/null", "df -h < /tmp/fifo",
		"df -h; rm file", "df -h && touch file", "df -h || reboot", "df -h | tee /tmp/report",
		"du $(touch /tmp/file)", "du `touch /tmp/file`", "du <(touch /tmp/file)", "du >(cat > /tmp/file)",
		`du "$HOME"`, "du $((x=1))", "du ${x:=value}", "du $'\\x2d'", `du $"/tmp"`,
		"du /{var,tmp}", "du ~/logs", "du \\", "df -h &", "(df -h)", "{ df -h; }",
		"if df -h; then rm file; fi", "for f in *; do du $f; done", "function df() { rm file; }; df -h",
		"PATH=/tmp df -h", "env df -h", "sudo df -h", "sh -c 'df -h'", "./df -h", "/tmp/df -h",
		"/usr/bin/../bin/df -h", "d? -h", "unknown --read-only", "curl https://example.test",
		"find /tmp -delete", "find /tmp -exec rm {} +", "sed -i s/a/b/ file", "awk 'BEGIN { system(\"id\") }'",
		"sort -o /tmp/report", "sort -ro/tmp/report", "sort --output=/tmp/report", "sort --compress-program=sh",
		"sort *", "sort --files0-from=files", "sort -k", "sort -t", "uniq input output", "uniq -c input output",
		"hostname new-name", "hostname -F /tmp/name", "hostname -F -d", "hostname --file=/tmp/name", "date -s 2026-01-01",
		strings.Repeat(" ", 64*1024) + "df",
	} {
		label := command
		if len(label) > 100 {
			label = "oversize"
		}
		t.Run(label, func(t *testing.T) {
			if readOnlyShell(command) {
				t.Fatal("unclassified or mutating command bypassed approval")
			}
		})
	}
}

func TestInvocationPolicyRequiresDeclaredTerminalPolicy(t *testing.T) {
	registration := domain.Registration{Name: "custom_shell", BindingKind: "panel", Namespace: "luna.terminal", Risk: "dangerous", RequiresConfirmation: true, AnnotationsJSON: json.RawMessage(`{"open_world":true,"command_policy":"shell-readonly-v1"}`)}
	for _, tc := range []struct {
		name string
		edit func(*domain.Registration)
		args string
		read bool
	}{
		{"diagnostic", func(*domain.Registration) {}, `{"command":"df -h","execution":"auto","timeout_seconds":600}`, true},
		{"mutation", func(*domain.Registration) {}, `{"command":"df -h; rm file"}`, false},
		{"unknown argument", func(*domain.Registration) {}, `{"command":"df -h","script":"rm file"}`, false},
		{"invalid json", func(*domain.Registration) {}, `{"command":"df -h"} {}`, false},
		{"missing command", func(*domain.Registration) {}, `{}`, false},
		{"service", func(r *domain.Registration) { r.BindingKind = "service" }, `{"command":"df -h"}`, false},
		{"namespace", func(r *domain.Registration) { r.Namespace = "luna.sql" }, `{"command":"df -h"}`, false},
		{"legacy", func(r *domain.Registration) { r.AnnotationsJSON = json.RawMessage(`{}`) }, `{"command":"df -h"}`, false},
		{"unknown policy", func(r *domain.Registration) { r.AnnotationsJSON = json.RawMessage(`{"command_policy":"v2"}`) }, `{"command":"df -h"}`, false},
		{"destructive", func(r *domain.Registration) {
			r.AnnotationsJSON = json.RawMessage(`{"command_policy":"shell-readonly-v1","destructive":true}`)
		}, `{"command":"df -h"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := registration
			tc.edit(&value)
			risk, confirmation := InvocationPolicy(value, json.RawMessage(tc.args))
			if (risk == "read") != tc.read || confirmation == tc.read {
				t.Fatalf("risk=%s confirmation=%t", risk, confirmation)
			}
		})
	}
}
