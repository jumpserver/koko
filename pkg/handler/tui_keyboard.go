package handler

import (
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
)

type tuiDialog struct {
	page        string
	returnFocus tview.Primitive
	focus       []tview.Primitive
	shortcuts   []tuiShortcut
}

func (h *terminalUI) focusOrder() []tview.Primitive {
	if len(h.dialogs) > 0 {
		return h.dialogs[len(h.dialogs)-1].focus
	}
	if h.fullscreen {
		return []tview.Primitive{h.sessions[h.activeSession].controls[2], h.popup}
	}
	order := []tview.Primitive{h.sessionTabs}
	if h.tabsOverflow {
		order = append(order, h.sessionMore)
	}
	order = append(order, h.language, h.appearance)
	if h.organizationsEnabled && h.activeSession < 0 && !h.sidebarHidden {
		order = append(order, h.org)
	}
	if h.activeSession >= 0 {
		session := h.sessions[h.activeSession]
		for _, control := range session.controls {
			if !control.(*tview.Button).IsDisabled() {
				order = append(order, control)
			}
		}
		return append(order, session.terminal)
	}
	if !h.sidebarHidden {
		order = append(order, h.treeKind, h.tree)
		for _, b := range h.treeActions {
			order = append(order, b)
		}
	}
	order = append(order, h.search, h.assetRefresh, h.table)

	if h.total > tuiPageSize {
		order = append(order, h.pagerButtons[0], h.pagerButtons[1])
	}
	return order
}

// A DropDown delegates focus to its internal list while open. Close that list
// before moving focus, so Tab cannot leave an invisible focused menu behind.
func (h *terminalUI) closeDropdown() bool {
	h.clearDropdownNumber()
	for _, item := range h.focusOrder() {
		if dropdown, ok := item.(*tview.DropDown); ok && dropdown.HasFocus() && dropdown.IsOpen() {
			dropdown.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { h.app.SetFocus(p) })
			return true
		}
	}
	return false
}

func (h *terminalUI) cycleFocus(backward bool) bool {
	order := h.focusOrder()
	if len(order) == 0 { // tview.Modal owns the focus cycle of its buttons.
		return false
	}
	index := -1
	for i, item := range order {
		if item.HasFocus() {
			index = i
			break
		}
	}
	h.closeDropdown()
	if index < 0 && backward {
		index = 0
	}
	step := 1
	if backward {
		step = len(order) - 1
	}
	h.app.SetFocus(order[(index+step+len(order))%len(order)])
	return true
}

func (h *terminalUI) input(ev *tcell.EventKey) *tcell.EventKey {
	h.drainUpdates()
	if ev.Key() == tuiWakeKey {
		if !h.dropdownNumberDue.IsZero() && !time.Now().Before(h.dropdownNumberDue) {
			h.commitDropdownNumber()
		}
		return nil
	}
	h.lastInput = time.Now()
	if ev.Key() != tcell.KeyRune || ev.Rune() < '0' || ev.Rune() > '9' {
		h.clearDropdownNumber()
	}
	// The same context-dependent bindings drive dispatch, inline labels and help.
	prefix := h.windowPrefix
	for _, binding := range h.shortcuts() {
		if binding.matches(ev) {
			if binding.id != "fullscreen" && binding.id != "window" {
				h.setFullscreen(false)
			}
			h.windowPrefix = false
			binding.run()
			return nil
		}
	}
	if prefix {
		h.windowPrefix = false // Unknown window commands cancel without reaching SSH.
		return nil
	}
	// Border controls are drawn over their owning panel rather than occupying
	// a Flex row. Route their native keys explicitly, just like mounted buttons.
	for _, p := range h.focusOrder() {
		if button, ok := p.(*tview.Button); ok && button.HasFocus() {
			button.InputHandler()(ev, func(next tview.Primitive) { h.app.SetFocus(next) })
			return nil
		}
	}
	if !h.modal && h.popup != nil && h.popup.HasFocus() {
		h.popup.InputHandler()(ev, func(tview.Primitive) {})
		return nil
	}
	return ev
}

func (h *terminalUI) editing() bool {
	for _, item := range h.focusOrder() {
		switch p := item.(type) {
		case *tview.InputField:
			if item.HasFocus() {
				return true
			}
		case *tview.DropDown:
			if p.IsOpen() {
				return true
			}
		}
	}
	return false
}

// Region actions restore the asset panel before focusing one of its controls.
func (h *terminalUI) focusArea(p tview.Primitive) {
	h.closeDropdown()
	if h.activeSession >= 0 {
		h.activateSession(-1)
	}
	if h.sidebarHidden && (p == h.tree || p == h.treeKind || p == h.org) {
		h.toggleSidebar()
	}
	h.app.SetFocus(p)
	if dropdown, ok := p.(*tview.DropDown); ok && dropdown.GetOptionCount() > 0 {
		dropdown.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(next tview.Primitive) { h.app.SetFocus(next) })
	}
}

func (h *terminalUI) escape() {
	if h.closeDropdown() {
		return
	}
	if h.modal {
		h.dismissModal()
		return
	}
	if h.popup != nil {
		h.app.SetFocus(h.popup)
	} else if h.search.HasFocus() || h.sidebarHidden {
		h.app.SetFocus(h.table)
	} else {
		h.app.SetFocus(h.tree)
	}
}

func (h *terminalUI) openDialog(page string, child tview.Primitive, focus []tview.Primitive) {
	h.setFullscreen(false)
	h.closeDropdown()
	// A late account lookup must not open a second dialog over newly opened help.
	h.detailGeneration++
	h.dialogs = append(h.dialogs, tuiDialog{page: page, returnFocus: h.app.GetFocus(), focus: focus})
	h.modal = true
	h.pages.AddPage(page, child, true, true)
	if len(focus) > 0 {
		h.app.SetFocus(focus[0])
	} else {
		h.app.SetFocus(child)
	}
	h.setWindowHelp()
}

func (h *terminalUI) showKeyboardHelp() {
	if len(h.dialogs) > 0 && h.dialogs[len(h.dialogs)-1].page == "help" {
		h.dismissModal()
		return
	}
	// Keep help actionable: commands listed here close help and execute in the
	// original control. Navigation, Enter and Esc belong to the help itself.
	var commands []tuiShortcut
	remote := !h.modal && h.popup != nil && h.popup.HasFocus()
	// Show the actual window commands in help opened from a remote terminal.
	if remote {
		h.windowPrefix = true
	}
	available := h.shortcuts()
	h.windowPrefix = false
	for _, binding := range available {
		switch binding.id {
		case "help", "back", "quit", "focus", "window", "literal-prefix":
			continue
		}
		if binding.run == nil {
			continue
		}
		// Window bindings also accept arrows and Tab. Inside help those keys
		// belong to scrolling and focus; retain only the advertised n/b aliases.
		switch binding.key {
		case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyLeft, tcell.KeyRight:
			binding.key = 0
		}
		if binding.key == 0 && binding.runes == "" {
			continue
		}
		action := binding.run
		binding.run = func() { h.dismissModal(); action() }
		binding.compact = false
		commands = append(commands, binding)
	}
	help := tview.NewTextView().SetTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).SetText(h.keyboardHelp(commands)).ScrollToBeginning()
	tuiBorder(help.Box, h.tr("帮助", "Help"), tui.Accent)
	close := tview.NewButton(h.tr("关闭", "Close") + " · Esc").SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected).SetSelectedFunc(h.dismissModal)
	help.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			h.dismissModal()
		}
	})
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(help, 0, 1, true).AddItem(close, 1, 0, false)
	content.SetBackgroundColor(tui.Panel)
	h.openDialog("help", &tuiOverlay{Box: tview.NewBox(), child: content, width: 94, height: 30}, []tview.Primitive{help, close})
	h.dialogs[len(h.dialogs)-1].shortcuts = commands
}

func (h *terminalUI) setWindowHelp() {
	exitKey := "Ctrl+C"
	remote := h.popup != nil && h.popup.HasFocus()
	if h.windowPrefix {
		exitKey = "q"
	} else if remote {
		exitKey = "Ctrl+] q"
	}
	h.exitHint = ""
	if !h.modal {
		h.exitHint = exitKey + " " + h.tr("退出", "Quit")
	}
	bindings := h.shortcuts()
	h.refreshShortcutLabels(bindings)
	// Common hints retain their order and width across focus and dialog changes.
	// Remote input and the explicit window prefix have their own command sets.
	hints := []tuiShortcut{
		{id: "help", label: "h / ?", description: h.tr("帮助", "Help")},
		{id: "focus", label: "Tab / Shift+Tab", description: h.tr("切换控件", "Focus")},
		{id: "back", label: "Esc", description: h.tr("返回", "Back")},
		{id: "control", label: "Enter", description: h.tr("确认", "Confirm")},
		{id: "full-text", label: "v", description: h.tr("完整内容", "Full text")},
		{id: "control", label: "↑↓ / PgUp / PgDn", description: h.tr("移动和滚动", "Move and scroll")},
		{id: "search-results", label: "↓", description: h.tr("资产", "Assets")},
		{id: "clear-search", label: "Ctrl+U", description: h.tr("清空搜索", "Clear search")},
	}
	if h.windowPrefix || remote {
		hints = nil
		for _, binding := range bindings {
			if binding.compact && binding.label != "" {
				hints = append(hints, binding)
			}
		}
	} else if len(h.sessions) > 0 {
		hints = append(hints, tuiShortcut{id: "window", label: "Ctrl+]", description: h.tr("窗口栏", "Window bar")})
	}
	w, _ := h.screen.Size()
	width := max(1, w-tuiClockWidth-5)
	lines := []string{tuiKeyText(exitKey, !h.modal) + " " + h.tr("退出", "Quit")}
	for _, binding := range hints {
		hint := tuiKeyText(binding.label, tuiShortcutAvailable(bindings, binding.id, binding.label)) + " " + binding.description
		if tview.TaggedStringWidth(hint) > width {
			continue
		}
		last := len(lines) - 1
		joined := lines[last] + " · " + hint
		if tview.TaggedStringWidth(joined) <= width {
			lines[last] = joined
		} else if len(lines) < 2 {
			lines = append(lines, hint)
		}
	}
	text := strings.Join(lines, "\n")
	if h.footer.GetText(false) != text {
		h.footer.SetText(text)
	}
	h.main.ResizeItem(h.footer, max(2, len(lines))+1, 0)
}
