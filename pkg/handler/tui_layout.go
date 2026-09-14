package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
	"github.com/jumpserver/koko/pkg/i18n"
)

func tuiDropdown() *tview.DropDown {
	d := tview.NewDropDown().SetFieldTextColor(tui.Foreground).SetFieldBackgroundColor(tui.Panel).
		SetLabelStyle(tcell.StyleDefault.Foreground(tui.Accent).Background(tui.Panel)).
		SetListStyles(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel), tui.Selected).
		SetFocusedStyle(tui.Selected).SetUseStyleTags(true).SetTextOptions(" ", " ", " ", " ▾", " … ")
	d.SetBackgroundColor(tui.Panel)
	return d
}

// Keep the native dropdown's keyboard and mouse routing, but give its list a
// framed surface wide enough to read. Draw last so underlying tree rows cannot
// bleed through the popup, including when the selected language has long names.
func (h *terminalUI) drawDropdown(screen tcell.Screen) {
	if h.modal && h.dialogs[len(h.dialogs)-1].boundedDropdowns {
		return // Connection dropdowns render once, within their bounded viewport.
	}
	for _, item := range h.focusOrder() {
		dropdown, ok := item.(*tview.DropDown)
		if !ok || !dropdown.IsOpen() || !dropdown.HasFocus() {
			continue
		}
		// An open DropDown delegates focus to its list. Resolve that delegate
		// without calling Application.GetFocus while Draw holds the app lock.
		var list *tview.List
		dropdown.Focus(func(p tview.Primitive) { list, _ = p.(*tview.List) })
		if list == nil {
			continue
		}
		sw, sh := screen.Size()
		// Enclose the native list's rendered rectangle. Anchoring at the field's
		// outer box leaves a second menu visible beside labels or when it opens up.
		x, y, nativeWidth, nativeHeight := list.GetRect()
		digits := len(strconv.Itoa(list.GetItemCount()))
		width := max(dropdown.GetFieldWidth()+2, nativeWidth)
		for i := range list.GetItemCount() {
			label, _ := list.GetItemText(i)
			width = max(width, tview.TaggedStringWidth(label)+digits+5)
		}
		width = min(width, max(1, sw))
		rows := min(max(list.GetItemCount()+4, nativeHeight), max(1, sh))
		x = max(0, min(x, sw-width))
		y = max(0, min(y, sh-rows))
		tuiBorder(list.Box, "", tui.FocusBorder)
		list.SetBackgroundColor(tui.Panel).SetBorderPadding(1, 1, digits+3, 1)
		list.SetRect(x, y, width, rows)
		offset, horizontal := list.GetOffset()
		list.SetOffset(min(offset, max(0, list.GetItemCount()-max(1, rows-4))), horizontal)
		list.Draw(screen)
		offset, _ = list.GetOffset()
		for i := offset; i < min(list.GetItemCount(), offset+max(0, rows-4)); i++ {
			color := tui.Muted
			if i == list.GetCurrentItem() {
				color = tui.Foreground
			}
			tview.Print(screen, fmt.Sprintf("%*d", digits, i+1), x+2, y+2+i-offset, digits, tview.AlignRight, color)
		}
		return
	}
}

func (h *terminalUI) refreshLabels() {
	if len(h.orgs) > 0 {
		h.setOrganizations(h.orgs)
	}
	h.treeKind.SetOptions([]string{h.tr("授权树", "Authorization tree"), h.tr("类型树", "Type tree"), h.tr("收藏树", "Favorites tree")}, func(_ string, i int) {
		if i >= 0 && i != h.scope.Mode {
			h.switchTree(i)
		}
	}).SetCurrentOption(h.scope.Mode)
	h.search.SetPlaceholder(h.tr("名称、地址、备注", "Name, address, comment"))
	h.refreshShortcutLabels(h.shortcuts())
}

func (h *terminalUI) rebuildNavigation() {
	h.navigation.Clear()
	if h.organizationsEnabled {
		h.navigation.AddItem(h.orgPane, 2, 0, false)
	}
	h.navigation.AddItem(h.treePane, 0, 1, true)
	h.tabRow.Clear().AddItem(h.sessionTabs, 0, 1, false)
}

// Draw the shared frame after its children, before any overlaid dialog. Child
// backgrounds and focus rendering cannot overwrite individual border segments.
type tuiNavigation struct {
	*tview.Flex
	drawFrame func(tcell.Screen)
}

func (n *tuiNavigation) Draw(screen tcell.Screen) {
	n.Flex.Draw(screen)
	n.drawFrame(screen)
}

func (h *terminalUI) drawNavigationFrame(screen tcell.Screen) {
	x, y, width, height := h.navigation.GetRect()
	if width < 2 || height < 2 {
		return
	}
	right, bottom := x+width-1, y+height-1
	_, headY, _, headHeight := h.treeHead.GetRect()
	treeDivider := min(bottom, headY+headHeight-1)
	orgDivider := y
	if h.organizationsEnabled {
		_, row, _, rows := h.orgPane.GetRect()
		orgDivider = min(bottom, row+rows-1)
	}
	focusTop, focusBottom := -1, -1
	if !h.modal {
		switch {
		case h.organizationsEnabled && h.org.HasFocus():
			focusTop, focusBottom = y, orgDivider
		case h.treeHead.HasFocus():
			focusTop, focusBottom = orgDivider, treeDivider
		case h.tree.HasFocus():
			focusTop, focusBottom = treeDivider, bottom
		}
	}
	style := func(row int) tcell.Style {
		color := tui.Border
		if row >= focusTop && row <= focusBottom {
			color = tui.FocusBorder
		}
		return tcell.StyleDefault.Foreground(color).Background(tui.Panel)
	}
	for row := y + 1; row < bottom; row++ {
		screen.SetContent(x, row, '│', nil, style(row))
		screen.SetContent(right, row, '│', nil, style(row))
	}
	horizontal := func(row int, leftCorner, rightCorner rune) {
		for col := x + 1; col < right; col++ {
			screen.SetContent(col, row, '─', nil, style(row))
		}
		screen.SetContent(x, row, leftCorner, nil, style(row))
		screen.SetContent(right, row, rightCorner, nil, style(row))
	}
	horizontal(y, '╭', '╮')
	for _, divider := range []int{orgDivider, treeDivider} {
		if divider > y && divider < bottom {
			horizontal(divider, '├', '┤')
		}
	}
	horizontal(bottom, '╰', '╯')
}

func (h *terminalUI) pagerWidth() int {
	return tview.TaggedStringWidth(h.pagerButtons[0].GetLabel()) + tview.TaggedStringWidth(h.pagerButtons[1].GetLabel()) + 4
}

func (h *terminalUI) navigationWidth() int {
	w, _ := h.screen.Size()
	return min(max(24, h.sidebarWidth), max(24, min(72, w-43)))
}

func (h *terminalUI) updateLayout(now time.Time) {
	h.refreshShortcutLabels(h.shortcuts())
	h.scrollSessionTabs()
	w, height := h.screen.Size()
	listBottomGap := 1
	if height < 24 {
		listBottomGap = 0
	}
	h.assetPane.ResizeItem(nil, listBottomGap, 0)
	headerHeight := 3
	text := now.Format(tuiClockLayout)
	if h.clock.GetText(false) != text {
		h.clock.SetText(text)
	}
	languageWidth := tview.TaggedStringWidth(h.language.GetLabel()) + 2
	themeWidth := tview.TaggedStringWidth(h.appearance.GetLabel()) + 2
	userWidth := min(32, tview.TaggedStringWidth(h.identity.GetText(false)))
	sideWidth := themeWidth + languageWidth + 6 + userWidth
	h.headerTools.ResizeItem(h.appearance, themeWidth, 0).ResizeItem(h.language, languageWidth, 0).ResizeItem(h.identity, userWidth, 0)
	h.header.Clear()
	if w-6 >= sideWidth+tview.TaggedStringWidth(h.brand.GetText(false))+2 {
		h.header.SetDirection(tview.FlexColumn).AddItem(h.brand, 0, 1, false).AddItem(h.headerTools, sideWidth, 0, false)
		h.main.ResizeItem(h.header, headerHeight, 0)
	} else {
		h.header.SetDirection(tview.FlexRow).AddItem(h.brand, 1, 0, false).AddItem(h.headerTools, 1, 0, false)
		h.main.ResizeItem(h.header, headerHeight+1, 0)
	}
	h.body.ResizeItem(h.navigation, h.navigationWidth(), 0)
	if h.lastNavigationWidth != h.navigationWidth() {
		h.lastNavigationWidth = h.navigationWidth()
		if root := h.tree.GetRoot(); root != nil {
			root.Walk(func(n, _ *tview.TreeNode) bool { h.refreshNodeLabel(n); return true })
		}
	}
	h.org.SetFieldWidth(max(1, h.navigationWidth()-4-tview.TaggedStringWidth(h.org.GetLabel())))
	labelWidth := tview.TaggedStringWidth(h.treeKind.GetLabel())
	_, treeLabel := h.treeKind.GetCurrentOption()
	fieldWidth := min(tview.TaggedStringWidth(treeLabel)+2, max(1, h.navigationWidth()-14-labelWidth))
	h.treeKind.SetLabelWidth(labelWidth)
	h.treeKind.SetFieldWidth(fieldWidth)
	h.treeHead.ResizeItem(h.treeKind, labelWidth+fieldWidth, 0)
}

func (h *terminalUI) resizeSidebar(delta int) {
	if h.sidebarHidden {
		h.toggleSidebar()
	}
	h.sidebarWidth = h.navigationWidth() + delta
	h.sidebarWidth = h.navigationWidth()
	h.updateLayout(time.Now())
}

// Size against the visible list, not the longest remark. Narrow terminals keep
// readable minimums and use the table's existing horizontal scroll/full text.
func (h *terminalUI) layoutAssetColumns(width int) {
	if width <= 0 || width == h.lastAssetWidth || h.table.GetRowCount() == 0 {
		return
	}
	h.lastAssetWidth = width
	weights := [...]int{0, 22, 22, 16, 40}
	minimums := [...]int{3, 14, 16, 12, 12}
	columns := h.table.GetColumnCount()
	indexWidth := max(3, len(strconv.Itoa(h.offset+len(h.assets)))+2)
	available := max(1, width-indexWidth-(columns-1))
	weight, last := 0, 0
	for col := 1; col < columns; col++ {
		if h.table.GetCell(0, col).Text != "" {
			weight += weights[col]
			last = col
		}
	}
	used := 0
	for col := 0; col < columns; col++ {
		header := h.table.GetCell(0, col)
		if header.Text == "" {
			continue
		}
		cellWidth := indexWidth
		if col > 0 {
			cellWidth = max(minimums[col], available*weights[col]/max(1, weight))
			if col == last {
				cellWidth = max(minimums[col], available-used)
			}
			used += cellWidth
		}
		text := strings.TrimRight(header.Text, " ")
		header.SetText(text + strings.Repeat(" ", max(0, cellWidth-tview.TaggedStringWidth(text))))
		for row := 0; row < h.table.GetRowCount(); row++ {
			h.table.GetCell(row, col).SetMaxWidth(cellWidth)
		}
	}
}

func (h *terminalUI) toggleSidebar() {
	h.closeDropdown()
	h.sidebarHidden = !h.sidebarHidden
	h.body.Clear()
	if !h.sidebarHidden {
		h.body.AddItem(h.navigation, h.navigationWidth(), 0, true).AddItem(nil, 1, 0, false)
	}
	h.body.AddItem(h.assetRegion, 0, 1, false)
	if h.sidebarHidden {
		h.app.SetFocus(h.table)
	} else {
		h.app.SetFocus(h.tree)
	}
}

// Consume at the root primitive, after tview derives move/down/up/click from
// the original event. Consuming in Application's capture can clear the shared
// event before its remaining actions are delivered.
func (h *terminalUI) captureRootMouse(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	ev, action = h.captureMouse(ev, action)
	return action, ev
}

func (h *terminalUI) captureMouse(ev *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	h.lastInput = time.Now()
	x, y := ev.Position()
	// Connection choices may extend beyond the compact dialog's mouse bounds.
	if h.modal && h.dialogs[len(h.dialogs)-1].boundedDropdowns {
		if dropdown := h.focusedDropdown(); dropdown != nil && dropdown.IsOpen() {
			if consumed, _ := dropdown.MouseHandler()(action, ev, func(p tview.Primitive) { h.app.SetFocus(p) }); consumed {
				return nil, tview.MouseConsumed
			}
			return nil, action
		}
	}
	if !h.modal {
		var controls []tview.Primitive
		if h.activeSession >= 0 {
			controls = h.sessions[h.activeSession].controls
			if h.fullscreen {
				controls = controls[2:]
			}
		}
		if h.tabsOverflow && !h.fullscreen {
			controls = append(append([]tview.Primitive{}, controls...), h.sessionMore)
		}
		for _, control := range controls {
			if control.(*tview.Button).InRect(x, y) {
				control.MouseHandler()(action, ev, func(p tview.Primitive) { h.app.SetFocus(p) })
				return nil, tview.MouseConsumed
			}
		}
	}
	if h.fullscreen {
		return ev, action
	}
	fx, fy, _, _ := h.footer.GetInnerRect()
	if action == tview.MouseLeftClick && !h.modal && h.helpHint != "" && y == fy && x >= fx && x < fx+tview.TaggedStringWidth(h.helpHint) {
		h.showKeyboardHelp()
		return nil, tview.MouseConsumed
	}
	if h.modal || h.activeSession >= 0 || h.sidebarHidden || h.org.IsOpen() || h.treeKind.IsOpen() {
		h.draggingSidebar = false
		return ev, action
	}
	nx, ny, nw, nh := h.navigation.GetRect()
	treeX, treeY, treeWidth, _ := h.tree.GetRect()
	if action == tview.MouseLeftClick && y == treeY && x == treeX+treeWidth-1 {
		h.app.SetFocus(h.tree)
		return nil, tview.MouseConsumed
	}
	if action == tview.MouseLeftDown && x >= nx+nw-1 && x <= nx+nw && y >= ny && y < ny+nh {
		h.draggingSidebar = true
		return nil, action
	}
	if h.draggingSidebar {
		if action == tview.MouseMove {
			h.resizeSidebar(x - nx - h.navigationWidth())
		}
		if action == tview.MouseLeftUp {
			h.draggingSidebar = false
		}
		return nil, action
	}
	return ev, action
}

func (h *terminalUI) refreshView() {
	if !h.organizationsReady || h.scope.Org.ID == "" {
		h.loadWorkspace()
	} else {
		h.switchTree(h.scope.Mode)
	}
}

func (h *terminalUI) expandTree(expand bool) {
	h.treeCollapsed = !expand
	label := "−"
	if !expand {
		label = "+"
	}
	h.treeActions[0].SetLabel(label)
	root := h.tree.GetRoot()
	if root == nil {
		return
	}
	root.Walk(func(n, _ *tview.TreeNode) bool {
		n.SetExpanded(expand && len(n.GetChildren()) > 0)
		h.refreshNodeLabel(n)
		return true
	})
	root.SetExpanded(true)
	h.refreshNodeLabel(root)
	if !expand {
		h.selectFirstTreeNode()
	}
}

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
	close := tview.NewButton(h.tr("关闭", "Close") + " · Esc").SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected).SetSelectedFunc(h.dismissModal)
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(list, 0, 1, true).AddItem(nil, 1, 0, false).AddItem(close, 1, 0, false)
	content.Box = tview.NewBox()
	tuiDialogBorder(content.Box, h.tr("语言 · 当前会话", "Language · this session"))
	h.openDialog("language", &tuiOverlay{Box: tview.NewBox(), child: content, width: 46, height: len(i18n.AllCodes) + 6}, []tview.Primitive{list, close})
}

func (h *terminalUI) changeLanguage(code i18n.LanguageCode) {
	for h.modal {
		h.dismissModal()
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

func (h *terminalUI) focusedContent() string {
	if len(h.dialogs) > 0 && h.dialogs[len(h.dialogs)-1].page == "full-text" {
		return ""
	}
	var fields []string
	field := func(label, value string) {
		label, value = strings.TrimSpace(label), strings.TrimSpace(value)
		if value == "" {
			value = h.tr("空", "Empty")
		}
		fields = append(fields, label+h.tr("：", ": ")+value)
	}
	optionLabel := h.tr("选项", "Option")
	if dropdown := h.focusedDropdown(); dropdown != nil {
		if dropdown == h.org {
			optionLabel = h.tr("组织", "Organization")
		} else if dropdown == h.treeKind {
			optionLabel = h.tr("树类型", "Tree view")
		} else if label := strings.TrimSpace(tuiPlainMnemonic(dropdown.GetLabel())); label != "" {
			optionLabel = label
		}
	}
	if h.modal {
		switch h.dialogs[len(h.dialogs)-1].page {
		case "language":
			optionLabel = h.tr("语言", "Language")
		case "appearance":
			optionLabel = h.tr("主题", "Theme")
		}
	}
	switch p := h.focusedControl().(type) {
	case *tview.List:
		if p.GetItemCount() > 0 {
			text, _ := p.GetItemText(p.GetCurrentItem())
			if h.modal && h.dialogs[len(h.dialogs)-1].page == "appearance" && strings.HasPrefix(text, "[#") {
				if _, name, ok := strings.Cut(text, "]●[-] "); ok {
					text = "● " + name
				}
			}
			field(optionLabel, text)
		}
	case *tview.TreeView:
		if n := p.GetCurrentNode(); n != nil {
			ref, ok := n.GetReference().(*tuiNodeRef)
			if !ok || ref.more {
				break
			}
			field(h.tr("节点名称", "Node name"), cleanTUIText(ref.scope.Label))
			path := ref.scope.Path
			if path == "" {
				path = ref.scope.Label
			}
			field(h.tr("路径", "Path"), "/"+cleanTUIText(strings.TrimLeft(path, "/")))
			if ref.count != nil {
				field(h.tr("资产数量", "Asset count"), fmt.Sprint(*ref.count))
			}
			if h.organizationsEnabled {
				field(h.tr("组织", "Organization"), cleanTUIText(ref.scope.Org.Name))
			}
		}
	case *tview.DropDown:
		if index, text := p.GetCurrentOption(); index >= 0 {
			field(optionLabel, text)
		}
	case *tview.Table:
		row, col := p.GetSelection()
		if p == h.sessionTabs {
			if col >= 0 && col < p.GetColumnCount() {
				field(h.tr("窗口", "Window"), h.sessionTabLabel(col-1))
			}
			break
		}
		if row < 0 || row >= p.GetRowCount() || p.GetCell(row, 0).NotSelectable {
			break
		}
		for col := 0; col < p.GetColumnCount(); col++ {
			label := strings.TrimSpace(p.GetCell(0, col).Text)
			if label == "" { // Hidden columns have no header or value.
				continue
			}
			if label == "#" {
				label = h.tr("序号", "No.")
			}
			field(label, p.GetCell(row, col).Text)
		}
	case *tview.InputField:
		label := strings.TrimSpace(tuiPlainMnemonic(p.GetLabel()))
		if p == h.search {
			label = h.tr("搜索", "Search")
		} else if label == "" {
			label = h.tr("内容", "Content")
		}
		field(label, cleanTUIText(p.GetText()))
	case *tview.Button:
		field(h.tr("操作", "Action"), p.GetLabel())
	}
	return tuiPlainMnemonic(strings.Join(fields, "\n\n"))
}

func (h *terminalUI) showFullText() {
	text := h.focusedContent()
	if text == "" {
		return
	}
	view := tview.NewTextView().SetDynamicColors(false).SetWrap(true).SetWordWrap(true).
		SetTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).SetText(tview.Unescape(text))
	tuiDialogBorder(view.Box, h.tr("完整内容", "Full text"))
	view.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			h.dismissModal()
		}
	})
	h.openDialog("full-text", &tuiOverlay{Box: tview.NewBox(), child: view, width: 94, height: 24}, []tview.Primitive{view})
}
