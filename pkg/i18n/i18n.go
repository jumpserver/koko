package i18n

import (
	"log"
	"net/url"
	"strings"
	"sync"

	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/service"
)

type Language struct {
	code   string
	domain string

	mu         sync.RWMutex
	dict       map[string]string // loaded once at NewLang/Refresh
	authClient httplib.Client
}

var (
	defaultDomain = "koko"
	baseURL       = "/api/v1/settings/i18n/koko/"
)

func NewLang(code string, jmsService *service.JMService) *Language {
	code = string(normalize(code))
	l := &Language{
		code:       code,
		domain:     defaultDomain,
		dict:       map[string]string{},
		authClient: jmsService.CloneClient(),
	}
	err := l.Refresh()
	if err != nil {
		log.Fatalf("failed to refresh language code: %v", err)
	}
	return l
}

func (l *Language) T(key string) string {
	l.mu.RLock()
	v, ok := l.dict[key]
	l.mu.RUnlock()
	if ok && v != "" {
		return v
	}

	return key
}

func (l *Language) Refresh() error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("lang", l.code)
	u.RawQuery = q.Encode()

	l.mu.Lock()
	_, err = l.authClient.Get(u.String(), &l.dict)
	l.mu.Unlock()
	if err != nil {
		return err
	}

	return nil
}

func normalize(code string) LanguageCode {
	code = strings.ToLower(code)
	if strings.Contains(code, "en") {
		return EN
	} else if strings.Contains(code, "ja") {
		return JA
	}
	if i18nCode, ok := i18nCodeMap[code]; ok {
		return i18nCode
	}
	return EN
}

//import (
//	"log"
//	"net/url"
//	"path"
//	"strings"
//	"sync"
//
//	"github.com/jumpserver-dev/sdk-go/httplib"
//	"github.com/leonelquinteros/gotext"
//
//	"github.com/jumpserver/koko/pkg/config"
//)
//
//func Initial() {
//	cf := config.GetConf()
//	localePath := path.Join(cf.RootPath, "locale")
//	lowerCode := strings.ToLower(cf.LanguageCode)
//	gotext.Configure(localePath, lowerCode, "koko")
//	setupLangMap(localePath)
//}
//
//func setupLangMap(localePath string) {
//	for _, code := range allLangCodes {
//		enLocal := gotext.NewLocale(localePath, code.String())
//		enLocal.AddDomain("koko")
//		langMap[code] = enLocal
//	}
//}
//
//func NewLang(code string) LanguageCode {
//	code = strings.ToLower(code)
//	if strings.Contains(code, "en") {
//		return EN
//	} else if strings.Contains(code, "ja") {
//		return JA
//	}
//	if i18nCode, ok := i18nCodeMap[code]; ok {
//		return i18nCode
//	}
//	return EN
//}
//
//func T(s string) string {
//	return gotext.Get(s)
//}
