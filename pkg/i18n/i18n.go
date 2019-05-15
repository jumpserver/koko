package i18n

import (
	"path"
	"strings"

	"github.com/leonelquinteros/gotext"

	"cocogo/pkg/config"
)

func init() {
	cf := config.GetConf()
	localePath := path.Join(cf.RootPath, "locale")
	if strings.HasPrefix(cf.Language, "zh") {
		gotext.Configure(localePath, "zh_CN", "coco")
	} else {
		gotext.Configure(localePath, "en_US", "coco")
	}
}

func T(s string) string {
	return gotext.Get(s)
}
