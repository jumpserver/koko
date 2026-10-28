package handler

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"github.com/olekukonko/tablewriter"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) displayResult(_ string, labels, fields []string,
	fieldSize map[string][3]int, data []map[string]string) {
	lang := i18n.NewLang(u.h.i18nLang)
	vt := u.h.term
	width, _ := u.h.GetPtySize()
	start, end := resultDisplayRange(u.CurrentOffSet(), len(u.currentResult), u.TotalCount())
	pageTip := ""
	if u.allAssetCount != nil {
		pageTip = assetPageSummary(lang, width, start, end, u.TotalCount(), *u.allAssetCount)
	} else if u.TotalPage() > 1 {
		pageTip = fmt.Sprintf("%d-%d / %d", start, end, u.TotalCount())
	}
	statusLines, searchHints := u.assetStatusAndSearchHints(lang, pageTip, width)
	hints := make([]classicHintRow, 0, 3)
	hints = append(hints, statusLines...)
	if u.loadErr == nil && u.allAssetCount != nil {
		hints = append(hints, classicHintRow{
			text: assetMatchSummary(lang, u.TotalCount(), *u.allAssetCount), style: classicHintPlain,
		})
	}
	hints = append(hints, searchHints...)
	hints = append(hints, classicHintRows(classicHintShortcut,
		u.assetActionHints(lang, width, u.HasPrev(), u.HasNext()))...)
	_, _ = vt.Write([]byte(utils.CharClear))
	if width < 60 {
		utils.IgnoreErrWriteString(vt, classicCompactAssetRows(fields, labels, data, width))
	} else {
		table := common.WrapperTable{
			Fields: fields, Labels: labels, FieldsSize: fieldSize, Data: data,
			TotalSize: width, TruncPolicy: common.TruncMiddle,
		}
		table.RowColors = make([][]tablewriter.Colors, len(data))
		for i := range data {
			if u.assetCanConnect(i) {
				continue
			}
			table.RowColors[i] = make([]tablewriter.Colors, len(fields))
			for j := range table.RowColors[i] {
				table.RowColors[i][j] = tablewriter.Colors{tablewriter.FgHiBlackColor}
			}
		}
		table.Initial()
		_, _ = vt.Write([]byte(table.Display()))
	}
	utils.IgnoreErrWriteString(vt, classicAssetHintPanel(hints, width))
}

func classicCompactAssetRows(fields, labels []string, data []map[string]string, width int) string {
	var result strings.Builder
	for _, asset := range data {
		name := ""
		var details []string
		for i, field := range fields {
			value := strings.TrimSpace(asset[field])
			if value == "" || field == "ID" {
				continue
			}
			if field == "Name" {
				name = value
			} else {
				details = append(details, labels[i]+": "+value)
			}
		}
		for _, line := range classicIndentedLines(asset["ID"]+". ", name, width) {
			result.WriteString(line + utils.CharNewLine)
		}
		if len(details) > 0 {
			for _, detail := range compactClassicHintRows(width-2, details...) {
				for _, line := range classicIndentedLines("  ", detail, width) {
					result.WriteString(line + utils.CharNewLine)
				}
			}
		}
	}
	return result.String()
}

func classicAssetStatusLines(parts []string, page string, width int) []string {
	var lines []string
	for _, row := range compactClassicHintRows(width, parts...) {
		lines = append(lines, classicHintLines(row, width)...)
	}
	if page == "" {
		return lines
	}
	pageWidth := runewidth.StringWidth(page)
	if pageWidth > width {
		return append(lines, classicHintLines(page, width)...)
	}
	if len(lines) > 0 {
		last := len(lines) - 1
		if runewidth.StringWidth(lines[last])+2+pageWidth <= width {
			lines[last] += strings.Repeat(" ", width-runewidth.StringWidth(lines[last])-pageWidth) + page
			return lines
		}
	}
	return append(lines, strings.Repeat(" ", width-pageWidth)+page)
}

func (u *UserSelectHandler) assetStatusAndSearchHints(lang i18n.LanguageCode, page string, width int) ([]classicHintRow, []classicHintRow) {
	searchHints := u.searchSyntaxHints(lang)
	searchTip := u.currentSearchTip(lang)
	scope := u.scopePathHint(lang)
	if searchTip == "" && len(searchHints) > 0 {
		if scope != "" {
			return classicHintRows(classicHintPlain, classicAssetStatusLines([]string{scope}, page, width)), searchHints
		}
		return classicHintRows(searchHints[0].style, classicRightAlignedHintLines(searchHints[0].text, page, width)), searchHints[1:]
	}
	return classicHintRows(classicHintPlain, classicAssetStatusLines([]string{searchTip, scope}, page, width)), searchHints
}

func (u *UserSelectHandler) currentSearchTip(lang i18n.LanguageCode) string {
	if summary := currentSearchSummary(u.searchKeys, ""); summary != "" {
		return fmt.Sprintf(lang.T("Search: %s"), summary)
	}
	return ""
}

func (u *UserSelectHandler) assetActionHints(lang i18n.LanguageCode, width int, hasPrev, hasNext bool) []string {
	actions := make([]string, 0, 5)
	if hasPrev {
		actions = append(actions, fmt.Sprintf("[p] %s", lang.T("Previous page")))
	}
	if hasNext {
		actions = append(actions, fmt.Sprintf("[n] %s", lang.T("Next page")))
	}
	actions = append(actions, lang.T("[number] Select an asset"),
		fmt.Sprintf("[b] %s", u.classicBackTarget(lang)),
		fmt.Sprintf("[?] %s", lang.T("View help")),
		classicExitHint(lang))
	return compactClassicHintRows(width, actions...)
}

func (u *UserSelectHandler) assetFooterRows(width int) int {
	lang := i18n.NewLang(u.h.i18nLang)
	page := assetPageSummary(lang, width, 1, 999999, 999999, 999999)
	hints, searchHints := u.assetStatusAndSearchHints(lang, page, width)
	hints = append(hints, searchHints...)
	hints = append(hints, classicHintRows(classicHintShortcut, u.assetActionHints(lang, width, true, true))...)
	rows := 1 // Separator.
	for _, hint := range hints {
		rows += len(classicHintLines(hint.text, width))
	}
	rows += max(
		len(classicHintLines(assetMatchSummary(lang, 999998, 999999), width)),
		len(classicHintLines(assetMatchSummary(lang, 0, 999999), width)),
		len(classicHintLines(assetMatchSummary(lang, 0, 0), width)),
	)
	return rows
}

func currentSearchSummary(searches []string, allAssets string) string {
	terms := make([]string, 0, len(searches))
	for _, search := range searches {
		search = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, search))
		if search != "" {
			terms = append(terms, search)
		}
	}
	if len(terms) == 0 {
		return allAssets
	}
	return strings.Join(terms, " & ")
}

func resultDisplayRange(currentOffset, currentCount, total int) (start, end int) {
	if currentCount <= 0 || total <= 0 {
		return 0, 0
	}
	end = min(max(currentOffset, 0), total)
	start = max(1, end-currentCount+1)
	return start, end
}

func assetPageSummary(lang i18n.LanguageCode, width, start, end, supported, total int) string {
	connectableLabel, totalLabel := lang.T("Connectable"), lang.T("Total")
	switch lang {
	case i18n.ZH:
		connectableLabel, totalLabel = "可连接", "总数"
	case i18n.ZHHant:
		connectableLabel, totalLabel = "可連線", "總數"
	}
	summary := fmt.Sprintf("%d-%d / %s %d / %s %d", start, end, connectableLabel, supported, totalLabel, total)
	if runewidth.StringWidth(summary) <= width {
		return summary
	}
	summary = fmt.Sprintf("%d-%d / %d / %d", start, end, supported, total)
	if runewidth.StringWidth(summary) <= width {
		return summary
	}
	return fmt.Sprintf("%d-%d/%d/%d", start, end, supported, total)
}

func assetCountExplanation(lang i18n.LanguageCode) string {
	switch lang {
	case i18n.ZH:
		return "序号范围 / 可连接 / 总数；数量随当前筛选变化。可连接指已启用且协议受支持。"
	case i18n.ZHHant:
		return "序號範圍 / 可連線 / 總數；數量隨目前篩選變化。可連線指已啟用且通訊協定受支援。"
	default:
		return lang.T("Range / Connectable / Total; counts follow the current filters. Connectable means enabled with a supported protocol.")
	}
}

func assetMatchSummary(lang i18n.LanguageCode, supported, total int) string {
	if total == 0 {
		switch lang {
		case i18n.ZH:
			return "未找到符合当前条件的资产"
		case i18n.ZHHant:
			return "未找到符合目前條件的資產"
		default:
			return lang.T("No assets match the current filters")
		}
	}
	if supported == 0 {
		switch lang {
		case i18n.ZH:
			return fmt.Sprintf("共匹配 %d 台资产，均不支持连接", total)
		case i18n.ZHHant:
			return fmt.Sprintf("共匹配 %d 台資產，均不支援連線", total)
		default:
			return fmt.Sprintf(lang.T("Matched %d assets; none are connectable"), total)
		}
	}
	if supported == total {
		switch lang {
		case i18n.ZH:
			return fmt.Sprintf("共匹配 %d 台资产，均可连接", total)
		case i18n.ZHHant:
			return fmt.Sprintf("共匹配 %d 台資產，均可連線", total)
		default:
			return fmt.Sprintf(lang.T("Matched %d assets; all are connectable"), total)
		}
	}
	switch lang {
	case i18n.ZH:
		return fmt.Sprintf("共匹配 %d 台资产，其中 %d 台可连接", total, supported)
	case i18n.ZHHant:
		return fmt.Sprintf("共匹配 %d 台資產，其中 %d 台可連線", total, supported)
	default:
		return fmt.Sprintf(lang.T("Matched %d assets, %d connectable"), total, supported)
	}
}

func (u *UserSelectHandler) displayNoResultMsg(_ string, tips string) {
	lang := i18n.NewLang(u.h.i18nLang)
	if u.loadErr == nil && len(u.searchKeys) > 0 {
		tips = lang.T("No matching assets")
	}
	width, _ := u.h.GetPtySize()
	utils.IgnoreErrWriteString(u.h.term, utils.CharClear)
	searchHints := u.searchSyntaxHints(lang)
	hasCounts := u.loadErr == nil && u.allAssetCount != nil
	if !hasCounts {
		for _, line := range classicHintLines(tips, width) {
			utils.IgnoreErrWriteString(u.h.term, utils.WrapperString(line, utils.Red)+utils.CharNewLine)
		}
	}
	hints := make([]classicHintRow, 0, 3)
	page := ""
	if hasCounts {
		page = assetPageSummary(lang, width, 0, 0, u.TotalCount(), *u.allAssetCount)
	}
	hints = append(hints, classicHintRows(classicHintPlain,
		classicAssetStatusLines([]string{u.currentSearchTip(lang), u.scopePathHint(lang)}, page, width))...)
	if hasCounts {
		hints = append(hints, classicHintRow{
			text: assetMatchSummary(lang, u.TotalCount(), *u.allAssetCount), style: classicHintPlain,
		})
	}
	hints = append(hints, searchHints...)
	hints = append(hints, classicHintRows(classicHintShortcut, compactClassicHintRows(width,
		fmt.Sprintf("[b] %s", u.classicBackTarget(lang)), fmt.Sprintf("[?] %s", lang.T("View help")),
		classicExitHint(lang)))...)
	utils.IgnoreErrWriteString(u.h.term, classicAssetHintPanel(hints, width))
}

func (u *UserSelectHandler) classicBackTarget(lang i18n.LanguageCode) string {
	switch u.h.treeOrigin {
	case TypeNodeAsset:
		return lang.T("Back to authorization tree")
	case TypeTypeAsset:
		return lang.T("Back to type tree")
	case TypeFavoriteAsset:
		return lang.T("Back to favorites tree")
	default:
		return lang.T("Back to help")
	}
}

func (u *UserSelectHandler) scopePathHint(lang i18n.LanguageCode) string {
	switch u.currentType {
	case TypeNodeAsset:
		return fmt.Sprintf(lang.T("Node path: %s"), u.selectedPath)
	case TypeTypeAsset:
		return fmt.Sprintf(lang.T("Asset type: %s"), strings.TrimPrefix(u.selectedPath, "/"))
	case TypeFavoriteAsset:
		return fmt.Sprintf(lang.T("Favorite path: %s"), u.selectedPath)
	default:
		return ""
	}
}

func (u *UserSelectHandler) searchSyntaxHints(lang i18n.LanguageCode) []classicHintRow {
	if u.currentType != TypeNodeAsset && u.currentType != TypeTypeAsset &&
		u.currentType != TypeFavoriteAsset && currentSearchSummary(u.searchKeys, "") == "" {
		return []classicHintRow{{
			text:  fmt.Sprintf(lang.T("Search tip: %s"), lang.T("/ + IP, Hostname, Comment")),
			style: classicHintSearchTip,
		}}
	}
	return []classicHintRow{{
		text:  lang.T("Search guide: //keyword (refine) · /keyword (new global search) · IP/hostname/comment"),
		style: classicHintSearchGuide,
	}}
}
