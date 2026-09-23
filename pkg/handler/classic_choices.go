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
	backToProtocol, retry bool, page *int) (account model.PermAccount, ok, back bool) {
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
	width, height := h.GetPtySize()
	if len(displayAccounts) > h.accountPageSize(width, height, protocol, backToProtocol, len(displayAccounts)) {
		return h.choosePagedAccount(displayAccounts, protocol, backToProtocol, retry, page)
	}
	var tableText string
	var accountHints []string
	backTarget := lang.T("Back to assets")
	if backToProtocol {
		backTarget = lang.T("Back to protocols")
	}
	if !retry || len(displayAccounts) == 1 {
		tableText = accountChoiceTable(displayAccounts, 0, width, lang)
		accountHints = []string{fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String()),
			fmt.Sprintf("%s: %s", lang.T("Protocol"), protocol)}
	}
	accountHints = append(accountHints, compactClassicHintRows(width,
		fmt.Sprintf(lang.T("Enter 1-%d to choose an account"), len(displayAccounts)),
		fmt.Sprintf("[b] %s", backTarget), classicExitHint(lang))...)
	number, back, err := h.readClassicChoice(tableText, accountHints, width, "Account> ", len(displayAccounts))
	if err != nil {
		if !h.exitRequested {
			logger.Errorf("select account err: %s", err)
		}
		return account, false, false
	}
	if back {
		return account, false, true
	}
	return displayAccounts[number-1], true, false
}

func accountChoiceTable(accounts []model.PermAccount, first, width int, lang i18n.LanguageCode) string {
	if width < 50 {
		var result strings.Builder
		for i, account := range accounts {
			for _, line := range classicIndentedLines(fmt.Sprintf("%d. ", first+i+1), account.Name, width) {
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
			"ID": strconv.Itoa(first + i + 1), "Name": account.Name, "Username": account.Username,
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

func (h *InteractiveHandler) accountPageSize(width, height int, protocol string, backToProtocol bool, count int) int {
	lang := i18n.NewLang(h.i18nLang)
	backTarget := lang.T("Back to assets")
	if backToProtocol {
		backTarget = lang.T("Back to protocols")
	}
	context := []string{fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String()),
		fmt.Sprintf("%s: %s", lang.T("Protocol"), protocol)}
	footer := classicHintPanel(compactClassicHintRows(width,
		fmt.Sprintf(lang.T("Enter 1-%d to choose an account"), count),
		fmt.Sprintf("[p] %s", lang.T("Previous page")),
		fmt.Sprintf("[n] %s", lang.T("Next page")),
		fmt.Sprintf("[b] %s", backTarget), classicExitHint(lang)), width)
	reserved := strings.Count(footer, utils.CharNewLine) + 4 // Table header, prompt, and spare row.
	for _, line := range context {
		reserved += len(classicHintLines(line, width))
	}
	pageSize := max(1, height-reserved)
	if width < 50 {
		pageSize = max(1, pageSize/2)
	}
	return pageSize
}

func (h *InteractiveHandler) choosePagedAccount(accounts []model.PermAccount, protocol string,
	backToProtocol, retry bool, page *int) (account model.PermAccount, ok, back bool) {
	lang := i18n.NewLang(h.i18nLang)
	backTarget := lang.T("Back to assets")
	if backToProtocol {
		backTarget = lang.T("Back to protocols")
	}
	showPage := !retry
	for {
		width, height := h.GetPtySize()
		pageSize := h.accountPageSize(width, height, protocol, backToProtocol, len(accounts))
		*page = min(*page, (len(accounts)-1)/pageSize)
		start := *page * pageSize
		end := min(start+pageSize, len(accounts))
		if showPage {
			h.resizeTerminal()
			utils.IgnoreErrWriteString(h.term, utils.CharClear)
			utils.IgnoreErrWriteString(h.term, accountChoiceTable(accounts[start:end], start, width, lang))
			for _, context := range []string{fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String()),
				fmt.Sprintf("%s: %s", lang.T("Protocol"), protocol)} {
				for _, line := range classicHintLines(context, width) {
					utils.IgnoreErrWriteString(h.term, line+utils.CharNewLine)
				}
			}
		}
		actions := []string{fmt.Sprintf(lang.T("Enter 1-%d to choose an account"), len(accounts))}
		if *page > 0 {
			actions = append(actions, fmt.Sprintf("[p] %s", lang.T("Previous page")))
		}
		if end < len(accounts) {
			actions = append(actions, fmt.Sprintf("[n] %s", lang.T("Next page")))
		}
		actions = append(actions, fmt.Sprintf("[b] %s", backTarget), classicExitHint(lang))
		utils.IgnoreErrWriteString(h.term, classicHintPanel(compactClassicHintRows(width, actions...), width))
		h.term.SetPrompt("Account> ")
		line, err := h.readClassicLine()
		if err != nil {
			if !h.exitRequested {
				logger.Errorf("select account err: %s", err)
			}
			return account, false, false
		}
		switch line {
		case "b":
			return account, false, true
		case "p":
			if *page > 0 {
				*page = *page - 1
				showPage = true
				continue
			}
		case "n":
			if end < len(accounts) {
				*page = *page + 1
				showPage = true
				continue
			}
		}
		if number, valid := classicChoiceNumber(line, len(accounts)); valid {
			return accounts[number-1], true, false
		}
		utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(lang.T("Invalid input.")))
		showPage = false
	}
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
	hints := []string{fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String())}
	hints = append(hints, compactClassicHintRows(width,
		fmt.Sprintf(lang.T("Enter 1-%d to choose a protocol"), len(protocols)),
		fmt.Sprintf("[b] %s", lang.T("Back to assets")), classicExitHint(lang))...)
	number, back, err := h.readClassicChoice(tableText, hints, width, "Protocol> ", len(protocols))
	if err != nil {
		if !h.exitRequested {
			logger.Errorf("select protocol err: %s", err)
		}
		return "", false
	}
	if back {
		return "", false
	}
	return protocols[number-1], true
}
