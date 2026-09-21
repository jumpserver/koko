package handler

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/olekukonko/tablewriter"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) displayK8sResult(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		u.displayNoResultMsg(searchHeader, lang.T("No kubernetes"))
		return
	}
	u.displayAssets(searchHeader)
}

func (u *UserSelectHandler) displayResult(_ string, labels, fields []string,
	fieldSize map[string][3]int, data []map[string]string) {
	lang := i18n.NewLang(u.h.i18nLang)
	vt := u.h.term
	width, _ := u.h.GetPtySize()

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
	start, end := resultDisplayRange(u.CurrentOffSet(), len(u.currentResult), u.TotalCount())
	searchTip := u.searchSyntaxHint(lang)
	searchSummary := currentSearchSummary(u.searchKeys, "")
	currentSearchTip := ""
	if searchSummary != "" {
		currentSearchTip = fmt.Sprintf(lang.T("Current search: %s"), searchSummary)
	}
	info := compactClassicHintRows(width, u.scopePathHint(lang), currentSearchTip)
	actionParts := make([]string, 0, 4)
	if u.HasPrev() {
		actionParts = append(actionParts, fmt.Sprintf("[b] %s", lang.T("Previous")))
	}
	if u.HasNext() {
		actionParts = append(actionParts, fmt.Sprintf("[n] %s", lang.T("Next")))
	}
	actionParts = append(actionParts,
		fmt.Sprintf("[%s] %s", lang.T("Number"), lang.T("Connect")),
		fmt.Sprintf("[?] %s", lang.T("Help")))
	if u.TotalPage() > 1 {
		pageInfo := fmt.Sprintf(lang.T("Asset range: %s  [b] Previous page  [n/Enter] Next page"),
			fmt.Sprintf("%d-%d/%d", start, end, u.TotalCount()))
		if shortcutPos := strings.Index(pageInfo, "  [b]"); shortcutPos >= 0 {
			pageInfo = strings.TrimSpace(pageInfo[:shortcutPos])
		}
		info = append(info, pageInfo)
	}
	hints := append(info, searchTip)
	hints = append(hints, compactClassicHintRows(width, actionParts...)...)
	_, _ = vt.Write([]byte(utils.CharClear))
	_, _ = vt.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(vt, classicHintPanel(hints, width))
}

type classicHint struct {
	label string
	value string
}

func splitClassicHint(text string) classicHint {
	separator := strings.IndexAny(text, ":：")
	if separator < 0 {
		return classicHint{value: strings.TrimSpace(text)}
	}
	separatorSize := 1
	if strings.HasPrefix(text[separator:], "：") {
		separatorSize = len("：")
	}
	return classicHint{
		label: strings.TrimSpace(text[:separator]),
		value: strings.TrimSpace(text[separator+separatorSize:]),
	}
}

func classicHintLine(text string, width int) string {
	hint := splitClassicHint(text)
	if hint.label == "" {
		return highlightClassicShortcuts(runewidth.Truncate(hint.value, width, "…"))
	}
	available := width - runewidth.StringWidth(hint.label) - 2
	if available <= 0 {
		return runewidth.Truncate(hint.label, width, "…")
	}
	hint.value = runewidth.Truncate(hint.value, available, "…")
	return hint.label + "  " + highlightClassicShortcuts(hint.value)
}

func compactClassicHintRows(width int, texts ...string) []string {
	filtered := make([]string, 0, len(texts))
	for _, text := range texts {
		if text = strings.TrimSpace(text); text != "" {
			filtered = append(filtered, text)
		}
	}
	if len(filtered) <= 1 {
		return filtered
	}
	rows := make([]string, 0, len(filtered))
	current := filtered[0]
	for _, text := range filtered[1:] {
		combined := current + "  ·  " + text
		if width > 0 && runewidth.StringWidth(combined) > width {
			rows = append(rows, current)
			current = text
			continue
		}
		current = combined
	}
	return append(rows, current)
}

func classicHintPanel(texts []string, width int) string {
	if len(texts) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString(strings.Repeat("─", max(1, width)))
	result.WriteString(utils.CharNewLine)
	for _, text := range texts {
		result.WriteString(classicHintLine(text, width))
		result.WriteString(utils.CharNewLine)
	}
	return result.String()
}

func highlightClassicShortcuts(value string) string {
	searchSyntax := strings.Contains(value, "IP") && strings.Contains(value, "/")
	highlightNumber := strings.Contains(value, "Enter") || strings.Contains(value, "回车")
	numberTokens := []string{"number", "编号", "編號", "番号", "번호", "número", "номер", "số"}
	var result strings.Builder
	for i := 0; i < len(value); {
		if value[i] == '[' {
			if end := strings.IndexByte(value[i:], ']'); end >= 0 {
				end += i + 1
				result.WriteString(utils.WrapperString(value[i:end], utils.Green, true))
				i = end
				continue
			}
		}
		if searchSyntax && value[i] == '/' {
			end := i + 1
			if end < len(value) && value[end] == '/' {
				end++
			}
			result.WriteString(utils.WrapperString(value[i:end], utils.Green, true))
			i = end
			continue
		}
		if value[i] == '?' {
			result.WriteString(utils.WrapperString("?", utils.Green, true))
			i++
			continue
		}
		if highlightNumber {
			matched := false
			for _, token := range numberTokens {
				if strings.HasPrefix(value[i:], token) {
					result.WriteString(utils.WrapperString(token, utils.Green, true))
					i += len(token)
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		matchedShortcut := false
		for _, token := range []string{"Enter", "回车"} {
			if strings.HasPrefix(value[i:], token) {
				result.WriteString(utils.WrapperString(token, utils.Green, true))
				i += len(token)
				matchedShortcut = true
				break
			}
		}
		if matchedShortcut {
			continue
		}
		_, size := utf8.DecodeRuneInString(value[i:])
		result.WriteString(value[i : i+size])
		i += size
	}
	return result.String()
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
	width, _ := u.h.GetPtySize()
	searchTip := u.searchSyntaxHint(lang)
	searchSummary := currentSearchSummary(u.searchKeys, "")
	currentSearchTip := ""
	if searchSummary != "" {
		currentSearchTip = fmt.Sprintf(lang.T("Current search: %s"), searchSummary)
	}
	utils.IgnoreErrWriteString(u.h.term, utils.WrapperString(tips, utils.Red))
	utils.IgnoreErrWriteString(u.h.term, utils.CharNewLine)
	hints := compactClassicHintRows(width, u.scopePathHint(lang), currentSearchTip)
	hints = append(hints, searchTip, fmt.Sprintf("[?] %s", lang.T("Help")))
	utils.IgnoreErrWriteString(u.h.term, classicHintPanel(hints, width))
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

func (u *UserSelectHandler) searchSyntaxHint(lang i18n.LanguageCode) string {
	if u.currentType == TypeNodeAsset || u.currentType == TypeTypeAsset || u.currentType == TypeFavoriteAsset {
		return lang.T("Search syntax: / + IP, hostname, comment (global search)  // + IP, hostname, comment (multi-level search within current scope)")
	}
	if currentSearchSummary(u.searchKeys, "") == "" {
		syntax := strings.Replace(lang.T("/ + IP, Hostname, Comment"), "/ + ", "/ ", 1)
		return fmt.Sprintf("%s: %s", lang.T("Search"), syntax)
	}
	return lang.T("Search syntax: / + IP, hostname, comment (global search)  // + IP, hostname, comment (multi-level search)")
}
