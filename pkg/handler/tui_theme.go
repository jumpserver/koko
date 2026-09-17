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
	accentNames := []string{h.tr("青绿", "Teal"), h.tr("蓝色", "Blue"), h.tr("紫色", "Purple"), h.tr("橙色", "Orange"), h.tr("粉色", "Pink"), h.tr("青蓝", "Cyan"), h.tr("灰色", "Gray"), h.tr("红色", "Red"), h.tr("青柠", "Lime")}
	setAccentOptions := func(light bool) {
		colors := make([]string, len(accentNames))
		for i, label := range accentNames {
			color := tui.AccentSwatch(h.colorProfile, light, i, h.themeScreen.Colors())
			colors[i] = fmt.Sprintf("[#%06x]●[-] %s", color.Hex(), label)
		}
		accent.SetOptions(colors, func(_ string, i int) { h.changeAppearance(h.lightTheme, i) }).SetCurrentOption(h.accentColor)
	}
	setAccentOptions(h.lightTheme)
	mode.SetSelectedFunc(func(_ string, i int) {
		light := i == 1
		h.changeAppearance(light, h.accentColor)
		setAccentOptions(light)
	})
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
	tuiDialogBorder(content.Box, h.tr("主题", "Theme"))
	h.openDialog("appearance", &tuiOverlay{Box: tview.NewBox(), child: content, width: 56, height: 9}, []tview.Primitive{mode, accent, reset, close})
}

func (h *terminalUI) changeAppearance(light bool, accent int) {
	if light == h.lightTheme && accent == h.accentColor {
		return
	}
	palette := tui.ThemePaletteForProfile(light, accent, h.colorProfile)
	for _, session := range h.sessions {
		if err := session.terminal.SetPalette(palette); err != nil {
			h.fail(err)
			return
		}
	}
	h.lightTheme, h.accentColor = light, accent
	h.themeScreen.SetPalette(palette)
	if h.preferences != nil && h.user != nil {
		h.preferences.storeAppearance(h.user.ID, light, accent)
	}
	h.dirty.Store(true)
}
