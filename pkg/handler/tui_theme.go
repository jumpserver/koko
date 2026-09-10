package handler

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
)

func (h *terminalUI) showAppearance() {
	if h.modal && h.dialogs[len(h.dialogs)-1].page == "appearance" {
		return
	}
	mode := tuiDropdown().SetLabel(h.tr("模式", "Mode") + " ")
	accent := tuiDropdown().SetLabel(h.tr("强调色", "Accent color") + " ")
	mode.SetOptions([]string{h.tr("深色主题", "Dark theme"), h.tr("浅色主题", "Light theme")}, nil)
	selected := 0
	if h.lightTheme {
		selected = 1
	}
	mode.SetCurrentOption(selected)
	colors := []string{h.tr("青绿", "Teal"), h.tr("蓝色", "Blue"), h.tr("紫色", "Purple"), h.tr("橙色", "Orange"), h.tr("粉色", "Pink"), h.tr("青蓝", "Cyan"), h.tr("灰色", "Gray"), h.tr("红色", "Red"), h.tr("青柠", "Lime")}
	for i, label := range colors {
		colors[i] = fmt.Sprintf("[#%06x]●[-] %s", tui.AccentSwatch(i).Hex(), label)
	}
	accent.SetOptions(colors, nil).SetCurrentOption(h.accentColor)
	mode.SetSelectedFunc(func(_ string, i int) { h.changeAppearance(i == 1, h.accentColor) })
	accent.SetSelectedFunc(func(_ string, i int) { h.changeAppearance(h.lightTheme, i) })
	labelWidth := max(tview.TaggedStringWidth(mode.GetLabel()), tview.TaggedStringWidth(accent.GetLabel())) + 1
	for _, dropdown := range []*tview.DropDown{mode, accent} {
		dropdown.SetLabelWidth(labelWidth).SetFieldWidth(0).
			SetLabelStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel))
	}
	close := tview.NewButton(h.tr("关闭", "Close") + " · Esc").SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected).SetSelectedFunc(h.dismissModal)
	reset := tview.NewButton(h.tr("还原主题色", "Reset accent")).SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected).
		SetSelectedFunc(func() { accent.SetCurrentOption(0) })
	buttons := tview.NewFlex().AddItem(reset, 0, 1, false).AddItem(close, 0, 1, false)
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(mode, 1, 0, true).AddItem(nil, 0, 1, false).
		AddItem(accent, 1, 0, false).AddItem(nil, 0, 1, false).AddItem(buttons, 1, 0, false)
	content.Box = tview.NewBox()
	tuiBorder(content.Box, "◇ "+h.tr("主题", "Theme"), tui.FocusBorder)
	content.SetBorderPadding(0, 0, 2, 2)
	h.openDialog("appearance", &tuiOverlay{Box: tview.NewBox(), child: content, width: 56, height: 7}, []tview.Primitive{mode, accent, reset, close})
}

func (h *terminalUI) changeAppearance(light bool, accent int) {
	if light == h.lightTheme && accent == h.accentColor {
		return
	}
	palette := tui.ThemePalette(light, accent)
	for _, session := range h.sessions {
		if err := session.terminal.SetPalette(palette); err != nil {
			h.fail(err)
			return
		}
	}
	h.lightTheme, h.accentColor = light, accent
	h.themeScreen.SetPalette(palette)
	h.dirty.Store(true)
}
