package i18n

import (
	"path"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/jumpserver/koko/pkg/config"
)

func Initial() {
	cf := config.GetConf()
	localePath := path.Join(cf.RootPath, "locale")
	if strings.HasPrefix(strings.ToLower(cf.LanguageCode), "en") {
		gotext.Configure(localePath, "en_US", "koko")
	} else if strings.HasPrefix(strings.ToLower(cf.LanguageCode), "ja") {
		gotext.Configure(localePath, "ja_JP", "koko")
	} else {
		gotext.Configure(localePath, "zh_CN", "koko")
	}
	setupLangMap(localePath)
}

func setupLangMap(localePath string) {
	for _, code := range []LanguageCode{EN, ZH, JA} {
		enLocal := gotext.NewLocale(localePath, code.String())
		enLocal.AddDomain("koko")
		langMap[code] = enLocal
	}
}

func NewLang(code string) LanguageCode {
	code = strings.ToLower(code)
	if strings.Contains(code, "en") {
		return EN
	} else if strings.Contains(code, "ja") {
		return JA
	}
	return ZH
}

func T(s string) string {
	return gotext.Get(s)
}
