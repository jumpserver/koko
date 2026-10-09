package proxy

import (
	"os/exec"
	"testing"
)

func TestShellEscapeLiteralArgument(t *testing.T) {
	for _, input := range []string{"", "default", "a;b", "a&&b", "a|b", "$(printf injected)", "`printf injected`", "a'b", "a\nb", "<input", "(command)"} {
		out, err := exec.Command("bash", "-c", "printf '%s' "+shellEscape(input)).Output()
		if err != nil || string(out) != input {
			t.Errorf("literal argument %q: output %q, error %v", input, out, err)
		}
	}
}
