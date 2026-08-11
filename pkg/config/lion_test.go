package config

import (
	"net"
	"testing"
)

func TestSelectGuacdAddr(t *testing.T) {
	t.Run("fallback", func(t *testing.T) {
		cfg := Config{GuaHost: "127.0.0.1", GuaPort: "4822"}
		want := net.JoinHostPort(cfg.GuaHost, cfg.GuaPort)
		if got := cfg.SelectGuacdAddr(); got != want {
			t.Fatalf("SelectGuacdAddr() = %q, want %q", got, want)
		}
	})

	t.Run("configured addresses", func(t *testing.T) {
		cfg := Config{GuacdAddrs: "guacd-a:4822, guacd-b:4822"}
		got := cfg.SelectGuacdAddr()
		if got != "guacd-a:4822" && got != "guacd-b:4822" {
			t.Fatalf("SelectGuacdAddr() returned unexpected address %q", got)
		}
	})
}
