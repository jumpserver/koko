package i18n

import "github.com/leonelquinteros/gotext"

const domName = "koko"

var langMaps = make(map[LanguageCode]*gotext.Locale)

func register(name LanguageCode, locText *gotext.Locale) {
	langMaps[name] = locText
}

func NewLanguage(code LanguageCode) Language {
	return Language{code}
}

type Language struct {
	code LanguageCode
}

func (l Language) T(s string) string {
	if lang, ok := langMaps[l.code]; ok {
		return lang.Get(s)
	}
	return s
}

func (l Language) Code() LanguageCode {
	return l.code
}
