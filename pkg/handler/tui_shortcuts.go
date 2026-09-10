package handler

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
	"github.com/jumpserver/koko/pkg/i18n"
)

// Prefer the first letter of a word. Translations without that Latin letter
// keep a compact mnemonic beside the name; SSH chords always remain explicit.
func tuiMnemonic(label, key string) string {
	return tuiMnemonicState(label, key, true)
}

func tuiKeyText(key string, enabled bool) string {
	prefix := "[" + tui.Accent.String() + "::bu]"
	if !enabled {
		prefix = "[" + tui.Muted.String() + "::-][::U]"
	}
	return prefix + key + "[::U][-::-]"
}

func tuiMnemonicState(label, key string, enabled bool) string {
	if key == "" {
		return label
	}
	mark := func(s string) string { return tuiKeyText(s, enabled) }
	if len(key) == 1 {
		first, previous := -1, rune(' ')
		for i, r := range label {
			if unicode.ToLower(r) == rune(key[0]) {
				if first < 0 {
					first = i
				}
				if !unicode.IsLetter(previous) {
					first = i
					break
				}
			}
			previous = r
		}
		if first >= 0 {
			return label[:first] + mark(label[first:first+1]) + label[first+1:]
		}
	}
	if prefix, letter, ok := strings.Cut(key, " "); ok {
		return label + " " + prefix + " " + mark(letter)
	}
	return label + " " + mark(key)
}

func tuiPlainMnemonic(label string) string {
	return strings.NewReplacer("["+tui.Accent.String()+"::bu]", "", "["+tui.Accent.String()+"::b]", "", "["+tui.Accent.String()+"::u]", "", "["+tui.Accent.String()+"::-]", "", "["+tui.Muted.String()+"::-][::U]", "", "[::U][-::-]", "", "[-::-]", "").Replace(label)
}

func tuiShortcutAvailable(bindings []tuiShortcut, id, key string) bool {
	for _, binding := range bindings {
		if binding.id == id && binding.label == key {
			return true
		}
	}
	return false
}

// A binding only exists while its action is available. Widget-owned navigation
// has no run function: tview receives those events through its native handler.
type tuiShortcut struct {
	id, label, description string
	key                    tcell.Key
	runes                  string
	run                    func()
	compact                bool
	modifiers              tcell.ModMask
}

func (s tuiShortcut) matches(ev *tcell.EventKey) bool {
	if s.run == nil || ev.Modifiers()&(tcell.ModAlt|tcell.ModMeta) != s.modifiers {
		return false
	}
	if s.key != 0 && ev.Key() == s.key {
		return true
	}
	return ev.Key() == tcell.KeyRune && ev.Modifiers()&tcell.ModCtrl == 0 && strings.ContainsRune(s.runes, ev.Rune())
}

// HasFocus is safe during Draw, unlike Application.GetFocus (the app is locked).
// Resolve the delegated list so full-text inspection describes the highlighted dropdown option.
func (h *terminalUI) focusedControl() tview.Primitive {
	for _, p := range h.focusOrder() {
		if !p.HasFocus() {
			continue
		}
		if d, ok := p.(*tview.DropDown); ok && d.IsOpen() {
			d.Focus(func(list tview.Primitive) { p = list })
		}
		return p
	}
	return nil
}

func (h *terminalUI) shortcuts() []tuiShortcut {
	var bindings []tuiShortcut
	add := func(id, label, description string, key tcell.Key, runes string, run func(), compact bool) {
		bindings = append(bindings, tuiShortcut{id: id, label: label, description: description, key: key, runes: runes, run: run, compact: compact})
	}
	fullscreenLabel := h.tr("全屏", "Fullscreen")
	if h.fullscreen {
		fullscreenLabel = h.tr("退出全屏", "Exit fullscreen")
	}
	areas := func() {
		add("assets", "p", h.tr("资产面板", "Asset panel"), 0, "pP", func() { h.closeDropdown(); h.activateSession(-1) }, false)
		add("tabs-area", "w", h.tr("窗口栏", "Window bar"), 0, "wW", func() { h.closeDropdown(); h.focusSessionTabs() }, false)
		if h.organizationsEnabled {
			add("organization-area", "o", h.tr("组织", "Organization"), 0, "oO", func() { h.focusArea(h.org) }, false)
		}
		add("tree-area", "t", h.tr("树类型", "Tree view"), 0, "tT", func() { h.focusArea(h.treeKind) }, false)
		add("tree-nodes", "e", h.tr("节点", "Nodes"), 0, "eE", func() { h.focusArea(h.tree) }, false)
		add("assets-area", "a", h.tr("资产", "Assets"), 0, "aA", func() { h.focusArea(h.table) }, false)
		if h.popup != nil {
			add("fullscreen", "f", fullscreenLabel, 0, "fF", func() { h.closeDropdown(); h.setFullscreen(!h.fullscreen) }, true)
			session := h.sessions[h.activeSession]
			if session.duplicate != nil && len(h.sessions) < maxTUISessions {
				add("duplicate-session", "d", h.tr("复制会话", "Duplicate session"), 0, "dD", session.duplicate, false)
			}
			add("close-session", "x", h.tr("关闭", "Close"), 0, "xX", func() { h.closeSession(session) }, false)
		}
	}
	if !h.modal {
		for number := 0; number <= 9; number++ {
			label, description := "", ""
			if number <= len(h.sessions) {
				label = fmt.Sprintf("Alt+%d", number)
				description = strings.TrimSpace(h.sessionTabLabel(number - 1))
			}
			bindings = append(bindings, tuiShortcut{id: "tab-number", label: label, description: description, runes: fmt.Sprint(number), modifiers: tcell.ModAlt, run: func() {
				h.tabHintUntil = time.Now().Add(2 * time.Second)
				if number <= len(h.sessions) {
					h.closeDropdown()
					h.activateSession(number - 1)
				}
			}})
		}
		if h.windowPrefix {
			areas()
			add("asset-index", "0", h.tr("资产面板", "Asset panel"), 0, "0", func() { h.activateSession(-1) }, false)
			for i := range h.sessions {
				label := fmt.Sprint(i + 1)
				add("session", label, cleanTUIText(h.sessions[i].name), 0, label, func() { h.activateSession(i) }, false)
			}
			if len(h.sessions) > 0 {
				add("next-window", "n / b", h.tr("下一个/上一个窗口", "Next/previous window"), tcell.KeyTab, "nN", func() { h.activateSession((h.activeSession+2)%(len(h.sessions)+1) - 1) }, true)
				add("next-window", "", "", tcell.KeyRight, "", func() { h.activateSession((h.activeSession+2)%(len(h.sessions)+1) - 1) }, false)
				previous := func() { h.activateSession((h.activeSession+len(h.sessions)+1)%(len(h.sessions)+1) - 1) }
				add("previous-window", "", "", tcell.KeyBacktab, "bB", previous, false)
				add("previous-window", "", "", tcell.KeyLeft, "", previous, false)
			}
			if h.popup != nil {
				add("literal-prefix", "Ctrl+]", h.tr("发送原始 Ctrl+]", "Send literal Ctrl+]"), tcell.KeyCtrlRightSq, "", func() { h.popup.SendInput([]byte{29}) }, false)
			}
			add("language", "l", h.tr("语言", "Language"), 0, "lL", h.showLanguage, true)
			add("appearance", "c", h.tr("主题", "Theme"), 0, "cC", h.showAppearance, false)
			add("help", "h / ?", h.tr("帮助", "Help"), 0, "?hH", h.showKeyboardHelp, true)
			add("quit", "q", h.tr("退出", "Quit"), 0, "qQ", h.quit, false)
			add("back", "Esc", h.tr("返回", "Back"), tcell.KeyEscape, "", func() {}, true)
			return bindings
		}
		add("window", "Ctrl+]", h.tr("窗口栏", "Window bar"), tcell.KeyCtrlRightSq, "", func() { h.closeDropdown(); h.windowPrefix = true }, len(h.sessions) > 0)
		if h.popup != nil && h.popup.HasFocus() {
			// These are two-key chords. Bare letters and all function keys go
			// unchanged to SSH; the prefix enables the matching actions above.
			add("assets", "Ctrl+] p", h.tr("资产面板", "Asset panel"), 0, "", nil, false)
			add("fullscreen", "Ctrl+] f", fullscreenLabel, 0, "", nil, true)
			if h.sessions[h.activeSession].duplicate != nil && len(h.sessions) < maxTUISessions {
				add("duplicate-session", "Ctrl+] d", h.tr("复制会话", "Duplicate session"), 0, "", nil, false)
			}
			add("close-session", "Ctrl+] x", h.tr("关闭", "Close"), 0, "", nil, false)
			return bindings
		}
	}

	if dropdown := h.focusedDropdown(); dropdown != nil && dropdown.GetOptionCount() > 0 {
		for digit := '0'; digit <= '9'; digit++ {
			label := ""
			if digit == '1' {
				label = fmt.Sprintf("1–%d", dropdown.GetOptionCount())
			}
			add("dropdown-number", label, h.tr("选择", "Select"), 0, string(digit), func() { h.selectDropdownNumber(dropdown, digit) }, digit == '1')
		}
	}
	// Dialogs retain their own navigation; help also executes the commands
	// available in the control from which it was opened.
	helpOpen := h.modal && h.dialogs[len(h.dialogs)-1].page == "help"
	if helpOpen {
		bindings = append(bindings, h.dialogs[len(h.dialogs)-1].shortcuts...)
	}
	if !h.editing() {
		add("help", "h / ?", h.tr("帮助", "Help"), 0, "?hH", h.showKeyboardHelp, true)
		if !helpOpen {
			if !h.modal || h.dialogs[len(h.dialogs)-1].page != "appearance" {
				add("appearance", "c", h.tr("主题", "Theme"), 0, "cC", h.showAppearance, false)
			}
			if !h.modal || h.dialogs[len(h.dialogs)-1].page != "language" {
				add("language", "l", h.tr("语言", "Language"), 0, "lL", h.showLanguage, false)
			}
			if h.focusedContent() != "" {
				add("full-text", "v", h.tr("完整内容", "Full text"), 0, "vV", h.showFullText, true)
			}
		}
	}
	if len(h.focusOrder()) > 1 || h.modal && len(h.focusOrder()) == 0 {
		if len(h.focusOrder()) > 1 {
			add("focus", "Tab / Shift+Tab", h.tr("切换控件", "Focus"), tcell.KeyTab, "", func() { h.cycleFocus(false) }, true)
			add("focus", "", "", tcell.KeyBacktab, "", func() { h.cycleFocus(true) }, false)
		} else {
			add("focus", "Tab / Shift+Tab", h.tr("切换控件", "Focus"), 0, "", nil, true)
		}
	}
	add("back", "Esc", h.tr("返回", "Back"), tcell.KeyEscape, "", h.escape, true)
	if h.modal {
		add("back", "", "", tcell.KeyCtrlC, "", h.escape, false)
	} else {
		add("quit", "Ctrl+C", h.tr("退出", "Quit"), tcell.KeyCtrlC, "", h.quit, false)
	}
	h.addControlHints(&bindings)
	if !h.modal && h.tabsOverflow && h.sessionTabs.HasFocus() {
		add("session-menu", "↓", h.tr("更多会话", "More sessions"), tcell.KeyDown, "", h.showSessionMenu, true)
	}
	if h.modal {
		return bindings
	}
	if h.activeSession < 0 {
		add("sidebar", "Ctrl+B", h.tr("显示/隐藏树区域", "Show/hide navigation"), tcell.KeyCtrlB, "", h.toggleSidebar, false)
		if h.search.HasFocus() {
			add("search-results", "↓", h.tr("资产", "Assets"), tcell.KeyDown, "", func() { h.app.SetFocus(h.table) }, true)
		} else {
			add("search", "Ctrl+F", h.tr("搜索", "Search"), tcell.KeyCtrlF, "", func() { h.closeDropdown(); h.app.SetFocus(h.search) }, false)
		}
		if h.search.HasFocus() && h.search.GetText() != "" {
			add("clear-search", "Ctrl+U", h.tr("清空搜索", "Clear search"), tcell.KeyCtrlU, "", func() { h.search.SetText("") }, true)
		}
	}
	if h.editing() {
		return bindings
	}
	areas()
	if h.activeSession >= 0 {
		return bindings
	}
	if h.focusedDropdown() == nil {
		for i, labels := range [][2]string{{"授权树", "Authorization tree"}, {"类型树", "Type tree"}, {"收藏树", "Favorites tree"}} {
			label := fmt.Sprint(i + 1)
			add("tree-type", label, h.tr(labels[0], labels[1]), 0, label, func() { h.switchTree(i) }, false)
		}
	}
	add("search-slash", "/", h.tr("搜索", "Search"), 0, "/", func() { h.app.SetFocus(h.search) }, false)
	if !h.sidebarHidden && h.tree.GetRowCount() > 0 {
		add("tree-toggle", "z", h.tr("折叠/展开", "Collapse/expand"), 0, "zZ", h.toggleTreeExpansion, false)
		add("collapse", "- / +", h.tr("折叠/展开", "Collapse/expand"), 0, "-", h.collapseTree, false)
		add("expand", "", "", 0, "+", h.expandFirstTreeLevel, false)
	}
	if !h.sidebarHidden {
		add("tree-refresh", "u", h.tr("刷新", "Refresh"), 0, "uU", h.refreshView, false)
	}
	add("narrow-tree", "<", h.tr("调整树宽度，也可拖动分隔线", "Resize tree, or drag the divider"), 0, "<", func() { h.resizeSidebar(-4) }, false)
	add("widen-tree", ">", h.tr("调整树宽度，也可拖动分隔线", "Resize tree, or drag the divider"), 0, ">", func() { h.resizeSidebar(4) }, false)
	if h.offset > 0 {
		add("previous-page", "[", h.tr("上页", "Previous"), 0, "[", func() { h.changePage(-1) }, false)
	}
	if h.offset+tuiPageSize < h.total {
		add("next-page", "]", h.tr("下页", "Next"), 0, "]", func() { h.changePage(1) }, false)
	}
	refresh := h.refreshAssets
	if !h.organizationsReady {
		refresh = h.refreshView
	}
	add("refresh", "r", h.tr("刷新", "Refresh"), 0, "rR", refresh, false)
	add("quit", "q", h.tr("退出", "Quit"), 0, "qQ", h.quit, false)
	return bindings
}

func (h *terminalUI) addControlHints(bindings *[]tuiShortcut) {
	add := func(keys, description string, compact bool) {
		*bindings = append(*bindings, tuiShortcut{id: "control", label: keys, description: description, compact: compact})
	}
	scroll, confirm := false, false
	switch p := h.focusedControl().(type) {
	case *tview.Table:
		if p == h.sessionTabs {
			add("←→", h.tr("选择", "Select"), true)
			confirm = p.GetColumnCount() > 0
		} else {
			scroll = p.GetRowCount() > 2
			row, col := p.GetSelection()
			confirm = row > 0 && row < p.GetRowCount() && !p.GetCell(row, col).NotSelectable
			if p == h.table {
				confirm = confirm && row <= len(h.assets) && len(h.sessions) < maxTUISessions
				add("←", h.tr("返回当前节点", "Return to current node"), false)
				add("Shift+← / Shift+→", h.tr("水平滚动", "Horizontal scroll"), false)
			} else if scroll {
				add("←→", h.tr("移动和滚动", "Move and scroll"), false)
			}
		}
	case *tview.TreeView:
		scroll, confirm = p.GetRowCount() > 1, p.GetCurrentNode() != nil
		if scroll {
			add("←→", h.tr("折叠/展开；当前范围 → 进入资产", "Collapse/expand; current scope → opens assets"), false)
		}
	case *tview.List:
		scroll, confirm = p.GetItemCount() > 1, p.GetItemCount() > 0
		if h.modal && h.dialogs[len(h.dialogs)-1].page == "language" {
			add("1–9", h.tr("选择", "Select"), true)
		}
	case *tview.DropDown:
		scroll, confirm = p.GetOptionCount() > 1, p.GetOptionCount() > 0
	case *tview.InputField:
		confirm = true
	case *tview.Button:
		confirm = (p != h.pagerButtons[0] || h.offset > 0) && (p != h.pagerButtons[1] || h.offset+tuiPageSize < h.total)
	case *tview.TextView:
		_, _, _, height := p.GetInnerRect()
		scroll, confirm = height > 0 && p.GetWrappedLineCount() > height, true
	default:
		if h.modal && len(h.focusOrder()) == 0 {
			confirm = true
			add("←→", h.tr("选择", "Select"), true)
		}
	}
	if scroll {
		add("↑↓ / PgUp / PgDn", h.tr("移动和滚动", "Move and scroll"), true)
		add("Home / End", h.tr("首项/末项", "First/last item"), false)
	}
	if confirm {
		description := h.tr("确认", "Confirm")
		switch p := h.focusedControl().(type) {
		case *tview.InputField:
			description = h.tr("搜索", "Search")
		case *tview.TextView:
			description = h.tr("关闭", "Close")
		case *tview.Button:
			description = strings.Split(p.GetLabel(), " · ")[0]
		case *tview.Table:
			if p == h.table {
				description = h.tr("连接", "Connect")
			}
		}
		add("Enter", description, true)
	}
}

func (h *terminalUI) refreshShortcutLabels(bindings []tuiShortcut) {
	// Labels keep their text and width; only availability changes their style.
	mnemonic := func(label, id, key string) string {
		return tuiMnemonicState(label, key, tuiShortcutAvailable(bindings, id, key))
	}
	if h.statusFailed {
		h.status.SetText(" " + h.tr("加载失败", "Load failed") + " · " + mnemonic("r", "refresh", "r") + " " + h.tr("刷新", "Refresh"))
	}
	h.orgPane.SetTitle("")
	h.org.SetLabel(mnemonic("o", "organization-area", "o") + " ")
	h.sessionTabs.SetTitle("")
	h.treePane.SetTitle("")
	h.treeKind.SetLabel(mnemonic("t", "tree-area", "t") + " ")
	h.tree.SetTitle(mnemonic("e", "tree-nodes", "e"))
	label := "+"
	if expanded, _ := h.treeExpansion(); expanded {
		label = "−"
	}
	h.treeActions[0].SetLabel(mnemonic("  "+label, "tree-toggle", "z"))
	h.treeActions[1].SetLabel(mnemonic("  ↻", "tree-refresh", "u"))
	path := h.scope.Path
	if path == "" {
		path = h.scope.Label
	}
	if path == "" {
		path = h.tr("资产", "Assets")
	}
	assetTitle := mnemonic("a", "assets-area", "a") + " · /" + cleanTUIText(strings.TrimLeft(path, "/"))
	h.assetPane.SetTitle(" " + assetTitle + " ")
	searchLabel := ""
	if !h.search.HasFocus() {
		searchLabel = mnemonic("/", "search-slash", "/") + " " + h.tr("搜索", "Search") + " "
	}
	h.search.SetLabel(searchLabel)
	refresh := mnemonic("↻", "refresh", "r")
	h.assetRefresh.SetLabel(refresh)
	previous, next := h.tr("上页", "Previous"), h.tr("下页", "Next")
	previous = mnemonic("[", "previous-page", "[") + " " + previous
	next += " " + mnemonic("]", "next-page", "]")
	h.pagerButtons[0].SetLabel(previous)
	h.pagerButtons[1].SetLabel(next)
	if h.assetBottom != nil && h.total > tuiPageSize {
		h.assetBottom.ResizeItem(h.pager, h.pagerWidth(), 0)
	}
	language := mnemonic("◎ "+strings.ToUpper(i18n.NewLang(h.data.lang).String()), "language", "l") + " ▾"
	h.language.SetLabel(language)
	appearance := mnemonic("◇ "+h.tr("主题", "Theme"), "appearance", "c") + " ▾"
	h.appearance.SetLabel(appearance)
	_, selectedTab := h.sessionTabs.GetSelection()
	showNumbers := !h.modal && (h.windowPrefix || time.Now().Before(h.tabHintUntil))
	for col := 0; col < h.sessionTabs.GetColumnCount(); col++ {
		label := strings.TrimSpace(h.sessionTabLabel(col - 1))
		padding := ""
		if col > 0 {
			padding = " "
		}
		if col == h.activeSession+1 {
			icon, rest, _ := strings.Cut(label, " ")
			label = "[" + tui.Accent.String() + "::-]" + icon + "[-::-] " + rest
		}
		if showNumbers {
			label = padding + "[" + tui.Accent.String() + "::u]" + fmt.Sprint(col) + "[::U][-::-] " + label + " "
		} else {
			label = padding + label + "   "
		}
		style := tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)
		// Keyboard focus underlines the text; the active page colors only its icon.
		style = style.Underline(h.sessionTabs.HasFocus() && col == selectedTab)
		h.sessionTabs.GetCell(0, col).SetText(label).SetStyle(style).SetSelectedStyle(style)
	}
	for _, session := range h.sessions {
		if len(session.controls) == 0 {
			continue
		}
		session.controls[0].(*tview.Button).SetLabel(mnemonic(h.tr("复制", "Duplicate"), "duplicate-session", "d")).SetDisabled(session.duplicate == nil || len(h.sessions) >= maxTUISessions)
		closeLabel := h.tr("断开", "Disconnect")
		if session.done {
			closeLabel = h.tr("关闭", "Close")
		}
		session.controls[1].(*tview.Button).SetLabel(mnemonic(closeLabel, "close-session", "x"))
		fullscreenLabel := h.tr("全屏", "Fullscreen")
		if h.fullscreen && session.terminal == h.popup {
			fullscreenLabel = h.tr("退出全屏", "Exit fullscreen")
		}
		session.controls[2].(*tview.Button).SetLabel(mnemonic(fullscreenLabel, "fullscreen", "f"))
	}
}
