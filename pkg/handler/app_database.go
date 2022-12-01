package handler

import (
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (u *UserSelectHandler) searchLocalDatabase(searches ...string) []model.Asset {
	fields := map[string]struct{}{
		"name":     {},
		"address":  {},
		"db_name":  {},
		"org_name": {},
		"comment":  {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayDatabaseResult(searchHeader string) {
	currentDBS := u.currentResult
	lang := i18n.NewLang(u.h.i18nLang)
	if len(currentDBS) == 0 {
		noDatabases := lang.T("No Databases")
		u.displayNoResultMsg(searchHeader, noDatabases)
		return
	}

	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	ipLabel := lang.T("IP")
	dbTypeLabel := lang.T("DBType")
	dbNameLabel := lang.T("DB Name")
	orgLabel := lang.T("Organization")
	commentLabel := lang.T("Comment")

	labels := []string{idLabel, nameLabel, ipLabel,
		dbTypeLabel, dbNameLabel, orgLabel, commentLabel}
	fields := []string{"ID", "Name", "IP", "DBType", "DBName", "Organization", "Comment"}
	fieldsSize := map[string][3]int{
		"ID":           {0, 0, 5},
		"Name":         {0, 8, 0},
		"IP":           {0, 15, 40},
		"DBType":       {0, 8, 0},
		"DBName":       {0, 8, 0},
		"Organization": {0, 8, 0},
		"Comment":      {0, 0, 0},
	}
	generateRowFunc := func(i int, item *model.Asset) map[string]string {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Name"] = item.Name
		row["IP"] = item.Address
		row["DBType"] = strings.Join(item.SupportProtocols(), "|")
		row["DBName"] = item.Specific.DBName
		row["Organization"] = item.OrgName
		row["Comment"] = joinMultiLineString(item.Comment)
		return row
	}
	assetDisplay := lang.T("the database")
	u.displayResult(searchHeader, assetDisplay,
		labels, fields, fieldsSize, generateRowFunc)
}
