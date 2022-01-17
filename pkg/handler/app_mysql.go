package handler

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteDatabase(reqParam model.PaginationParam) []map[string]interface{} {
	res, err := u.h.jmsService.GetUserPermsDatabase(u.user.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user perm Database failed: %s", err)
	}
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) searchLocalDatabase(searches ...string) []map[string]interface{} {
	/*
	   	  {
	                  "id": "2b8f37ad-1580-4275-962a-7ea0f53c40b3",
	                  "name": "www",
	                  "domain": null,
	                  "category": "db",
	                  "type": "mysql",
	                  "attrs": {
	                      "host": "www",
	                      "port": 32342,
	                      "database": null
	                  },
	                  "comment": "",
	                  "org_id": "",
	                  "category_display": "数据库",
	                  "type_display": "MySQL",
	                  "org_name": "DEFAULT"
	              }
	*/
	fields := map[string]struct{}{
		"name":     {},
		"host":     {},
		"database": {},
		"comment":  {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayDatabaseResult(searchHeader string) {
	currentDBS := u.currentResult
	term := u.h.term
	lang := i18n.NewLang(u.h.i18nLang)
	if len(currentDBS) == 0 {
		noDatabases := lang.T("No Databases")
		utils.IgnoreErrWriteString(term, utils.WrapperString(noDatabases, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}

	currentPage := u.CurrentPage()
	pageSize := u.PageSize()
	totalPage := u.TotalPage()
	totalCount := u.TotalCount()

	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	ipLabel := lang.T("IP")
	dbTypeLabel := lang.T("DBType")
	dbNameLabel := lang.T("DB Name")
	commentLabel := lang.T("Comment")

	Labels := []string{idLabel, nameLabel, ipLabel,
		dbTypeLabel, dbNameLabel, commentLabel}
	fields := []string{"ID", "Name", "IP", "DBType", "DBName", "Comment"}
	data := make([]map[string]string, len(currentDBS))
	for i, j := range currentDBS {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		fieldsMap := map[string]string{
			"name":     "Name",
			"host":     "IP",
			"type":     "DBType",
			"database": "DBName",
			"comment":  "Comment"}
		row = convertMapItemToRow(j, fieldsMap, row)
		// 特殊处理 comment
		row["Comment"] = joinMultiLineString(row["Comment"])
		data[i] = row
	}
	w, _ := term.GetSize()

	caption := fmt.Sprintf(lang.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		currentPage, pageSize, totalPage, totalCount)

	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":      {0, 0, 5},
			"Name":    {0, 8, 0},
			"IP":      {0, 15, 40},
			"DBType":  {0, 8, 0},
			"DBName":  {0, 8, 0},
			"Comment": {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	loginTip := lang.T("Enter ID number directly login the database, multiple search use // + field, such as: //16")
	pageActionTip := lang.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)

	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}
