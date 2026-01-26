package i18n

import (
	"github.com/leonelquinteros/gotext"
)

const (
	ZH     LanguageCode = "zh"
	EN     LanguageCode = "en"
	JA     LanguageCode = "ja"
	ZHHant LanguageCode = "zh_Hant"
	PtBr   LanguageCode = "pt_BR"
	Ko     LanguageCode = "ko"
	Ru     LanguageCode = "ru"
	Es     LanguageCode = "es"
	Vi     LanguageCode = "vi"
)

var (
	langMap = make(map[LanguageCode]*gotext.Locale)

	allLangCodes = []LanguageCode{ZH, EN, JA, ZHHant, PtBr, Ko, Ru, Es}

	AllLangCodesStr = []string{"English", "中文", "繁體中文", "日本語", "Português", "한국어", "Русский", "Español"}
	AllCodes        = []LanguageCode{EN, ZH, ZHHant, JA, PtBr, Ko, Ru, Es}
)

var i18nCodeMap = map[string]LanguageCode{
	"zh":      ZH,
	"en":      EN,
	"ja":      JA,
	"pt-br":   PtBr,
	"pt_br":   PtBr,
	"pt":      PtBr,
	"zh-cn":   ZH,
	"zh_cn":   ZH,
	"zh-hans": ZH,
	"zh-hant": ZHHant,
	"zh_hant": ZHHant,
	"ru":      Ru,
	"ko":      Ko,
	"es":      Es,
	"vi":      Vi,
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
