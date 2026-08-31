package srvconn

import (
	"os"
	"testing"
)

func TestAgentToolPathWithinRoot(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/", want: false},
		{path: "/tmp", want: true},
		{path: "/tmp/x.txt", want: true},
		{path: "x.txt", want: true},
		{path: "/tmp2/x.txt", want: false},
		{path: "/a.txt", want: false},
		{path: "/root/x.txt", want: false},
		{path: "/tmp/../root/x.txt", want: false},
		{path: "../root/x.txt", want: false},
	}
	for _, test := range tests {
		if got := agentToolPathWithinRoot("/tmp", test.path); got != test.want {
			t.Errorf("agentToolPathWithinRoot(/tmp, %q) = %v", test.path, got)
		}
	}
}

func TestAgentToolVirtualRootIsConfined(t *testing.T) {
	if !agentToolVirtualRootIsConfined("/tmp", "/tmp", "/tmp") {
		t.Fatal("virtual root mapped to the configured canonical root should be confined")
	}
	for _, roots := range [][3]string{
		{"/tmp", "/", "/"},
		{"/tmp", "/tmp", "/etc"},
		{"tmp", "/tmp", "/tmp"},
	} {
		if agentToolVirtualRootIsConfined(roots[0], roots[1], roots[2]) {
			t.Fatalf("mismatched roots %q were treated as confined", roots)
		}
	}
}

func TestAgentToolVirtualPathMapping(t *testing.T) {
	assetDir := &AssetDir{isFromWebTerminal: true}
	session := &SftpSession{SftpConn: &SftpConn{rootDirPath: "/tmp"}}
	for path, want := range map[string]string{
		"/":             "/tmp",
		"/report.txt":   "/tmp/report.txt",
		"../etc/passwd": "/tmp/etc/passwd",
		"/tmp/data.txt": "/tmp/tmp/data.txt",
	} {
		if got := assetDir.GetRealPath(session, path); got != want {
			t.Errorf("GetRealPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestVirtualSFTPPath(t *testing.T) {
	for realPath, want := range map[string]string{
		"/tmp":              "/",
		"/tmp/report.txt":   "/report.txt",
		"/tmp/tmp/data.txt": "/tmp/data.txt",
	} {
		got, ok := virtualSFTPPath("/tmp", realPath)
		if !ok || got != want {
			t.Errorf("virtualSFTPPath(/tmp, %q) = %q, %v; want %q, true", realPath, got, ok, want)
		}
	}
	if _, ok := virtualSFTPPath("/tmp", "/etc/passwd"); ok {
		t.Fatal("path outside the configured root was exposed as a virtual path")
	}
}

func TestCanonicalAgentToolPathWithinRoot(t *testing.T) {
	resolved := map[string]string{
		"/configured":      "/configured",
		"/configured/file": "/configured/file",
		"/configured/link": "/etc",
	}
	realPath := func(path string) (string, error) {
		if value, ok := resolved[path]; ok {
			return value, nil
		}
		return "", os.ErrNotExist
	}

	checks := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "existing path", target: "/configured/file", want: true},
		{name: "new path", target: "/configured/new/child", want: true},
		{name: "existing symlink escape", target: "/configured/link", want: false},
		{name: "new path below symlink escape", target: "/configured/link/new", want: false},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			canonical, got, err := canonicalAgentToolPathWithinRoot(
				"/configured", check.target, realPath,
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != check.want {
				t.Fatalf("within root = %v, want %v", got, check.want)
			}
			if got && canonical == "" {
				t.Fatal("safe path did not return its canonical target")
			}
		})
	}
}
