package i18n

import (
	"github.com/leonelquinteros/gotext"
)

const (
	ZH LanguageCode = "zh_CN"
	EN LanguageCode = "en_US"
)

var (
	langMap = make(map[LanguageCode]*gotext.Locale)
)

type LanguageCode string

func (l LanguageCode) String() string {
	return string(l)
}

func (l LanguageCode) T(s string) string {
	if lang, ok := langMap[l]; ok {
		return lang.Get(s)
	}
	return s
}
