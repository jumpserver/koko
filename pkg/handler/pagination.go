package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewAssetPagination(term *utils.Terminal, assets []model.Asset) *AssetPagination {
	assetPage := &AssetPagination{term: term, assets: assets}
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
	Labels := []string{i18n.T("ID"), i18n.T("hostname"), i18n.T("IP"), i18n.T("systemUsers"), i18n.T("comment")}
	fields := []string{"ID", "hostname", "IP", "systemUsers", "comment"}
	data := make([]map[string]string, len(p.currentData))
	for i, j := range p.currentData {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["hostname"] = j.Hostname
		row["IP"] = j.IP

		systemUser := selectHighestPrioritySystemUsers(j.SystemUsers)
		names := make([]string, len(systemUser))
		for i := range systemUser {
			names[i] = systemUser[i].Name
		}
		row["systemUsers"] = strings.Join(names, ",")
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
	caption := fmt.Sprintf(i18n.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		p.page.CurrentPage(), p.page.PageSize(), p.page.TotalPage(), p.page.TotalCount(),
	)
	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":          {0, 0, 4},
			"hostname":    {0, 8, 0},
			"IP":          {0, 15, 40},
			"systemUsers": {0, 12, 0},
			"comment":     {0, 0, 0},
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
	tips := []string{
		i18n.T("\nTips: Enter the asset ID and log directly into the asset.\n"),
		i18n.T("\nPage up: P/p	Page down: Enter|N/n	BACK: b.\n"),
	}
	for _, tip := range tips {
		_, _ = p.term.Write([]byte(tip))
	}

}
