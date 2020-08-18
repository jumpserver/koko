package handler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

type DatabaseEngine interface {
	baseEngine
	Retrieve(pageSize, offset int, searches ...string) []model.Database
}

var (
	_ Application    = (*DatabaseApplication)(nil)
	_ DatabaseEngine = (*remoteDatabaseEngine)(nil)
	_ DatabaseEngine = (*localDatabaseEngine)(nil)
)

type DatabaseApplication struct {
	h *interactiveHandler

	engine DatabaseEngine

	searchKeys []string

	currentResult []model.Database
}

func (k *DatabaseApplication) Name() string {
	return "database"
}

func (k *DatabaseApplication) MoveNextPage() {
	if k.engine.HasNext() {
		offset := k.engine.CurrentOffSet()
		newPageSize := getPageSize(k.h.term)
		k.currentResult = k.engine.Retrieve(newPageSize, offset, k.searchKeys...)
	}
	k.DisplayCurrentResult()
}

func (k *DatabaseApplication) MovePrePage() {
	if k.engine.HasPrev() {
		offset := k.engine.CurrentOffSet()
		newPageSize := getPageSize(k.h.term)
		start := offset - newPageSize
		if start <= 0 {
			start = 0
		}
		k.currentResult = k.engine.Retrieve(newPageSize, start, k.searchKeys...)
	}
	k.DisplayCurrentResult()
}

func (k *DatabaseApplication) Search(key string) {
	newPageSize := getPageSize(k.h.term)
	k.searchKeys = []string{key}
	k.currentResult = k.engine.Retrieve(newPageSize, 0, key)
	k.DisplayCurrentResult()

}

func (k *DatabaseApplication) SearchAgain(key string) {
	k.searchKeys = append(k.searchKeys, key)
	newPageSize := getPageSize(k.h.term)
	k.currentResult = k.engine.Retrieve(newPageSize, 0, k.searchKeys...)
	k.DisplayCurrentResult()
}

func (k *DatabaseApplication) SearchOrProxy(key string) {
	if indexNum, err := strconv.Atoi(key); err == nil && len(k.currentResult) > 0 {
		if indexNum > 0 && indexNum <= len(k.currentResult) {
			k.ProxyDB(k.currentResult[indexNum-1])
			return
		}
	}

	newPageSize := getPageSize(k.h.term)
	currentResult := k.engine.Retrieve(newPageSize, 0, key)
	if len(currentResult) == 1 {
		k.ProxyDB(currentResult[0])
		return
	}
	k.currentResult = currentResult
	k.searchKeys = []string{key}
	k.DisplayCurrentResult()
}

func (k *DatabaseApplication) DisplayCurrentResult() {
	currentDBS := k.currentResult
	term := k.h.term
	searchHeader := fmt.Sprintf(i18n.T("Search: %s"), strings.Join(k.searchKeys, " "))
	if len(currentDBS) == 0 {
		_, _ = term.Write([]byte(i18n.T("No Databases") + "\n\r"))
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}

	currentPage := k.engine.CurrentPage()
	pageSize := k.engine.PageSize()
	totalPage := k.engine.TotalPage()
	totalCount := k.engine.TotalCount()

	idLabel := i18n.T("ID")
	nameLabel := i18n.T("Name")
	ipLabel := i18n.T("IP")
	dbTypeLabel := i18n.T("DBType")
	dbNameLabel := i18n.T("DB Name")
	commentLabel := i18n.T("Comment")

	Labels := []string{idLabel, nameLabel, ipLabel,
		dbTypeLabel, dbNameLabel, commentLabel}
	fields := []string{"ID", "name", "IP", "DBType", "DBName", "comment"}
	data := make([]map[string]string, len(currentDBS))
	for i, j := range currentDBS {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["name"] = j.Name
		row["IP"] = j.Host
		row["DBType"] = j.DBType
		row["DBName"] = j.DBName

		comments := make([]string, 0)
		for _, item := range strings.Split(strings.TrimSpace(j.Comment), "\r\n") {
			if strings.TrimSpace(item) == "" {
				continue
			}
			comments = append(comments, strings.ReplaceAll(strings.TrimSpace(item), " ", ","))
		}
		row["comment"] = strings.Join(comments, "|")
		data[i] = row
	}
	w, _ := term.GetSize()

	caption := fmt.Sprintf(i18n.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		currentPage, pageSize, totalPage, totalCount)

	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":      {0, 0, 5},
			"name":    {0, 8, 0},
			"IP":      {0, 15, 40},
			"DBType":  {0, 8, 0},
			"DBName":  {0, 8, 0},
			"comment": {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	loginTip := i18n.T("Enter ID number directly login the database, multiple search use // + field, such as: //16")
	pageActionTip := i18n.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)

	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}

func (k *DatabaseApplication) ProxyDB(dbSelect model.Database) {
	systemUsers := service.GetUserDatabaseSystemUsers(k.h.user.ID, dbSelect.ID)
	defer k.h.term.SetPrompt("[DB]> ")
	systemUserSelect, ok := k.h.chooseSystemUser(systemUsers)
	if !ok {
		return
	}
	p := proxy.DBProxyServer{
		UserConn:   k.h.sess,
		User:       k.h.user,
		Database:   &dbSelect,
		SystemUser: &systemUserSelect,
	}
	k.h.pauseWatchWinSize()
	p.Proxy()
	k.h.resumeWatchWinSize()
	logger.Infof("Request %s: database %s proxy end", k.h.sess.Uuid, dbSelect.Name)
}

type localDatabaseEngine struct {
	data []model.Database
	*pageInfo

	cacheLastSearchResult []model.Database
	cacheLastSearchKeys   []string
}

func (e *localDatabaseEngine) Retrieve(pageSize, offset int, searches ...string) (databases []model.Database) {
	if pageSize <= 0 {
		pageSize = PAGESIZEALL
	}
	if offset < 0 {
		offset = 0
	}

	searchResult := e.searchResult(searches...)
	var (
		totalDatabase   []model.Database
		total           int
		currentOffset   int
		currentPageSize int
	)

	if offset < len(searchResult) {
		totalDatabase = searchResult[offset:]
	}
	total = len(totalDatabase)
	currentPageSize = pageSize
	databases = totalDatabase

	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(totalDatabase)
	}
	if total > currentPageSize {
		databases = totalDatabase[:currentPageSize]
	}
	currentOffset = offset + len(databases)
	e.updatePageInfo(currentPageSize, total, currentOffset)
	return
}

func (e *localDatabaseEngine) searchResult(searches ...string) []model.Database {
	sort.Strings(searches)
	if len(searches) == 0 {
		return e.data
	}
	if len(searches) == 1 && searches[0] == "" {
		return e.data
	}

	if IsEqualStringSlice(e.cacheLastSearchKeys, searches) &&
		e.cacheLastSearchResult != nil {
		return e.cacheLastSearchResult
	}
	e.cacheLastSearchKeys = searches
	e.cacheLastSearchResult = searchMatchedDatabases(e.data, searches...)
	return e.cacheLastSearchResult
}

func (e *localDatabaseEngine) HasPrev() bool {
	return e.currentPage > 1
}

func (e *localDatabaseEngine) HasNext() bool {
	return e.currentPage < e.totalPage
}

type remoteDatabaseEngine struct {
	user model.User

	nextUrl string
	preUrl  string
	*pageInfo
}

func (e *remoteDatabaseEngine) Retrieve(pageSize, offset int, searches ...string) (databases []model.Database) {
	resp := service.GetUserPaginationDatabases(e.user.ID, pageSize, offset, searches...)
	var (
		total           int
		currentOffset   int
		currentPageSize int
	)
	e.nextUrl = resp.NextURL
	e.preUrl = resp.PreviousURL
	databases = resp.Data
	total = resp.Total
	currentPageSize = pageSize
	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(databases)
	}
	if len(databases) > currentPageSize {
		databases = databases[:currentPageSize]
	}
	currentOffset = offset + len(databases)
	e.updatePageInfo(currentPageSize, total, currentOffset)
	return
}

func (e *remoteDatabaseEngine) HasPrev() bool {
	return e.preUrl != ""
}

func (e *remoteDatabaseEngine) HasNext() bool {
	return e.nextUrl != ""
}

func searchMatchedDatabases(data []model.Database, keys ...string) []model.Database {
	matched := make([]model.Database, 0, len(data))
	for i := range data {
		db := data[i]
		ok := true
		contents := []string{strings.ToLower(db.Name), strings.ToLower(db.DBName),
			strings.ToLower(db.Host), strings.ToLower(db.Comment)}

		for j := range keys {
			if !isSubstring(contents, strings.ToLower(keys[j])) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, db)
		}
	}
	return matched
}
