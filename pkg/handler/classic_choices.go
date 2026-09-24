package handler

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *InteractiveHandler) chooseAccount(permAccounts []model.PermAccount, protocol string,
	backToProtocol, retry bool) (account model.PermAccount, ok, back bool) {
	lang := i18n.NewLang(h.i18nLang)
	switch len(permAccounts) {
	case 0:
		_, _ = io.WriteString(h.term, lang.T("No account found.")+"\n\r")
		return account, false, false
	case 1:
		if !retry {
			return permAccounts[0], true, false
		}
	}
	displayAccounts := model.PermAccountList(permAccounts)
	sort.Sort(displayAccounts)
	render := func() (string, []classicHintRow, int) {
		lang := i18n.NewLang(h.i18nLang)
		width, _ := h.GetPtySize()
		backTarget := lang.T("Back to assets")
		if backToProtocol {
			backTarget = lang.T("Back to protocols")
		}
		hints := classicHintRows(classicHintPlain, compactClassicHintRows(width,
			fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String()),
			fmt.Sprintf("%s: %s", lang.T("Protocol"), protocol)))
		hints = append(hints, classicHintRows(classicHintShortcut, compactClassicHintRows(width,
			lang.T("[number] Select an account"), fmt.Sprintf("[b] %s", backTarget),
			fmt.Sprintf("[?] %s", lang.T("View help")), classicExitHint(lang)))...)
		return accountChoiceTable(displayAccounts, width, lang), hints, width
	}
	tableText, accountHints, width := render()
	if retry && len(displayAccounts) > 1 {
		tableText = ""
		backTarget := "Back to assets"
		if backToProtocol {
			backTarget = "Back to protocols"
		}
		accountHints = classicHintRows(classicHintShortcut, compactClassicHintRows(width,
			lang.T("[number] Select an account"), fmt.Sprintf("[b] %s", lang.T(backTarget)),
			fmt.Sprintf("[?] %s", lang.T("View help")), classicExitHint(lang)))
	}
	number, back, err := h.readClassicChoice(tableText, accountHints, width, "Account> ", len(displayAccounts), "Back to account list", render)
	if err != nil {
		if !h.exitRequested && !h.classicNavigation {
			logger.Errorf("select account err: %s", err)
		}
		return account, false, false
	}
	if back {
		return account, false, true
	}
	return displayAccounts[number-1], true, false
}

func accountChoiceTable(accounts []model.PermAccount, width int, lang i18n.LanguageCode) string {
	if width < 50 {
		var result strings.Builder
		for i, account := range accounts {
			for _, line := range classicIndentedLines(fmt.Sprintf("%d. ", i+1), account.Name, width) {
				result.WriteString(line + utils.CharNewLine)
			}
			if account.Username != "" {
				for _, line := range classicIndentedLines("  ", lang.T("Username")+": "+account.Username, width) {
					result.WriteString(line + utils.CharNewLine)
				}
			}
		}
		return result.String()
	}
	data := make([]map[string]string, len(accounts))
	for i, account := range accounts {
		data[i] = map[string]string{
			"ID": strconv.Itoa(i + 1), "Name": account.Name, "Username": account.Username,
		}
	}
	table := common.WrapperTable{
		Fields:     []string{"ID", "Name", "Username"},
		Labels:     []string{lang.T("Number"), lang.T("Name"), lang.T("Username")},
		FieldsSize: map[string][3]int{"ID": {0, 0, 5}, "Name": {0, 8, 0}, "Username": {0, 10, 0}},
		Data:       data, TotalSize: width, TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	return table.Display()
}

func classicCompactChoiceRows(values []string, width int) string {
	var result strings.Builder
	for i, value := range values {
		for _, line := range classicIndentedLines(fmt.Sprintf("%d. ", i+1), value, width) {
			result.WriteString(line + utils.CharNewLine)
		}
	}
	return result.String()
}

func (h *InteractiveHandler) chooseAssetProtocol(protocols []string) (string, bool) {
	lang := i18n.NewLang(h.i18nLang)
	switch len(protocols) {
	case 0:
		_, _ = io.WriteString(h.term, lang.T("No protocol found.")+"\n\r")
		return "", false
	case 1:
		return protocols[0], true
	}
	render := func() (string, []classicHintRow, int) {
		lang := i18n.NewLang(h.i18nLang)
		width, _ := h.GetPtySize()
		tableText := ""
		if width < 50 {
			tableText = classicCompactChoiceRows(protocols, width)
		} else {
			data := make([]map[string]string, len(protocols))
			for i, protocol := range protocols {
				data[i] = map[string]string{"ID": strconv.Itoa(i + 1), "Protocol": protocol}
			}
			table := common.WrapperTable{
				Fields: []string{"ID", "Protocol"}, Labels: []string{lang.T("ID"), lang.T("Protocol")},
				FieldsSize: map[string][3]int{"ID": {0, 0, 5}, "Protocol": {0, 8, 0}},
				Data:       data, TotalSize: width, TruncPolicy: common.TruncMiddle,
			}
			table.Initial()
			tableText = table.Display()
		}
		hints := []classicHintRow{{
			text:  fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String()),
			style: classicHintPlain,
		}}
		hints = append(hints, classicHintRows(classicHintShortcut, compactClassicHintRows(width,
			lang.T("[number] Select a protocol"),
			fmt.Sprintf("[b] %s", lang.T("Back to assets")), fmt.Sprintf("[?] %s", lang.T("View help")), classicExitHint(lang)))...)
		return tableText, hints, width
	}
	tableText, hints, width := render()
	number, back, err := h.readClassicChoice(tableText, hints, width, "Protocol> ", len(protocols), "Back to protocols", render)
	if err != nil {
		if !h.exitRequested && !h.classicNavigation {
			logger.Errorf("select protocol err: %s", err)
		}
		return "", false
	}
	if back {
		return "", false
	}
	return protocols[number-1], true
}
