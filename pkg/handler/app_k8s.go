package handler

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) displayK8sResult(searchHeader string) {
	currentResult := u.currentResult
	lang := i18n.NewLang(u.h.i18nLang, u.h.jmsService)
	if len(currentResult) == 0 {
		noK8s := lang.T("No kubernetes")
		u.displayNoResultMsg(searchHeader, noK8s)
		return
	}
	u.displayAssets(searchHeader)
}

func (u *UserSelectHandler) displayResult(searchHeader string, Labels, fields []string,
	fieldSize map[string][3]int, data []map[string]string) {
	lang := i18n.NewLang(u.h.i18nLang, u.h.jmsService)
	vt := u.h.term
	w, _ := u.h.GetPtySize()
	currentPage := u.CurrentPage()
	pageSize := u.PageSize()
	totalPage := u.TotalPage()
	totalCount := u.TotalCount()
	caption := fmt.Sprintf(lang.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		currentPage, pageSize, totalPage, totalCount)

	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields:      fields,
		Labels:      Labels,
		FieldsSize:  fieldSize,
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	loginTip := lang.T("Enter ID number directly login, multiple search use // + field, such as: //16")
	pageActionTip := lang.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)
	_, _ = vt.Write([]byte(utils.CharClear))
	_, _ = vt.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(vt, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	utils.IgnoreErrWriteString(vt, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
}

func (u *UserSelectHandler) displayNoResultMsg(searchHeader, tips string) {
	vt := u.h.term
	utils.IgnoreErrWriteString(vt, utils.WrapperString(tips, utils.Red))
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	utils.IgnoreErrWriteString(vt, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
}
