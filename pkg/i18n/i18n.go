package i18n

import (
	"path"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/jumpserver/koko/pkg/config"
)

func Initial()() {
	cf := config.GetConf()
	localePath := path.Join(cf.RootPath, "locale")
	if strings.HasPrefix(strings.ToLower(cf.LanguageCode), "en") {
		gotext.Configure(localePath, "en_US", "koko")
	} else {
		gotext.Configure(localePath, "zh_CN", "koko")
	}
}

func T(s string) string {
	return gotext.Get(s)
}
