package handler

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *InteractiveHandler) ChangeLang() {
	lang := i18n.NewLang(h.i18nLang)
	language := h.i18nLang
	width, _ := h.GetPtySize()
	tableText := ""
	if width < 50 {
		tableText = classicCompactChoiceRows(i18n.AllLangCodesStr, width)
	} else {
		data := make([]map[string]string, len(i18n.AllLangCodesStr))
		for i, name := range i18n.AllLangCodesStr {
			data[i] = map[string]string{"ID": strconv.Itoa(i + 1), "Name": name}
		}
		table := common.WrapperTable{
			Fields: []string{"ID", "Name"}, Labels: []string{lang.T("Number"), lang.T("Name")},
			FieldsSize: map[string][3]int{"ID": {0, 0, 5}, "Name": {0, 8, 0}},
			Data:       data, TotalSize: width, TruncPolicy: common.TruncMiddle,
		}
		table.Initial()
		tableText = table.Display()
	}
	hints := compactClassicHintRows(width,
		fmt.Sprintf(lang.T("Enter 1-%d to change language"), len(i18n.AllCodes)),
		fmt.Sprintf("[b] %s", lang.T("Back to help")), classicExitHint(lang))
	number, back, err := h.readClassicChoice(tableText, hints, width, "Language> ", len(i18n.AllCodes))
	if err != nil {
		if !h.exitRequested {
			logger.Errorf("User %s switch language err %s", h.user.Name, err)
		}
		return
	}
	if back {
		return
	}
	lang = i18n.AllCodes[number-1]
	language = lang.String()
	if language != h.i18nLang {
		setAPIClientLang(h.jmsService, language)
		if h.preferences != nil {
			h.preferences.storeLanguage(h.user.ID, language)
		}
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(lang.T("Switch language successfully"), utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	}
	h.i18nLang = language
}
