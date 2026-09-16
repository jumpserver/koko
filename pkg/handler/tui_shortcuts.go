package handler

import (
	"fmt"
	"strings"
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
		prefix = "[" + tui.Disabled.String() + "::-][::U]"
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
	return strings.NewReplacer("["+tui.Accent.String()+"::bu]", "", "["+tui.Accent.String()+"::b]", "", "["+tui.Accent.String()+"::u]", "", "["+tui.Accent.String()+"::-]", "", "["+tui.Disabled.String()+"::-][::U]", "", "[::U][-::-]", "", "[-::-]", "").Replace(label)
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
}

func (s tuiShortcut) matches(ev *tcell.EventKey) bool {
	if s.run == nil || ev.Modifiers()&(tcell.ModAlt|tcell.ModMeta) != 0 {
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
		if d, ok := p.(*tui.DropDown); ok && d.IsOpen() {
			d.Focus(func(list tview.Primitive) { p = list })
		}
		return p
	}
	return nil
}

func (h *terminalUI) hintsVisible() bool {
	return !h.modal && (h.windowPrefix || h.shortcutHints && !(h.popup != nil && h.popup.HasFocus()))
}

func (h *terminalUI) toggleShortcutHints() {
	if h.popup != nil && h.popup.HasFocus() && !h.modal {
		h.windowPrefix = !h.windowPrefix
		return
	}
	h.shortcutHints = !h.shortcutHints
}

func (h *terminalUI) shortcuts() []tuiShortcut {
	var bindings []tuiShortcut
	add := func(id, label, description string, key tcell.Key, runes string, run func(), compact bool) {
		bindings = append(bindings, tuiShortcut{id: id, label: label, description: description, key: key, runes: runes, run: run, compact: compact})
	}
	remote := !h.modal && h.popup != nil && h.popup.HasFocus()
	if !remote || h.windowPrefix {
		add("help", "F10", h.tr("帮助", "Help"), tcell.KeyF10, "", h.showKeyboardHelp, false)
	}
	if !h.modal && (!remote || h.windowPrefix) {
		// Keep the entry clickable while typing; a literal ? still reaches the field.
		runes := "?"
		if h.editing() && !h.windowPrefix {
			runes = ""
		}
		add("hints", "?", h.tr("快捷键", "Shortcuts"), 0, runes, h.toggleShortcutHints, true)
	}
	fullscreenLabel := h.tr("全屏", "Fullscreen")
	if h.fullscreen {
		fullscreenLabel = h.tr("退出全屏", "Exit fullscreen")
	}
	sessions := func() {
		for i := range h.sessions {
			label := fmt.Sprint(i + 1)
			add("session", label, cleanTUIText(h.sessions[i].name), 0, label, func() { h.activateSession(i) }, false)
		}
	}
	areas := func() {
		if h.organizationsEnabled {
			add("organization-area", "o", h.tr("聚焦组织选择框", "Focus the organization selector"), 0, "o", func() { h.focusArea(h.org) }, false)
		}
		add("tree-area", "t", h.tr("聚焦资产树类型选择框", "Focus the asset tree type selector"), 0, "t", func() { h.focusArea(h.treeKind) }, false)
		add("tree-nodes", "e", h.tr("聚焦资产树节点", "Focus asset tree nodes"), 0, "e", func() { h.focusArea(h.tree) }, false)
		add("assets-area", "a", h.tr("聚焦资产列表", "Focus the asset list"), 0, "a", func() { h.focusArea(h.table) }, false)
		if h.popup != nil {
			add("fullscreen", "f", fullscreenLabel, 0, "f", func() { h.closeDropdown(); h.setFullscreen(!h.fullscreen) }, true)
			session := h.sessions[h.activeSession]
			if session.duplicate != nil && len(h.sessions) < maxTUISessions {
				add("duplicate-session", "d", h.tr("复制会话", "Duplicate session"), 0, "d", session.duplicate, false)
			}
			add("close-session", "x", h.tr("关闭", "Close"), 0, "x", func() { h.closeSession(session) }, false)
		}
	}
	if !h.modal {
		if h.windowPrefix {
			areas()
			add("asset-index", "0", h.tr("资产面板", "Asset panel"), 0, "0", func() { h.activateSession(-1) }, false)
			sessions()
			if len(h.sessions) > 0 {
				add("session-number", fmt.Sprintf("0–%d", len(h.sessions)), h.tr("切换窗口", "Switch window"), 0, "", nil, true)
			}
			if h.popup != nil {
				add("literal-prefix", "Ctrl+]", h.tr("发送原始 Ctrl+]", "Send literal Ctrl+]"), tcell.KeyCtrlRightSq, "", func() { h.popup.SendInput([]byte{29}) }, false)
			}
			add("help", "h", h.tr("帮助", "Help"), 0, "h", h.showKeyboardHelp, true)
			add("quit", "Ctrl+C", h.tr("退出", "Quit"), tcell.KeyCtrlC, "", h.quit, false)
			add("back", "Esc", h.tr("返回", "Back"), tcell.KeyEscape, "", func() {}, true)
			return bindings
		}
		add("window", "Ctrl+]", h.tr("窗口栏", "Window bar"), tcell.KeyCtrlRightSq, "", func() { h.closeDropdown(); h.windowPrefix = true }, len(h.sessions) > 0)
		if h.editing() || h.popup != nil && h.popup.HasFocus() {
			add("help", "Ctrl+] h", h.tr("帮助", "Help"), 0, "", nil, true)
		}
		if h.popup != nil && h.popup.HasFocus() {
			// These are two-key chords. Bare letters and all function keys go
			// unchanged to SSH; the prefix enables the matching actions above.
			add("asset-index", "Ctrl+] 0", h.tr("资产面板", "Asset panel"), 0, "", nil, false)
			add("fullscreen", "Ctrl+] f", fullscreenLabel, 0, "", nil, true)
			if h.sessions[h.activeSession].duplicate != nil && len(h.sessions) < maxTUISessions {
				add("duplicate-session", "Ctrl+] d", h.tr("复制会话", "Duplicate session"), 0, "", nil, false)
			}
			add("close-session", "Ctrl+] x", h.tr("关闭", "Close"), 0, "", nil, false)
			return bindings
		}
	}

	if dropdown := h.focusedDropdown(); dropdown != nil && dropdown.GetOptionCount() > 0 && (dropdown.IsOpen() || h.modal) && !(dropdown.IsOpen() && h.accountSearchFor(dropdown) != nil) {
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
		add("help", "h", h.tr("帮助", "Help"), 0, "h", h.showKeyboardHelp, true)
		if !helpOpen {
			if h.focusedContent() != "" {
				add("full-text", "v", h.tr("完整内容", "Full text"), 0, "v", h.showFullText, true)
			}
		}
	}
	focusCount := len(h.tabFocusOrder())
	if focusCount > 1 || h.modal && focusCount == 0 {
		if focusCount > 1 {
			focusLabel, previousLabel := h.tr("切换区域", "Switch region"), h.tr("上一区域", "Previous region")
			if h.modal {
				focusLabel, previousLabel = h.tr("切换控件", "Focus"), h.tr("上一个控件", "Previous control")
			}
			add("focus", "Tab", focusLabel, tcell.KeyTab, "", func() { h.cycleFocus(false) }, true)
			add("previous-focus", "Shift+Tab", previousLabel, tcell.KeyBacktab, "", func() { h.cycleFocus(true) }, false)
		} else {
			add("focus", "Tab", h.tr("切换控件", "Focus"), 0, "", nil, true)
		}
	}
	add("back", "Esc", h.tr("返回", "Back"), tcell.KeyEscape, "", h.escape, true)
	if !h.modal {
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
		add("sidebar", "Ctrl+B", h.tr("显示或隐藏资产树区域", "Show or hide the asset tree pane"), tcell.KeyCtrlB, "", h.toggleSidebar, false)
		if h.search.HasFocus() {
			add("search-results", "↓", h.tr("资产", "Assets"), tcell.KeyDown, "", func() { h.app.SetFocus(h.table) }, true)
		}
		if h.search.HasFocus() && h.search.GetText() != "" {
			add("clear-search", "Ctrl+U", h.tr("清空搜索", "Clear search"), tcell.KeyCtrlU, "", h.clearSearch, true)
		}
	}
	if h.editing() {
		return bindings
	}
	areas()
	if h.activeSession >= 0 {
		return bindings
	}
	sessions()
	add("search-slash", "/", h.tr("聚焦资产搜索框", "Focus the asset search field"), 0, "/", func() { h.app.SetFocus(h.search) }, false)
	if !h.sidebarHidden && h.tree.GetRowCount() > 0 {
		add("tree-toggle", "z", h.tr("逐级收起资产树；完全收起时展开一级节点", "Collapse the asset tree in stages; expand top-level nodes when fully collapsed"), 0, "z", h.toggleTreeExpansion, false)
	}
	if !h.sidebarHidden {
		treeRefreshDescription := h.tr("重载当前资产树和顶层资产列表，并重置选中节点", "Reload the current asset tree and top-level asset list, resetting the selected node")
		if !h.organizationsReady || h.scope.Org.ID == "" {
			treeRefreshDescription = h.tr("重新加载组织、资产树和资产列表", "Reload organizations, asset tree, and asset list")
		}
		add("tree-refresh", "u", treeRefreshDescription, 0, "u", h.refreshView, false)
	}
	add("narrow-tree", "<", h.tr("缩窄资产树区域（每次 4 列）", "Narrow the asset tree pane by 4 columns"), 0, "<", func() { h.resizeSidebar(-4) }, false)
	add("widen-tree", ">", h.tr("加宽资产树区域（每次 4 列）", "Widen the asset tree pane by 4 columns"), 0, ">", func() { h.resizeSidebar(4) }, false)
	if h.offset > 0 {
		add("previous-page", "[", h.tr("资产列表上一页", "Previous page of the asset list"), 0, "[", func() { h.changePage(-1) }, false)
	}
	if h.offset+tuiPageSize < h.total {
		add("next-page", "]", h.tr("资产列表下一页", "Next page of the asset list"), 0, "]", func() { h.changePage(1) }, false)
	}
	refresh := h.refreshAssets
	refreshDescription := h.tr("刷新当前节点的资产列表，保留搜索条件和页码", "Refresh the current node's asset list, keeping search and page")
	if !h.organizationsReady || h.scope.Org.ID == "" {
		refresh = h.refreshView
		refreshDescription = h.tr("重新加载组织、资产树和资产列表", "Reload organizations, asset tree, and asset list")
	}
	add("refresh", "r", refreshDescription, 0, "r", refresh, false)
	return bindings
}

func (h *terminalUI) addControlHints(bindings *[]tuiShortcut) {
	add := func(keys, description string, compact bool) {
		*bindings = append(*bindings, tuiShortcut{id: "control", label: keys, description: description, compact: compact})
	}
	scroll, confirm := false, false
	treeControls, assetControls := false, false
	switch p := h.focusedControl().(type) {
	case *tui.Table:
		if p == h.sessionTabs {
			add("←→", h.tr("选择", "Select"), true)
			confirm = p.GetColumnCount() > 0
		} else {
			scroll = p.GetRowCount() > tuiAssetTableHeaderRows+1
			row, col := p.GetSelection()
			confirm = row > 0 && row < p.GetRowCount() && !p.GetCell(row, col).NotSelectable
			if p == h.table {
				assetControls = true
				assetIndex := row - tuiAssetTableHeaderRows
				confirm = confirm && assetIndex >= 0 && assetIndex < len(h.assets) && h.assetCanConnect(assetIndex) && len(h.sessions) < maxTUISessions
				add("← / →", h.tr("横向滚动资产列表", "Scroll the asset list horizontally"), false)
			} else if scroll {
				add("←→", h.tr("移动和滚动", "Move and scroll"), false)
			}
		}
	case *tui.TreeView:
		treeControls = true
		scroll, confirm = p.GetRowCount() > 1, p.GetCurrentNode() != nil
		if confirm {
			add("Space", h.tr("折叠或展开当前树节点", "Collapse or expand the current tree node"), false)
		}
		if scroll {
			add("← / →", h.tr("收起当前节点或移至父节点 / 展开当前节点", "Collapse the current node or move to its parent / expand the current node"), false)
			add("J / K", h.tr("移动到子节点 / 父节点", "Move to a child / parent node"), false)
		}
	case *tui.List:
		scroll, confirm = p.GetItemCount() > 1, p.GetItemCount() > 0
		if h.modal && h.dialogs[len(h.dialogs)-1].page == "language" {
			add("1–9", h.tr("选择", "Select"), true)
		}
	case *tui.DropDown:
		if h.modal && !h.editing() {
			if button := h.dialogs[len(h.dialogs)-1].defaultButton; button != nil {
				add("↓", h.tr("展开选择", "Open choices"), true)
				if !button.IsDisabled() {
					*bindings = append(*bindings, tuiShortcut{id: "control", label: "Enter", description: strings.Split(button.GetLabel(), " · ")[0], key: tcell.KeyEnter,
						run: func() {
							button.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { h.app.SetFocus(p) })
						}, compact: true})
				}
				return
			}
		}
		scroll, confirm = p.GetOptionCount() > 1, p.GetOptionCount() > 0
	case *tui.InputField:
		confirm = true
	case *tui.Button:
		confirm = !p.IsDisabled() && (p != h.pagerButtons[0] || h.offset > 0) && (p != h.pagerButtons[1] || h.offset+tuiPageSize < h.total)
	case *tui.TextView:
		_, _, _, height := p.GetInnerRect()
		scroll, confirm = height > 0 && p.GetWrappedLineCount() > height, true
	default:
		if h.modal && len(h.focusOrder()) == 0 {
			confirm = true
			add("←→", h.tr("选择", "Select"), true)
		}
	}
	if scroll {
		switch {
		case treeControls:
			add("↑↓ / j / k", h.tr("在可见树节点间移动", "Move through visible tree nodes"), true)
			add("PgUp / PgDn", h.tr("在资产树中向上或向下移动一屏", "Move one screen up or down in the asset tree"), false)
			add("Home / End / g / G", h.tr("定位到资产树首个或末个节点", "Jump to the first or last asset tree node"), false)
		case assetControls:
			add("↑↓ / j / k", h.tr("在资产列表的行间移动", "Move between asset list rows"), true)
			add("PgUp / PgDn", h.tr("在当前页内向上或向下移动一屏", "Move one screen up or down within the current asset page"), false)
			add("Home / End / g / G", h.tr("定位到资产列表首行或末行", "Jump to the first or last asset list row"), false)
		default:
			add("↑↓ / PgUp / PgDn", h.tr("移动和滚动", "Move and scroll"), true)
			add("Home / End", h.tr("首项/末项", "First/last item"), false)
		}
	}
	if confirm {
		description := h.tr("确认", "Confirm")
		switch p := h.focusedControl().(type) {
		case *tui.TreeView:
			description = h.tr("显示当前节点的资产", "Show assets in the current tree node")
		case *tui.InputField:
			if p == h.search {
				description = h.tr("立即搜索资产", "Search assets immediately")
			}
		case *tui.TextView:
			description = h.tr("关闭", "Close")
		case *tui.Button:
			description = strings.Split(p.GetLabel(), " · ")[0]
		case *tui.Table:
			if p == h.table {
				description = h.tr("连接选中的资产", "Connect to the selected asset")
			}
		}
		add("Enter", description, true)
	}
}

func (h *terminalUI) refreshShortcutLabels(bindings []tuiShortcut) {
	// Hidden hints do not reserve label space.
	mnemonic := func(label, id, key string) string {
		text := tuiMnemonicState(label, key, tuiShortcutAvailable(bindings, id, key))
		if !h.hintsVisible() {
			if label == key {
				return ""
			}
			return label
		}
		return text
	}
	if h.statusFailed {
		h.status.SetText(" " + h.tr("加载失败", "Load failed") + " · " + mnemonic("r", "refresh", "r") + " " + h.tr("刷新", "Refresh"))
	}
	h.orgPane.SetTitle("")
	h.org.SetLabel(mnemonic("o", "organization-area", "o"))
	h.sessionTabs.SetTitle("")
	h.treePane.SetTitle("")
	h.treeKind.SetLabel(mnemonic("t", "tree-area", "t"))
	h.tree.SetTitle(mnemonic("e", "tree-nodes", "e"))
	if h.hintsVisible() {
		h.org.SetLabel(h.org.GetLabel() + " ")
		h.treeKind.SetLabel(h.treeKind.GetLabel() + " ")
		h.tree.SetBorderPadding(0, 0, 0, 2)
	} else {
		h.tree.SetBorderPadding(0, 0, 0, 0)
	}
	h.treeRefresh.SetLabel(strings.TrimSpace(tuiRefreshIcon + " " + mnemonic("u", "tree-refresh", "u")))
	path := h.scope.Path
	if path == "" {
		path = h.scope.Label
	}
	if path == "" {
		path = h.tr("资产", "Assets")
	}
	separator := "/"
	if h.hintsVisible() {
		separator = " · /"
	}
	assetTitle := mnemonic("a", "assets-area", "a") + separator + cleanTUIText(strings.TrimLeft(path, "/"))
	h.assetPane.SetTitle(" " + assetTitle + " ")
	searchLabel := ""
	if !h.search.HasFocus() {
		searchLabel = "/ " + h.tr("搜索", "Search") + " "
	}
	h.search.SetLabel(searchLabel)
	h.assetRefresh.SetLabel(strings.TrimSpace(tuiRefreshIcon + " " + mnemonic("r", "refresh", "r")))
	h.assetToolbar.ResizeItem(h.assetRefresh, tview.TaggedStringWidth(h.assetRefresh.GetLabel())+5, 0)
	refreshWidth := tview.TaggedStringWidth(h.treeRefresh.GetLabel()) + 2
	h.treeTools.ResizeItem(h.treeRefresh, refreshWidth, 0)
	h.treeHead.ResizeItem(h.treeTools, refreshWidth, 0)
	previous, next := h.tr("上页", "Previous"), h.tr("下页", "Next")
	previous = mnemonic("[", "previous-page", "[") + " " + previous
	next += " " + mnemonic("]", "next-page", "]")
	h.pagerButtons[0].SetLabel(strings.TrimSpace(previous))
	h.pagerButtons[1].SetLabel(strings.TrimSpace(next))
	if h.assetBottom != nil && h.total > tuiPageSize {
		h.assetBottom.ResizeItem(h.pager, h.pagerWidth(), 0)
	}
	currentLanguage := i18n.NewLang(h.data.lang)
	language := currentLanguage.String()
	for index, code := range i18n.AllCodes {
		if code == currentLanguage {
			language = i18n.AllLangCodesStr[index]
			break
		}
	}
	language += " ▾"
	h.language.SetLabel(language)
	appearance := h.tr("主题", "Theme") + " ▾"
	h.appearance.SetLabel(appearance)
	_, selectedTab := h.sessionTabs.GetSelection()
	showNumbers := !h.modal && h.windowPrefix
	for col := 0; col < h.sessionTabs.GetColumnCount(); col++ {
		label := strings.TrimSpace(h.sessionTabLabel(col - 1))
		padding := ""
		if col > 0 {
			padding = " "
		}
		if col == h.activeSession+1 || h.sessionTabs.HasFocus() && col == selectedTab {
			color := tui.Foreground
			if col == h.activeSession+1 {
				color = tui.Accent
			}
			label = "[" + color.String() + "::-]" + label + "[-::-]"
		}
		if showNumbers || h.hintsVisible() && h.activeSession < 0 && col > 0 {
			id, number := "session", fmt.Sprint(col)
			if col == 0 {
				id = "asset-index"
			}
			key := strings.ReplaceAll(tuiKeyText(number, tuiShortcutAvailable(bindings, id, number)), "::bu]", "::u]")
			label = padding + key + " " + label + " "
		} else {
			label = padding + label + " "
		}
		style := tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)
		h.sessionTabs.GetCell(0, col).SetText(label).SetStyle(style).SetSelectedStyle(style)
	}
	compactMnemonic := func(label, id, key string) string {
		text := tuiMnemonicState(label, key, tuiShortcutAvailable(bindings, id, key))
		keyText := tuiKeyText(key, tuiShortcutAvailable(bindings, id, key))
		if strings.HasSuffix(text, " "+keyText) {
			text = strings.TrimSuffix(text, " "+keyText) + keyText
		}
		if !h.hintsVisible() {
			return label
		}
		return strings.ReplaceAll(text, "::bu]", "::u]")
	}
	for _, session := range h.sessions {
		if len(session.controls) == 0 {
			continue
		}
		session.controls[0].(*tui.Button).SetLabel(compactMnemonic(h.tr("复制", "Duplicate"), "duplicate-session", "d")).SetDisabled(session.duplicate == nil || len(h.sessions) >= maxTUISessions)
		closeLabel := h.tr("断开", "Disconnect")
		if session.done {
			closeLabel = h.tr("关闭", "Close")
		}
		session.controls[1].(*tui.Button).SetLabel(compactMnemonic(closeLabel, "close-session", "x"))
		fullscreenLabel := h.tr("全屏", "Fullscreen")
		if h.fullscreen && session.terminal == h.popup {
			fullscreenLabel = h.tr("退出全屏", "Exit fullscreen")
		}
		session.controls[2].(*tui.Button).SetLabel(compactMnemonic(fullscreenLabel, "fullscreen", "f"))
	}
}
