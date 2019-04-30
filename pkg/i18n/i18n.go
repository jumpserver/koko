package i18n

import (
	"path"
	"strings"

	"github.com/leonelquinteros/gotext"

	"cocogo/pkg/config"
)

func init() {
	localePath := path.Join(config.Conf.RootPath, "locale")
	if strings.HasPrefix(config.Conf.Language, "zh") {
		gotext.Configure(localePath, "zh_CN", "coco")
	} else {
		gotext.Configure(localePath, "en_US", "coco")
	}
}

func T(s string) string {
	return gotext.Get(s)
}
