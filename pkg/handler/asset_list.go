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
	searchHints := u.searchSyntaxHints(lang, width)
	pageTip := ""
	if u.TotalPage() > 1 {
		pageTip = fmt.Sprintf("%d-%d / %d", start, end, u.TotalCount())
	}
	statusParts := []string{u.currentSearchTip(lang), u.scopePathHint(lang)}
	for _, connectable := range u.connectable {
		if !connectable {
			statusParts = append(statusParts, lang.T("⊘ Cannot connect"))
			break
		}
	}
	statusLines := classicAssetStatusLines(statusParts, pageTip, width)
	hints := make([]string, 0, 3)
	hints = append(hints, statusLines...)
	hints = append(hints, searchHints...)
	hints = append(hints, u.assetActionHints(lang, width, u.HasPrev(), u.HasNext())...)
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
	parts := []string{u.currentSearchTip(lang), u.scopePathHint(lang), lang.T("⊘ Cannot connect")}
	hints := classicAssetStatusLines(parts, "1-99999 / 99999", width)
	hints = append(hints, u.searchSyntaxHints(lang, width)...)
	hints = append(hints, u.assetActionHints(lang, width, true, true)...)
	rows := 1 // Separator.
	for _, hint := range hints {
		rows += len(classicHintLines(hint, width))
	}
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
	end = min(max(currentOffset, 0), total)
	start = max(1, end-currentCount+1)
	return start, end
}

func (u *UserSelectHandler) displayNoResultMsg(_ string, tips string) {
	lang := i18n.NewLang(u.h.i18nLang)
	if u.loadErr == nil && len(u.searchKeys) > 0 {
		tips = lang.T("No matching assets")
	}
	width, _ := u.h.GetPtySize()
	utils.IgnoreErrWriteString(u.h.term, utils.CharClear)
	searchHints := u.searchSyntaxHints(lang, width)
	for _, line := range classicHintLines(tips, width) {
		utils.IgnoreErrWriteString(u.h.term, utils.WrapperString(line, utils.Red)+utils.CharNewLine)
	}
	hints := make([]string, 0, 3)
	hints = append(hints, compactClassicHintRows(width, u.currentSearchTip(lang), u.scopePathHint(lang))...)
	hints = append(hints, searchHints...)
	hints = append(hints, compactClassicHintRows(width,
		fmt.Sprintf("[b] %s", u.classicBackTarget(lang)), fmt.Sprintf("[?] %s", lang.T("View help")),
		classicExitHint(lang))...)
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

func (u *UserSelectHandler) searchSyntaxHints(lang i18n.LanguageCode, width int) []string {
	if u.currentType == TypeNodeAsset || u.currentType == TypeTypeAsset || u.currentType == TypeFavoriteAsset {
		return compactClassicHintRows(width, strings.Split(lang.T(
			"Search syntax: / + IP, hostname, comment (global search)  // + IP, hostname, comment (multi-level search within current scope)"), " · ")...)
	}
	if currentSearchSummary(u.searchKeys, "") == "" {
		return []string{fmt.Sprintf(lang.T("Search tip: %s"), lang.T("/ + IP, Hostname, Comment"))}
	}
	return compactClassicHintRows(width, strings.Split(lang.T(
		"Search syntax: / + IP, hostname, comment (global search)  // + IP, hostname, comment (multi-level search)"), " · ")...)
}
