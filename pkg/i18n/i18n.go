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
	lowerCode := strings.ToLower(cf.LanguageCode)
	gotext.Configure(localePath, lowerCode, "koko")
	setupLangMap(localePath)
}

func setupLangMap(localePath string) {
	for _, code := range allLangCodes {
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
	if i18nCode, ok := i18nCodeMap[code]; ok {
		return i18nCode
	}
	return EN
}

func T(s string) string {
	return gotext.Get(s)
}
