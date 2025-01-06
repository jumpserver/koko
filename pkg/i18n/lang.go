package i18n

import (
	"github.com/leonelquinteros/gotext"
)

const (
	ZH     LanguageCode = "zh"
	EN     LanguageCode = "en"
	JA     LanguageCode = "ja"
	ZHHant LanguageCode = "zh_hant"
	PtBr   LanguageCode = "pt_br"
)

var (
	langMap = make(map[LanguageCode]*gotext.Locale)

	allLangCodes = []LanguageCode{ZH, EN, JA, ZHHant, PtBr}
)

var i18nCodeMap = map[string]LanguageCode{
	"zh":      ZH,
	"en":      EN,
	"ja":      JA,
	"pt-br":   PtBr,
	"pt_br":   PtBr,
	"pt":      PtBr,
	"zh-cn":   ZH,
	"zh-hans": ZH,
	"zh-hant": ZHHant,
	"zh_hant": ZHHant,
}

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
