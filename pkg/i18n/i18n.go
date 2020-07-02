package i18n

import (
	"io/ioutil"
	"log"
	"path"

	"github.com/leonelquinteros/gotext"
)

func Initial(rootPath string) {
	localePath := path.Join(rootPath, "locale")
	dirs, err := ioutil.ReadDir(localePath)
	if err != nil {
		log.Printf("register i18n lang err: %s\n", err)
		return
	}
	for i := range dirs {
		local := gotext.NewLocale(localePath, dirs[i].Name())
		local.AddDomain(domName)
		register(LanguageCode(dirs[i].Name()), local)
		log.Println("register i18n lang: ", dirs[i].Name())
	}
	gotext.Configure(localePath, string(ZH), domName)
}
