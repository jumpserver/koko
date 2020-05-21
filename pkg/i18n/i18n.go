package i18n

import (
	"io/ioutil"
	"log"
	"path"

	"github.com/leonelquinteros/gotext"
)

var langMaps = make(map[langValue]*gotext.Locale)

func Initial(rootPath string) () {
	localePath := path.Join(rootPath, "locale")
	dirs, err := ioutil.ReadDir(localePath)
	if err != nil {
		log.Printf("register i18n lang err: %s\n", err)
		return
	}
	for i := range dirs {
		local := gotext.NewLocale(localePath, dirs[i].Name())
		local.AddDomain(domName)
		register(dirs[i].Name(), local)
	}

}

func register(name string, locText *gotext.Locale) {
	langMaps[langValue(name)] = locText
}

const domName = "koko"

var defaultLang = Language(ZH)

func T(s string) string {
	return defaultLang.T(s)
}

type langValue string

const (
	ZH langValue = "zh_CN"
	EN           = "en_US"
)

func NewLanguage(lanType langValue) Language {
	return Language(lanType)
}

type Language string

func (l Language) T(s string) string {
	if lang, ok := langMaps[langValue(l)]; ok {
		return lang.Get(s)
	}
	return s
}
