package handler

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) searchLocalK8s(searches ...string) []model.Asset {
	fields := map[string]struct{}{
		"name":     {},
		"address":  {},
		"org_name": {},
		"comment":  {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayK8sResult(searchHeader string) {
	currentDBS := u.currentResult
	lang := i18n.NewLang(u.h.i18nLang)
	if len(currentDBS) == 0 {
		noK8s := lang.T("No kubernetes")
		u.displayNoResultMsg(searchHeader, noK8s)
		return
	}

	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	clusterLabel := lang.T("Cluster")
	orgLabel := lang.T("Organization")
	commentLabel := lang.T("Comment")
	Labels := []string{idLabel, nameLabel, clusterLabel, orgLabel, commentLabel}
	fields := []string{"ID", "Name", "Cluster", "Organization", "Comment"}
	fieldSize := map[string][3]int{
		"ID":           {0, 0, 5},
		"Name":         {0, 8, 0},
		"Cluster":      {0, 20, 0},
		"Organization": {0, 8, 0},
		"Comment":      {0, 0, 0},
	}
	generateRowFunc := func(i int, item *model.Asset) map[string]string {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Name"] = item.Name
		row["Cluster"] = item.Address
		row["Organization"] = item.OrgName
		row["Comment"] = joinMultiLineString(item.Comment)
		return row
	}
	assetDisplay := lang.T("the kubernetes")
	u.displayResult(searchHeader, assetDisplay,
		Labels, fields, fieldSize, generateRowFunc)
}

type createRowFunc func(int, *model.Asset) map[string]string

func (u *UserSelectHandler) displayResult(searchHeader, assetDisplay string,
	Labels, fields []string, fieldSize map[string][3]int,
	generateRowFunc createRowFunc) {
	lang := i18n.NewLang(u.h.i18nLang)
	currentDBS := u.currentResult
	term := u.h.term
	data := make([]map[string]string, len(currentDBS))
	for i := range currentDBS {
		item := currentDBS[i]
		data[i] = generateRowFunc(i, &item)
	}

	w, _ := term.GetSize()
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
	loginTip := lang.T("Enter ID number directly login %s, multiple search use // + field, such as: //16")
	loginTip = fmt.Sprintf(loginTip, assetDisplay)
	pageActionTip := lang.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)
	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}

func (u *UserSelectHandler) displayNoResultMsg(searchHeader, tips string) {
	term := u.h.term
	utils.IgnoreErrWriteString(term, utils.WrapperString(tips, utils.Red))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}
