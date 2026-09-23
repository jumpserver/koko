package handler

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
	"github.com/jumpserver/koko/pkg/i18n"
)

func (h *terminalUI) showLanguage() {
	if len(h.dialogs) > 0 && h.dialogs[len(h.dialogs)-1].page == "language" {
		return
	}
	list := tview.NewList().ShowSecondaryText(false).
		SetMainTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).
		SetSelectedStyle(tui.Selected.Bold(true)).SetHighlightFullLine(true)
	list.SetBackgroundColor(tui.Panel)
	current := i18n.NewLang(h.data.lang)
	for i, code := range i18n.AllCodes {
		list.AddItem(fmt.Sprintf("%d  %s", i+1, i18n.AllLangCodesStr[i]), "", 0, func() { h.changeLanguage(code) })
		if current == code {
			list.SetCurrentItem(i)
		}
	}
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune && ev.Rune() >= '1' && ev.Rune() < '1'+rune(len(i18n.AllCodes)) {
			h.changeLanguage(i18n.AllCodes[ev.Rune()-'1'])
			return nil
		}
		return ev
	})
	close := tview.NewButton(h.tr("关闭", "Close") + " · Esc").SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tuiButtonFocusedStyle).SetSelectedFunc(h.dismissModal)
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(list, 0, 1, true).AddItem(nil, 1, 0, false).AddItem(close, 1, 0, false)
	content.Box = tview.NewBox()
	tuiDialogBorder(content.Box, h.tr("语言", "Language"))
	h.openDialog("language", &tuiOverlay{Box: tview.NewBox(), child: content, width: 46, height: len(i18n.AllCodes) + 6}, []tview.Primitive{list, close})
}

func (h *terminalUI) changeLanguage(code i18n.LanguageCode) {
	for h.modal {
		h.dismissModal()
	}
	if h.preferences != nil && h.user != nil {
		h.preferences.storeLanguage(h.user.ID, code.String())
	}
	if h.data.lang == code.String() {
		return
	}
	h.data.lang = code.String()
	h.refreshLabels()
	h.refreshSessionTabs()
	h.switchTree(h.scope.Mode)
	if !h.organizationsReady {
		h.loadWorkspace()
	}
	// Existing remote sessions retain their original language and input stream.
	if h.activeSession >= 0 {
		h.app.SetFocus(h.popup)
	}
}
