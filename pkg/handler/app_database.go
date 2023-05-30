package handler

import (
	"github.com/jumpserver/koko/pkg/i18n"
)

func (u *UserSelectHandler) displayDatabaseResult(searchHeader string) {
	currentResult := u.currentResult
	lang := i18n.NewLang(u.h.i18nLang)
	if len(currentResult) == 0 {
		noDatabases := lang.T("No Databases")
		u.displayNoResultMsg(searchHeader, noDatabases)
		return
	}
	u.displayAssets(searchHeader)
}
