package handler

import (
	"cocogo/pkg/common"
	"cocogo/pkg/i18n"
	"fmt"
	"strconv"
	"strings"

	"cocogo/pkg/config"
	"cocogo/pkg/model"
	"cocogo/pkg/utils"
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
		pageSize int
	)
	_, height := p.term.GetSize()
	switch config.Conf.AssetListPageSize {
	case "auto":
		pageSize = height - 8
	case "all":
		pageSize = len(p.assets)
	default:
		if value, err := strconv.Atoi(config.Conf.AssetListPageSize); err == nil {
			pageSize = value
		} else {
			pageSize = height - 8
		}
	}
	if pageSize <= 0 {
		pageSize = 1
	}
	return pageSize
}

func (p *AssetPagination) Start() []model.Asset {

	for {
		// 当前页是第一个，如果当前页数据小于page size，显示所有
		if p.page.CurrentPage() == 1 && p.page.GetPageSize() > len(p.currentData) {
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
				if !p.page.HasPrePage() {
					continue
				}
				tmpData := p.page.GetPrePageData()
				if len(p.currentData) != len(tmpData) {
					p.currentData = make([]model.Asset, len(tmpData))
				}
				for i, item := range tmpData {
					p.currentData[i] = item.(model.Asset)
				}

			case "", "n":
				if !p.page.HasNextPage() {
					continue
				}
				tmpData := p.page.GetNextPageData()
				if len(p.currentData) != len(tmpData) {
					p.currentData = make([]model.Asset, len(tmpData))
				}
				for i, item := range tmpData {
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
	Labels := []string{i18n.T("ID"), i18n.T("主机名"), i18n.T("IP"), i18n.T("系统用户"), i18n.T("Comment")}
	fields := []string{"ID", "hostname", "IP", "systemUsers", "comment"}
	data := make([]map[string]string, len(p.currentData))
	for i, j := range p.currentData {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["hostname"] = j.Hostname
		row["IP"] = j.Ip

		systemUser := selectHighestPrioritySystemUsers(j.SystemUsers)
		names := make([]string, len(systemUser))
		for i := range systemUser {
			names[i] = systemUser[i].Name
		}
		row["systemUsers"] = strings.Join(names, ",")
		fmt.Println(row["系统用户"], len(row["系统用户"]))
		row["comment"] = j.Comment
		data[i] = row
	}
	w, _ := p.term.GetSize()
	caption := fmt.Sprintf(i18n.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		p.page.CurrentPage(), p.page.GetPageSize(), p.page.TotalPage(), p.page.TotalCount(),
	)
	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":          {0, 0, 0},
			"hostname":    {0, 8, 0},
			"IP":          {15, 0, 0},
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
		"\nTips: Enter the asset ID and log directly into the asset.\n",
		"\nPage up: P/p	Page down: Enter|N/n	BACK: b.\n",
	}
	for _, tip := range tips {
		_, _ = p.term.Write([]byte(tip))
	}

}
