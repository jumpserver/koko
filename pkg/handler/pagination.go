package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewAssetPagination(term *utils.Terminal, assets []model.Asset) AssetPagination {
	assetPage := AssetPagination{term: term, assets: assets}
	assetPage.Initial()
	return assetPage
}

type AssetPagination struct {
	term        *utils.Terminal
	assets      []model.Asset
	page        *common.Pagination
	currentData []model.Asset
}

func (p *AssetPagination) Initial() {
	pageData := make([]interface{}, len(p.assets))
	for i, v := range p.assets {
		pageData[i] = v
	}
	pageSize := p.getPageSize()
	p.page = common.NewPagination(pageData, pageSize)
	firstPageData := p.page.GetPageData(1)
	p.currentData = make([]model.Asset, len(firstPageData))
	for i, item := range firstPageData {
		p.currentData[i] = item.(model.Asset)
	}
}

func (p *AssetPagination) getPageSize() int {
	var (
		pageSize  int
		minHeight = 8 // 分页显示的最小高度
	)
	_, height := p.term.GetSize()
	switch config.GetConf().AssetListPageSize {
	case "auto":
		pageSize = height - minHeight
	case "all":
		pageSize = len(p.assets)
	default:
		if value, err := strconv.Atoi(config.GetConf().AssetListPageSize); err == nil {
			pageSize = value
		} else {
			pageSize = height - minHeight
		}
	}
	if pageSize <= 0 {
		pageSize = 1
	}
	return pageSize
}

func (p *AssetPagination) Start() []model.Asset {
	p.term.SetPrompt(": ")
	defer p.term.SetPrompt("Opt> ")
	for {
		// 总数据小于page size，则显示所有资产且退出
		if p.page.PageSize() >= p.page.TotalCount() {
			p.currentData = p.assets
			p.displayPageAssets()
			return []model.Asset{}
		}

		p.displayPageAssets()
		p.displayTipsInfo()
		line, err := p.term.ReadLine()
		if err != nil {
			return []model.Asset{}
		}
		pageSize := p.getPageSize()
		p.page.SetPageSize(pageSize)

		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				if !p.page.HasPrev() {
					continue
				}
				prePageData := p.page.GetPrevPageData()
				if len(p.currentData) != len(prePageData) {
					p.currentData = make([]model.Asset, len(prePageData))
				}
				for i, item := range prePageData {
					p.currentData[i] = item.(model.Asset)
				}

			case "", "n":
				if !p.page.HasNext() {
					continue
				}
				nextPageData := p.page.GetNextPageData()
				if len(p.currentData) != len(nextPageData) {
					p.currentData = make([]model.Asset, len(nextPageData))
				}
				for i, item := range nextPageData {
					p.currentData[i] = item.(model.Asset)
				}
			case "b", "q":
				return []model.Asset{}
			default:
				if indexID, err := strconv.Atoi(line); err == nil {
					if indexID > 0 && indexID <= len(p.currentData) {
						return []model.Asset{p.currentData[indexID-1]}
					}
				}
			}
		default:
			if indexID, err := strconv.Atoi(line); err == nil {
				if indexID > 0 && indexID <= len(p.currentData) {
					return []model.Asset{p.currentData[indexID-1]}
				}
			}
		}
	}
}

func (p *AssetPagination) displayPageAssets() {
	Labels := []string{getI18nFromMap("ID"), getI18nFromMap("Hostname"),
		getI18nFromMap("IP"), getI18nFromMap("Comment")}
	fields := []string{"ID", "hostname", "IP", "comment"}
	data := make([]map[string]string, len(p.currentData))
	for i, j := range p.currentData {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["hostname"] = j.Hostname
		row["IP"] = j.IP

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
	w, _ := p.term.GetSize()
	caption := fmt.Sprintf(getI18nFromMap("AssetTableCaption"),
		p.page.CurrentPage(), p.page.PageSize(), p.page.TotalPage(), p.page.TotalCount(),
	)
	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"hostname": {0, 8, 0},
			"IP":       {0, 15, 40},
			"comment":  {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	_, _ = p.term.Write([]byte(utils.CharClear))
	_, _ = p.term.Write([]byte(table.Display()))
}

func (p *AssetPagination) displayTipsInfo() {
	displayAssetPaginationTipsInfo(p.term)

}

func NewUserPagination(term *utils.Terminal, uid, search string, policy bool) UserAssetPagination {
	return UserAssetPagination{
		UserID:        uid,
		offset:        0,
		limit:         0,
		search:        search,
		term:          term,
		displayPolicy: policy,
		Data:          model.AssetsPaginationResponse{},
	}
}

type UserAssetPagination struct {
	UserID        string
	offset        int
	limit         int
	search        string
	term          *utils.Terminal
	displayPolicy bool
	Data          model.AssetsPaginationResponse
	IsNeedProxy   bool
	currentData   []model.Asset
}

func (p *UserAssetPagination) Start() []model.Asset {
	p.term.SetPrompt(": ")
	defer p.term.SetPrompt("Opt> ")
	for {
		p.retrieveData()

		if p.displayPolicy && p.Data.Total == 1 {
			p.IsNeedProxy = true
			return p.Data.Data
		}

		// 无上下页，则退出循环
		if p.Data.NextURL == "" && p.Data.PreviousURL == "" {
			p.displayPageAssets()
			return p.currentData
		}

	inLoop:
		p.displayPageAssets()
		p.displayTipsInfo()
		line, err := p.term.ReadLine()
		if err != nil {
			return p.currentData
		}

		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				if p.Data.PreviousURL == "" {
					continue
				}
				p.offset -= p.limit
			case "", "n":
				if p.Data.NextURL == "" {
					continue
				}
				p.offset += p.limit
			case "b", "q":
				return []model.Asset{}
			default:
				if indexID, err := strconv.Atoi(line); err == nil {
					if indexID > 0 && indexID <= len(p.currentData) {
						p.IsNeedProxy = true
						return []model.Asset{p.currentData[indexID-1]}
					}
				}
				goto inLoop
			}
		default:
			if indexID, err := strconv.Atoi(line); err == nil {
				if indexID > 0 && indexID <= len(p.currentData) {
					p.IsNeedProxy = true
					return []model.Asset{p.currentData[indexID-1]}
				}
			}
			goto inLoop
		}
	}
}

func (p *UserAssetPagination) displayPageAssets() {
	if len(p.Data.Data) == 0 {
		_, _ = p.term.Write([]byte(getI18nFromMap("NoAssets") + "\n\r"))
		return
	}

	Labels := []string{getI18nFromMap("ID"), getI18nFromMap("Hostname"),
		getI18nFromMap("IP"), getI18nFromMap("Comment")}
	fields := []string{"ID", "hostname", "IP", "comment"}
	p.currentData = model.AssetList(p.Data.Data).SortBy(config.GetConf().AssetListSortBy)
	data := make([]map[string]string, len(p.currentData))
	for i, j := range p.currentData {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["hostname"] = j.Hostname
		row["IP"] = j.IP

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
	w, _ := p.term.GetSize()
	var pageSize int
	var totalPage int
	var currentPage int
	var totalCount int
	var currentOffset int
	currentOffset = p.offset + len(p.currentData)
	switch p.limit {
	case 0:
		pageSize = len(p.currentData)
		totalCount = pageSize
		totalPage = 1
		currentPage = 1
	default:
		pageSize = p.limit
		totalCount = p.Data.Total

		switch totalCount % pageSize {
		case 0:
			totalPage = totalCount / pageSize
		default:
			totalPage = (totalCount / pageSize) + 1
		}
		switch currentOffset % pageSize {
		case 0:
			currentPage = currentOffset / pageSize
		default:
			currentPage = (currentOffset / pageSize) + 1
		}
	}
	caption := fmt.Sprintf(getI18nFromMap("AssetTableCaption"),
		currentPage, pageSize, totalPage, totalCount)

	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"hostname": {0, 8, 0},
			"IP":       {0, 15, 40},
			"comment":  {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	_, _ = p.term.Write([]byte(utils.CharClear))
	_, _ = p.term.Write([]byte(table.Display()))
}

func (p *UserAssetPagination) displayTipsInfo() {
	displayAssetPaginationTipsInfo(p.term)
}

func (p *UserAssetPagination) retrieveData() {
	p.limit = getPageSize(p.term)
	if p.limit == 0 || p.offset < 0 || p.limit >= p.Data.Total {
		p.offset = 0
	}
	p.Data = service.GetUserAssets(p.UserID, p.search, p.limit, p.offset)
}

func getPageSize(term *utils.Terminal) int {
	var (
		pageSize  int
		minHeight = 8 // 分页显示的最小高度

	)
	_, height := term.GetSize()
	conf := config.GetConf()
	switch conf.AssetListPageSize {
	case "auto":
		pageSize = height - minHeight
	case "all":
		return 0
	default:
		if value, err := strconv.Atoi(conf.AssetListPageSize); err == nil {
			pageSize = value
		} else {
			pageSize = height - minHeight
		}
	}
	if pageSize <= 0 {
		pageSize = 1
	}
	return pageSize
}

func displayAssetPaginationTipsInfo(w io.Writer) {
	utils.IgnoreErrWriteString(w, getI18nFromMap("LoginTip"))
	utils.IgnoreErrWriteString(w, getI18nFromMap("PageActionTip"))
}
