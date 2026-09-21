package handler

import "github.com/jumpserver/koko/pkg/i18n"

func (u *UserSelectHandler) displayDatabaseResult(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		u.displayNoResultMsg(searchHeader, lang.T("No Databases"))
		return
	}
	u.displayAssets(searchHeader)
}
