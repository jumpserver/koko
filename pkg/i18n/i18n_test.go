package i18n

import (
	"os"
	"testing"
)

func TestT(t *testing.T) {
	// please set KOKO_ROOT_PATH env, before test
	rootPath := os.Getenv("KOKO_ROOT_PATH")
	if rootPath == "" {
		t.Fatal("please set KOKO_ROOT_PATH before test i18n")
	}
	Initial(rootPath)
	lan := NewLanguage(ZH)
	t.Log(lan.T("Welcome to use JumpServer open source fortress system"))
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
