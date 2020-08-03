package handler

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/utils"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

type K8sEngine interface {
	baseEngine
	Retrieve(pageSize, offset int, searches ...string) []model.K8sCluster
}

var (
	_ Application = (*K8sApplication)(nil)
	_ K8sEngine   = (*remoteK8sEngine)(nil)
	_ K8sEngine   = (*localK8sEngine)(nil)
)

type K8sApplication struct {
	h *interactiveHandler

	engine K8sEngine

	searchKeys []string

	currentResult []model.K8sCluster
}

func (k *K8sApplication) Name() string {
	return "k8s"
}

func (k *K8sApplication) MoveNextPage() {
	if k.engine.HasNext() {
		offset := k.engine.CurrentOffSet()
		newPageSize := getPageSize(k.h.term)
		k.currentResult = k.engine.Retrieve(newPageSize, offset, k.searchKeys...)
	}
	k.DisplayCurrentResult()
}

func (k *K8sApplication) MovePrePage() {
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

func (k *K8sApplication) Search(key string) {
	newPageSize := getPageSize(k.h.term)
	k.currentResult = k.engine.Retrieve(newPageSize, 0, key)
	k.searchKeys = []string{key}
	k.DisplayCurrentResult()
}

func (k *K8sApplication) SearchAgain(key string) {
	k.searchKeys = append(k.searchKeys, key)
	newPageSize := getPageSize(k.h.term)
	k.currentResult = k.engine.Retrieve(newPageSize, 0, k.searchKeys...)
	k.DisplayCurrentResult()
}

func (k *K8sApplication) SearchOrProxy(key string) {
	if indexNum, err := strconv.Atoi(key); err == nil && len(k.currentResult) > 0 {
		if indexNum > 0 && indexNum <= len(k.currentResult) {
			k.ProxyK8s(k.currentResult[indexNum-1])
			return
		}
	}

	newPageSize := getPageSize(k.h.term)
	currentResult := k.engine.Retrieve(newPageSize, 0, key)
	if len(currentResult) == 1 {
		k.ProxyK8s(currentResult[0])
		return
	}
	k.currentResult = currentResult
	k.searchKeys = []string{key}
	k.DisplayCurrentResult()
}

func (k *K8sApplication) DisplayCurrentResult() {
	currentDBS := k.currentResult
	term := k.h.term
	searchHeader := fmt.Sprintf(i18n.T("Search: %s"), strings.Join(k.searchKeys, " "))
	if len(currentDBS) == 0 {
		_, _ = term.Write([]byte(i18n.T("No kubernetes") + "\n\r"))
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
	clusterLabel := i18n.T("Cluster")
	commentLabel := i18n.T("Comment")

	Labels := []string{idLabel, nameLabel, clusterLabel, commentLabel}
	fields := []string{"ID", "name", "cluster", "comment"}
	data := make([]map[string]string, len(currentDBS))
	for i, j := range currentDBS {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["name"] = j.Name
		row["cluster"] = j.Cluster

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
			"cluster": {0, 20, 0},
			"comment": {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	loginTip := i18n.T("Enter ID number directly login the kubernetes, multiple search use // + field, such as: //16")
	pageActionTip := i18n.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)
	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}

func (k *K8sApplication) ProxyK8s(dbSelect model.K8sCluster) {
	systemUsers := service.GetUserK8sSystemUsers(k.h.user.ID, dbSelect.ID)
	defer k.h.term.SetPrompt("[k8s]> ")
	systemUserSelect, ok := k.h.chooseSystemUser(systemUsers)
	if !ok {
		return
	}
	p := proxy.K8sProxyServer{
		UserConn:   k.h.sess,
		User:       k.h.user,
		Cluster:    &dbSelect,
		SystemUser: &systemUserSelect,
	}
	k.h.pauseWatchWinSize()
	p.Proxy()
	k.h.resumeWatchWinSize()
	logger.Infof("Request %s: k8s %s proxy end", k.h.sess.Uuid, dbSelect.Name)
}

type remoteK8sEngine struct {
	user *model.User

	nextUrl string
	preUrl  string

	*pageInfo
}

func (e *remoteK8sEngine) Retrieve(pageSize, offset int, searches ...string) (clusters []model.K8sCluster) {
	resp := service.GetUserK8sClusters(e.user.ID, pageSize, offset, searches...)
	var (
		total           int
		currentOffset   int
		currentPageSize int
	)
	e.nextUrl = resp.NextURL
	e.preUrl = resp.PreviousURL
	clusters = resp.Data
	total = resp.Total
	currentPageSize = pageSize

	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(clusters)
	}
	if len(clusters) > currentPageSize {
		clusters = clusters[:currentPageSize]
	}
	currentOffset = offset + len(clusters)
	e.updatePageInfo(currentPageSize, total, currentOffset)
	return
}

func (e *remoteK8sEngine) HasPrev() bool {
	return e.preUrl != ""
}

func (e *remoteK8sEngine) HasNext() bool {
	return e.nextUrl != ""
}

type localK8sEngine struct {
	data []model.K8sCluster
	*pageInfo

	cacheLastSearchResult []model.K8sCluster
	cacheLastSearchKeys   string
}

func (e *localK8sEngine) Retrieve(pageSize, offset int, searches ...string) (clusters []model.K8sCluster) {
	if pageSize <= 0 {
		pageSize = PAGESIZEALL
	}
	if offset < 0 {
		offset = 0
	}

	searchResult := e.searchResult(searches...)
	var (
		totalClusters   []model.K8sCluster
		total           int
		currentOffset   int
		currentPageSize int
	)

	if offset < len(searchResult) {
		totalClusters = searchResult[offset:]
	}
	total = len(totalClusters)
	currentPageSize = pageSize
	if currentPageSize == PAGESIZEALL {
		currentPageSize = len(totalClusters)
	}
	if total > e.pageSize {
		clusters = totalClusters[:e.pageSize]
	} else {
		clusters = totalClusters
	}
	currentOffset = offset + len(clusters)
	e.updatePageInfo(currentPageSize, total, currentOffset)
	return
}

func (e *localK8sEngine) searchResult(searches ...string) []model.K8sCluster {
	compareKey := strings.Join(searches, "")
	if strings.EqualFold(e.cacheLastSearchKeys, compareKey) &&
		e.cacheLastSearchResult != nil {
		return e.cacheLastSearchResult
	}
	e.cacheLastSearchKeys = compareKey
	switch len(searches) {
	case 0:
		e.cacheLastSearchResult = e.data
	default:
		clusters := make([]model.K8sCluster, 0, len(e.data))
		for i := range e.data {
			ok := true
			for j := range searches {
				if !strings.Contains(strings.ToLower(e.data[i].Cluster),
					strings.ToLower(searches[j])) {
					ok = false
				}
			}
			if ok {
				clusters = append(clusters, e.data[i])
			}
		}
		e.cacheLastSearchResult = clusters
	}
	return e.cacheLastSearchResult
}

func (e *localK8sEngine) HasPrev() bool {
	return e.currentPage > 1
}

func (e *localK8sEngine) HasNext() bool {
	return e.currentPage < e.totalPage
}
