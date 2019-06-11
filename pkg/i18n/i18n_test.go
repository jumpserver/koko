package i18n

import (
	"fmt"
	"os"
	"testing"
)

func TestT(t *testing.T) {
	//loc := gotext.NewLocale("./locale", "zh_CN")
	//loc.AddDomain("koko")
	fmt.Println(T("Welcome to use Jumpserver open source fortress system"))
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
