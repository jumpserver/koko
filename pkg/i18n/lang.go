package i18n

import (
	"strings"

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

	allLangCodes = []LanguageCode{ZH, EN, JA, ZHHant, PtBr, Ko, Ru, Es, Vi}

	AllLangCodesStr = []string{"English", "中文", "繁體中文", "日本語", "Português", "한국어", "Русский", "Español", "Tiếng Việt"}
	AllCodes        = []LanguageCode{EN, ZH, ZHHant, JA, PtBr, Ko, Ru, Es, Vi}
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

// coreLangCodeMap koko 语言代码到 Core 语言代码的映射，仅列出无法直接转换的部分
var coreLangCodeMap = map[LanguageCode]string{
	ZH:     "zh-hans",
	ZHHant: "zh-hant",
	PtBr:   "pt-br",
}

type LanguageCode string

func (l LanguageCode) String() string {
	return string(l)
}

// CoreCode 返回 Core API 使用的语言代码
func (l LanguageCode) CoreCode() string {
	if code, ok := coreLangCodeMap[l]; ok {
		return code
	}
	return strings.ToLower(string(l))
}

func (l LanguageCode) T(s string) string {
	if lang, ok := langMap[l]; ok {
		return lang.Get(s)
	}
	return s
}
