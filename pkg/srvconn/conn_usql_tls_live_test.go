package srvconn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in: real usql, generated connection arguments and private TLS fixtures.
func TestUSQLLiveBackendTLS(t *testing.T) {
	binary, dir := os.Getenv("USQL_SQL_TLS_TEST_BINARY"), os.Getenv("USQL_SQL_TLS_TEST_DIR")
	if binary == "" || dir == "" {
		t.Skip("set USQL_SQL_TLS_TEST_BINARY/DIR and test-only MySQL/PG ports")
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, schema := range []string{"mysql", "postgres"} {
		port, err := strconv.Atoi(os.Getenv("USQL_SQL_TLS_TEST_" + strings.ToUpper(schema) + "_PORT"))
		if err != nil || port < 1 {
			t.Fatal("test-only database port is required")
		}
		modes := []string{"verified", "mutual", "wrong-ca"}
		if schema == "postgres" {
			modes = append(modes, "verify-ca", "require-ca", "prefer-ca")
		}
		for _, mode := range modes {
			t.Run(schema+"/"+mode, func(t *testing.T) {
				opt := &sqlOption{Schema: schema, Host: "127.0.0.1", ServerName: "database.test", Port: port,
					UseSSL: true, SqlPgSSLMode: "verify-full", DBName: "tls_test", Password: "FixtureTLS@2026", CaCert: read("ca.crt"), Username: "root"}
				query := "SHOW SESSION STATUS LIKE 'Ssl_cipher'"
				if schema == "postgres" {
					opt.Username = "postgres"
					query = "SELECT ssl, version FROM pg_stat_ssl WHERE pid=pg_backend_pid()"
				}
				if mode == "mutual" {
					opt.Username, opt.ClientCert, opt.CertKey = "tls_user", read("client.crt"), read("client.key")
				}
				if mode == "wrong-ca" {
					opt.CaCert = read("wrong-ca.crt")
				}
				switch mode {
				case "verify-ca", "require-ca", "prefer-ca":
					opt.SqlPgSSLMode = strings.TrimSuffix(mode, "-ca")
					if mode == "verify-ca" {
						opt.SqlPgSSLMode = mode
					}
					opt.ServerName = "different-identity.example"
				}
				args, err := opt.USQLCommandArgs()
				if err != nil {
					t.Fatal(err)
				}
				defer opt.cleanupTLSFiles()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, append(args, "--no-init", "-A", "-t", "-c", query)...)
				cmd.Env = append(os.Environ(), "USQL_HISTORY=/dev/null")
				output, err := cmd.CombinedOutput()
				if mode == "wrong-ca" {
					if err == nil {
						t.Fatal("untrusted backend certificate was accepted")
					}
					return
				}
				if err != nil || !strings.Contains(string(output), "TLS") {
					t.Fatalf("CLI backend TLS failed: %s", strings.ReplaceAll(string(output), opt.Password, "[redacted]"))
				}
			})
		}
	}
}
