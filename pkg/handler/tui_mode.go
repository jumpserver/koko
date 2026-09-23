package handler

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
)

func (h *terminalUI) showUserMenu() {
	h.closeDropdown()
	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).
		SetMainTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).SetSelectedStyle(tuiButtonFocusedStyle)
	quitLabel := h.tr("退出", "Quit")
	list.AddItem(quitLabel, "", 0, h.quit)
	tuiDialogBorder(list.Box, "")
	list.SetBorderPadding(0, 0, 2, 2)
	width := max(18, min(46, max(tview.TaggedStringWidth(h.identity.GetLabel())+2,
		tview.TaggedStringWidth(quitLabel)+6)))
	h.openDialog("user", &tuiOverlay{Box: tview.NewBox(), child: list, width: width, height: 3, anchor: h.identity}, []tview.Primitive{list})
}

func (h *terminalUI) switchToTextMode() {
	h.windowPrefix = false
	switchMode := func() {
		if h.preferences != nil && h.user != nil {
			h.preferences.storeTerminalMode(h.user.ID, terminalModeText)
		}
		h.nextMode = terminalModeText
		h.app.Stop()
	}
	if len(h.sessions) == 0 {
		switchMode()
		return
	}
	dialog := tview.NewModal().
		SetText(h.tr("切换模式会断开所有已连接的资产会话，是否继续？",
			"Switching modes will disconnect all connected asset sessions. Continue?")).
		AddButtons([]string{h.tr("取消", "Cancel"), h.tr("切换模式", "Switch mode")}).
		SetDoneFunc(func(index int, _ string) {
			if index == 1 {
				switchMode()
			} else {
				h.dismissModal()
			}
		})
	dialog.SetBackgroundColor(tui.Panel).SetTextColor(tui.Foreground).
		SetButtonStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).
		SetButtonActivatedStyle(tuiButtonFocusedStyle).SetBorderColor(tui.FocusBorder)
	dialog.Box.SetBackgroundColor(tui.Panel)
	dialog.SetTitle("  " + h.tr("切换交互模式", "Switch interaction mode") + " ").
		SetTitleAlign(tview.AlignLeft).SetTitleColor(tui.Accent)
	tui.RoundedBorder(dialog.Box)
	h.openDialog("switch-mode", dialog, nil)
}
