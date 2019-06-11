package i18n

import (
	"path"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/jumpserver/koko/pkg/config"
)

func init() {
	cf := config.GetConf()
	localePath := path.Join(cf.RootPath, "locale")
	if strings.HasPrefix(cf.Language, "zh") {
		gotext.Configure(localePath, "zh_CN", "koko")
	} else {
		gotext.Configure(localePath, "en_US", "koko")
	}
}

func T(s string) string {
	return gotext.Get(s)
}
