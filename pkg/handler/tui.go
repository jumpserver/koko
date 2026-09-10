package handler

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
)

const tuiWakeKey tcell.Key = -100

const tuiClockLayout = "2006-01-02 15:04:05"
const tuiClockWidth = len(tuiClockLayout)

type terminalUI struct {
	app                                                 *tview.Application
	screen                                              tcell.Screen
	themeScreen                                         *tui.ThemeScreen
	lightTheme                                          bool
	accentColor                                         int
	pages                                               *tview.Pages
	session                                             ssh.Session
	user                                                *model.User
	data                                                tuiData
	conf                                                model.TerminalConfig
	ctx                                                 context.Context
	shutdown                                            <-chan struct{}
	cancel                                              context.CancelFunc
	dirty                                               atomic.Bool
	updates                                             chan func()
	assetJobs, treeJobs, detailJobs, orgJobs, countJobs chan func()
	countLoading                                        bool
	treeLoading                                         bool
	treeRequest                                         uint64
	lastNavigationWidth                                 int
	lastAssetWidth                                      int
	orgs                                                []tuiOrganization
	org                                                 *tview.DropDown
	organizationsEnabled                                bool
	organizationsReady                                  bool
	workspaceGeneration                                 uint64
	navigation, assetRegion, assetPane                  *tview.Flex
	orgPane, treePane                                   *tview.Flex
	body, header, treeTools, treeHead                   *tview.Flex
	tabRow                                              *tview.Flex
	headerTools                                         *tview.Flex
	treeKind                                            *tview.DropDown
	clock, brand, identity                              *tview.TextView
	exitHint                                            string
	language                                            *tview.Button
	appearance                                          *tview.Button
	treeActions                                         [2]*tview.Button
	sidebarWidth                                        int
	sidebarHidden, draggingSidebar, treeCollapsed       bool
	assetBottom, pager                                  *tview.Flex
	tree                                                *tview.TreeView
	pagerButtons                                        [2]*tview.Button
	search                                              *tview.InputField
	assetRefresh                                        *tview.Button
	table                                               *tview.Table
	status                                              *tview.TextView
	statusFailed                                        bool
	footer                                              *tview.TextView
	main                                                *tview.Flex
	scope                                               tuiScope
	assets                                              []model.PermAsset
	offset, total                                       int
	viewGeneration, assetGeneration, detailGeneration   uint64
	treeCount                                           int
	searchQuery                                         string
	dropdownNumber                                      string
	dropdownNumberDue                                   time.Time
	dropdownNumberTarget                                *tview.DropDown
	dropdownNumberTimer                                 *time.Timer
	lastInput                                           time.Time
	popup                                               *tui.Terminal
	modal                                               bool
	dialogs                                             []tuiDialog
	sessions                                            []*tuiSession
	sessionTabs                                         *tview.Table
	sessionMore                                         *tview.Button
	tabsOverflow                                        bool
	activeSession                                       int
	windowPrefix                                        bool
	tabHintUntil                                        time.Time
	fullscreen                                          bool
}

func cleanTUIText(s string) string {
	return tview.Escape(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
}

func (h *terminalUI) tr(zh, en string) string {
	lang := i18n.NewLang(h.data.lang)
	if lang == i18n.ZH {
		return zh
	}
	return lang.T(en)
}

func tuiBorder(box *tview.Box, title string, color tcell.Color) {
	box.SetBorder(true).SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft).
		SetTitleColor(tui.Muted).SetBorderColor(color).SetBackgroundColor(tui.Panel)
	tui.RoundedBorder(box)
}

func newTerminalUI(sess ssh.Session, user *model.User, api *service.JMService, conf model.TerminalConfig, screen tcell.Screen) *terminalUI {
	ctx, cancel := context.WithCancel(sess.Context())
	h := &terminalUI{session: sess, user: user, conf: conf, screen: screen,
		ctx: ctx, cancel: cancel, data: tuiData{api: api, userID: user.ID, lang: getUserDefaultLangCode(user)},
		updates: make(chan func(), 16), assetJobs: make(chan func(), 1), treeJobs: make(chan func(), 1),
		detailJobs: make(chan func(), 1), orgJobs: make(chan func(), 1), countJobs: make(chan func(), 1), lastInput: time.Now(), activeSession: -1, sidebarWidth: 40}
	h.themeScreen = tui.NewThemeScreen(screen)
	h.app = tview.NewApplication().SetScreen(h.themeScreen).EnableMouse(true).EnablePaste(true)
	h.pages = tview.NewPages()
	h.pages.SetBackgroundColor(tui.Background)
	h.build()
	h.app.SetRoot(h.main, true).SetFocus(h.tree).SetInputCapture(h.input)
	h.app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		cursorStyle := tcell.CursorStyleDefault
		if h.search.HasFocus() && !h.modal && h.activeSession < 0 {
			cursorStyle = tcell.CursorStyleBlinkingBar
		}
		s.SetCursorStyle(cursorStyle)
		if h.fullscreen {
			h.refreshShortcutLabels(h.shortcuts())
			return false
		}
		h.updateLayout(time.Now())
		for _, box := range []*tview.Box{h.navigation.Box, h.orgPane.Box, h.treePane.Box, h.assetPane.Box} {
			box.SetBorderColor(tui.Border).SetTitleColor(tui.Muted)
		}
		for _, pane := range []*tview.Flex{h.navigation, h.orgPane, h.treePane, h.assetPane} {
			if pane.HasFocus() {
				pane.SetBorderColor(tui.FocusBorder).SetTitleColor(tui.Accent)
			}
		}
		for _, item := range h.focusOrder() {
			if box, ok := item.(interface {
				SetBorderColor(tcell.Color) *tview.Box
			}); ok {
				color, titleColor := tui.Border, tui.Muted
				if item.HasFocus() {
					color, titleColor = tui.FocusBorder, tui.Accent
				}
				box.SetBorderColor(color).SetTitleColor(titleColor)
			}
			if table, ok := item.(*tview.Table); ok {
				if table == h.sessionTabs {
					continue
				}
				table.SetSelectedStyle(tui.InactiveSelected)
				if table.HasFocus() {
					table.SetSelectedStyle(tui.Selected)
				}
			}
		}
		h.table.SetSelectedStyle(tui.InactiveSelected)
		if h.table.HasFocus() {
			h.table.SetSelectedStyle(tui.Selected)
		}
		if node := h.tree.GetCurrentNode(); node != nil {
			node.SetSelectedTextStyle(tui.InactiveSelected)
			if h.tree.HasFocus() {
				node.SetSelectedTextStyle(tui.Selected)
			}
		}
		h.setWindowHelp()
		if w, ht := s.Size(); w < 72 || ht < 20 {
			s.Clear()
			tview.Print(s, h.tr("请放大终端至至少 72 × 20", "Resize terminal to at least 72 × 20"), 1, 1, max(0, w-2), tview.AlignLeft, tui.Foreground)
			return true
		}
		return false
	})
	h.main.SetMouseCapture(h.captureRootMouse)
	h.app.SetAfterDrawFunc(func(s tcell.Screen) {
		if h.fullscreen {
			return
		}
		h.drawSessionTabs(s)
		if !h.modal {
			h.drawSessionControls(s)
			if h.tabsOverflow {
				x, y, w, _ := h.sessionTabs.GetRect()
				h.sessionMore.SetRect(x+w-4, y, 3, 1)
				h.sessionMore.Draw(s)
			}
		}
		// Center the timestamp vertically alongside the shortcut text.
		x, y, w, ht := h.footer.GetRect()
		_, textY, _, textHeight := h.footer.GetInnerRect()
		h.clock.SetRect(x+w-tuiClockWidth-1, min(y+ht-1, textY+max(0, (textHeight-1)/2)), tuiClockWidth, 1)
		h.clock.Draw(s)
		h.drawDropdown(s)
		h.advanceTree()
	})
	return h
}

func (h *terminalUI) build() {
	title := h.conf.HeaderTitle
	if title == "" {
		title = "JumpServer · Koko"
	}
	h.brand = tview.NewTextView().SetDynamicColors(true).SetTextStyle(tcell.StyleDefault.Foreground(tui.Accent).Background(tui.Panel).Bold(true)).SetWrap(false).SetTextAlign(tview.AlignLeft)
	h.brand.SetText(cleanTUIText(title))
	name := h.user.Name
	if name == "" {
		name = h.user.Username
	}
	if h.user.Username != "" {
		name += "(" + h.user.Username + ")"
	}
	h.identity = tview.NewTextView().SetDynamicColors(true).SetTextStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetWrap(false).SetTextAlign(tview.AlignRight)
	h.identity.SetText(cleanTUIText(name))
	h.clock = tview.NewTextView().SetTextAlign(tview.AlignRight).SetTextStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetWrap(false)
	headerControlStyle := tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)
	h.language = tview.NewButton("").SetStyle(headerControlStyle).SetActivatedStyle(headerControlStyle.Underline(true)).SetSelectedFunc(h.showLanguage)
	h.appearance = tview.NewButton("").SetStyle(headerControlStyle).SetActivatedStyle(headerControlStyle.Underline(true)).SetSelectedFunc(h.showAppearance)
	divider := tview.NewTextView().SetText(" | ").SetTextStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetWrap(false)
	themeDivider := tview.NewTextView().SetText(" | ").SetTextStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetWrap(false)
	h.headerTools = tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(h.language, 10, 0, false).AddItem(themeDivider, 3, 0, false).AddItem(h.appearance, 12, 0, false).AddItem(divider, 3, 0, false).AddItem(h.identity, 28, 0, false)
	h.headerTools.SetBackgroundColor(tui.Panel)
	h.header = tview.NewFlex()
	h.header.Box = tview.NewBox()
	tuiBorder(h.header.Box, "", tui.Border)
	h.header.SetTitle("").SetBorderPadding(0, 0, 1, 1)
	h.org = tuiDropdown().SetTextOptions(" ", " ", "", " ▾", " … ")
	h.treeKind = tuiDropdown()
	h.treeKind.SetTextOptions(" ", " ", "", " ▾", " … ")
	for _, dropdown := range []*tview.DropDown{h.org, h.treeKind} {
		dropdown.SetFieldBackgroundColor(tui.Panel).
			SetLabelStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).
			SetFocusedStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel).Underline(true))
	}
	h.treeTools = tview.NewFlex()
	for i, label := range []string{"−", "↻"} {
		b := tview.NewButton(label).SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected)
		b.SetSelectedFunc(func() {
			if i == 0 {
				h.toggleTreeExpansion()
			} else {
				h.refreshView()
			}
		})
		h.treeActions[i] = b
		h.treeTools.AddItem(b, 5, 0, false)
	}
	h.treeTools.SetBackgroundColor(tui.Panel)
	h.treeHead = tview.NewFlex().AddItem(h.treeKind, 16, 0, false).AddItem(nil, 0, 1, false).AddItem(h.treeTools, 10, 0, false)
	h.treeHead.SetBackgroundColor(tui.Panel)
	h.treeHead.SetDrawFunc(func(s tcell.Screen, x, y, w, ht int) (int, int, int, int) {
		style := tcell.StyleDefault.Foreground(h.navigation.GetBorderColor()).Background(tui.Panel)
		h.treeKind.SetFieldTextColor(tui.Foreground)
		left, _, width, _ := h.navigation.GetRect()
		for col := left + 1; col < left+width-1; col++ {
			s.SetContent(col, y+ht-1, '─', nil, style)
		}
		s.SetContent(left, y+ht-1, '├', nil, style)
		s.SetContent(left+width-1, y+ht-1, '┤', nil, style)
		return x, y, w, min(1, ht)
	})
	h.tree = tview.NewTreeView().SetGraphicsColor(tui.Border)
	h.tree.SetBackgroundColor(tui.Panel).SetBorderPadding(0, 0, 0, 2)
	h.tree.SetDrawFunc(func(s tcell.Screen, x, y, w, ht int) (int, int, int, int) {
		if ht > 0 && w > 0 {
			tview.Print(s, h.tree.GetTitle(), x+w-1, y, 1, tview.AlignLeft, tui.Muted)
		}
		return h.tree.GetInnerRect()
	})
	h.tree.SetSelectedFunc(h.selectNode)
	h.tree.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseScrollUp:
			h.keepTreeSelectionInView(-1)
		case tview.MouseScrollDown:
			h.keepTreeSelectionInView(1)
		}
		return action, ev
	})
	h.tree.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		node := h.tree.GetCurrentNode()
		if node == nil {
			return e
		}
		if e.Key() == tcell.KeyHome || e.Key() == tcell.KeyEnd || e.Key() == tcell.KeyRune && (e.Rune() == 'g' || e.Rune() == 'G') {
			step := h.tree.GetRowCount()
			if e.Key() == tcell.KeyHome || e.Rune() == 'g' {
				step = -step
			}
			h.tree.Move(step)
			return nil
		}
		if e.Key() == tcell.KeyRight {
			ref, ok := node.GetReference().(*tuiNodeRef)
			if !ok {
				return e
			}
			if !ref.more && ref.scope == h.scope {
				h.app.SetFocus(h.table)
				return nil
			}
			if !ref.loaded {
				h.selectNode(node)
			} else {
				node.SetExpanded(true)
				h.refreshNodeLabel(node)
			}
			return nil
		}
		if e.Key() == tcell.KeyLeft {
			if node.IsExpanded() {
				node.SetExpanded(false)
				h.refreshNodeLabel(node)
			} else {
				h.tree.GetRoot().Walk(func(n, parent *tview.TreeNode) bool {
					if n == node && parent != nil && (h.scope.Mode != 0 || parent != h.tree.GetRoot()) {
						h.tree.SetCurrentNode(parent)
						return false
					}
					return true
				})
			}
			return nil
		}
		return e
	})
	h.search = tview.NewInputField().SetFieldBackgroundColor(tui.Panel).
		SetFieldTextColor(tui.Foreground).SetLabelStyle(tcell.StyleDefault.Foreground(tui.Accent).Background(tui.Panel)).SetPlaceholder(h.tr("名称、地址、备注", "Name, address, comment")).
		SetPlaceholderStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel))
	h.search.SetAcceptanceFunc(func(s string, _ rune) bool { return len([]rune(s)) <= 256 })
	tuiBorder(h.search.Box, "", tui.Border)
	h.search.SetTitle("").SetBorderPadding(0, 0, 1, 1)
	h.search.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			h.searchQuery = h.search.GetText()
			h.loadAssets(0)
		}
		if key == tcell.KeyEscape {
			h.app.SetFocus(h.table)
		}
	})
	h.assetRefresh = tview.NewButton("↻").SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected).SetSelectedFunc(h.refreshAssets)
	tuiBorder(h.assetRefresh.Box, "", tui.Border)
	h.assetRefresh.SetTitle("")
	assetToolbar := tview.NewFlex().AddItem(h.search, 0, 1, false).AddItem(nil, 1, 0, false).AddItem(h.assetRefresh, 8, 0, false)
	assetToolbar.SetBackgroundColor(tui.Panel)
	h.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(tui.Selected)
	h.table.SetBackgroundColor(tui.Panel)
	h.table.SetDrawFunc(func(_ tcell.Screen, x, y, w, ht int) (int, int, int, int) {
		h.layoutAssetColumns(w)
		return x, y, w, ht
	})
	h.table.SetSelectedFunc(func(row, _ int) { h.showAccounts(row) })
	enableTableScroll(h.table)
	h.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyLeft && ev.Modifiers() == 0 {
			h.focusAssetNode()
			return nil
		}
		return ev
	})
	h.status = tview.NewTextView().SetTextStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetDynamicColors(true)
	h.status.SetWrap(false).SetBackgroundColor(tui.Panel)
	pager := tview.NewFlex()
	for i, label := range []string{"[ " + h.tr("上页", "Previous"), h.tr("下页", "Next") + " ]"} {
		button := tview.NewButton(label).SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected)
		button.SetSelectedFunc(func() { h.changePage(i*2 - 1) })
		h.pagerButtons[i] = button
		pager.AddItem(button, 0, 1, false)
	}
	bottom := tview.NewFlex().AddItem(h.status, 0, 1, false).AddItem(pager, 16, 0, false)
	h.assetBottom, h.pager = bottom, pager
	h.navigation = tview.NewFlex().SetDirection(tview.FlexRow)
	tuiBorder(h.navigation.Box, "", tui.Border)
	h.navigation.SetTitle("")
	h.orgPane = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(h.org, 1, 0, false)
	h.orgPane.Box = tview.NewBox()
	h.orgPane.SetBackgroundColor(tui.Panel).SetBorderPadding(0, 0, 1, 1)
	h.orgPane.SetDrawFunc(func(s tcell.Screen, x, y, w, ht int) (int, int, int, int) {
		style := tcell.StyleDefault.Foreground(h.navigation.GetBorderColor()).Background(tui.Panel)
		for col := x; col < x+w; col++ {
			s.SetContent(col, y+ht-1, '─', nil, style)
		}
		s.SetContent(x-1, y+ht-1, '├', nil, style)
		s.SetContent(x+w, y+ht-1, '┤', nil, style)
		return h.orgPane.GetInnerRect()
	})
	h.treePane = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(h.treeHead, 2, 0, false).AddItem(h.tree, 0, 1, true)
	h.treePane.Box = tview.NewBox()
	h.treePane.SetBackgroundColor(tui.Panel)
	h.treePane.SetTitle("").SetBorderPadding(0, 1, 1, 1)
	h.assetPane = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(h.table, 0, 1, false).AddItem(nil, 1, 0, false).AddItem(bottom, 1, 0, false)
	h.assetPane.Box = tview.NewBox()
	tuiBorder(h.assetPane.Box, "", tui.Border)
	h.assetPane.SetTitle("").SetBorderPadding(1, 0, 1, 1)
	h.assetRegion = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(assetToolbar, 3, 0, false).AddItem(nil, 1, 0, false).AddItem(h.assetPane, 0, 1, false)
	h.assetRegion.SetBackgroundColor(tui.Panel)
	h.body = tview.NewFlex().AddItem(h.navigation, h.sidebarWidth, 0, true).AddItem(nil, 1, 0, false).AddItem(h.assetRegion, 0, 1, false)
	h.footer = tview.NewTextView().SetDynamicColors(true).SetTextStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel))
	h.footer.SetWrap(true).SetWordWrap(true)
	h.footer.SetBackgroundColor(tui.Panel).SetBorderPadding(1, 0, 1, tuiClockWidth+2)
	h.sessionTabs = tview.NewTable().SetSelectable(false, true).SetSeparator('│').SetBordersColor(tui.Muted)
	h.sessionTabs.SetBackgroundColor(tui.Panel)
	h.sessionTabs.SetTitle("").SetBorderPadding(0, 0, 2, 1)
	h.sessionMore = tview.NewButton("▾").SetStyle(headerControlStyle).SetActivatedStyle(headerControlStyle.Underline(true)).SetSelectedFunc(h.showSessionMenu)
	h.sessionTabs.SetSelectedFunc(func(_, col int) { h.activateSession(col - 1) })
	h.sessionTabs.SetSelectionChangedFunc(func(_, _ int) { h.scrollSessionTabs() })
	h.sessionTabs.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyHome || ev.Key() == tcell.KeyEnd {
			col := 0
			if ev.Key() == tcell.KeyEnd {
				col = max(0, h.sessionTabs.GetColumnCount()-1)
			}
			h.sessionTabs.Select(0, col)
			return nil
		}
		return ev
	})
	h.sessionTabs.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := ev.Position()
		if !h.sessionTabs.InRect(x, y) {
			return action, ev
		}
		if action == tview.MouseScrollUp || action == tview.MouseScrollDown || action == tview.MouseScrollLeft || action == tview.MouseScrollRight {
			_, col := h.sessionTabs.GetSelection()
			step := 1
			if action == tview.MouseScrollUp || action == tview.MouseScrollLeft {
				step = -1
			}
			h.app.SetFocus(h.sessionTabs)
			h.sessionTabs.Select(0, max(0, min(h.sessionTabs.GetColumnCount()-1, col+step)))
			return tview.MouseConsumed, nil
		}
		return action, ev
	})
	h.tabRow = tview.NewFlex()
	h.tabRow.SetBackgroundColor(tui.Panel)
	h.pages.AddPage("assets", h.body, true, true)
	h.main = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(h.header, 3, 0, false).AddItem(h.tabRow, 1, 0, false).
		AddItem(nil, 1, 0, false).AddItem(h.pages, 0, 1, true).AddItem(h.footer, 3, 0, false)
	h.main.Box = tview.NewBox().SetBackgroundColor(tui.Background).SetBorderPadding(1, 0, 1, 1)
	for _, layout := range []*tview.Flex{pager, bottom} {
		layout.SetBackgroundColor(tui.Panel)
	}
	for _, layout := range []*tview.Flex{h.body, h.main} {
		layout.SetBackgroundColor(tui.Background)
	}
	h.refreshLabels()
	h.rebuildNavigation()
	h.refreshSessionTabs()
	h.setWindowHelp()

	h.message(h.tr("正在加载工作区…", "Loading workspace…"))
}

// UI mutations run on tview's event loop. A bounded mailbox and synthetic key
// avoid QueueUpdateDraw goroutines getting stuck after Application.Stop.
func (h *terminalUI) update(f func()) {
	select {
	case h.updates <- f:
		h.dirty.Store(true)
	case <-h.ctx.Done():
	}
}

func (h *terminalUI) drainUpdates() {
	for i := 0; i < cap(h.updates); i++ {
		select {
		case f := <-h.updates:
			f()
		default:
			return
		}
	}
}

func (h *terminalUI) queue(ch chan func(), job func()) {
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- job:
	case <-h.ctx.Done():
	}
}

func (h *terminalUI) run() error {
	defer h.cancel()
	defer h.clearDropdownNumber()
	defer func() {
		for _, session := range h.sessions {
			session.terminal.Dispose()
		}
	}()
	for _, lane := range []chan func(){h.assetJobs, h.treeJobs, h.detailJobs, h.orgJobs, h.countJobs} {
		go func() {
			for {
				select {
				case work := <-lane:
					if h.ctx.Err() == nil {
						work()
					}
				case <-h.ctx.Done():
					return
				}
			}
		}()
	}
	h.loadWorkspace()
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		second := int64(0)
		for {
			select {
			case now := <-tick.C:
				if second != now.Unix() {
					second = now.Unix()
					h.dirty.Store(true)
				}
				// Wake the event loop for queued updates and the login idle timeout.
				if h.dirty.Swap(false) {
					if err := h.screen.PostEvent(tcell.NewEventKey(tuiWakeKey, 0, tcell.ModNone)); err != nil {
						h.dirty.Store(true)
					}
				}
			case <-h.shutdown:
				h.cancel()
				h.app.Stop()
				return
			case <-h.ctx.Done():
				h.app.Stop()
				return
			}
		}
	}()
	if h.conf.MaxIdleTime > 0 {
		go func() {
			interval := time.Duration(h.conf.MaxIdleTime) * time.Minute
			timer := time.NewTimer(interval)
			defer timer.Stop()
			for {
				select {
				case <-timer.C:
					h.update(func() {
						remaining := interval - time.Since(h.lastInput)
						if h.hasRunningSession() {
							timer.Reset(interval)
						} else if remaining <= 0 {
							h.app.Stop()
						} else {
							timer.Reset(remaining)
						}
					})
				case <-h.ctx.Done():
					return
				}
			}
		}()
	}

	if interval := config.GetConf().ClientAliveInterval; interval > 0 {
		go func() {
			tick := time.NewTicker(time.Duration(interval) * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-tick.C:
					_, _ = h.session.SendRequest("keepalive@openssh.com", true, nil)
				case <-h.ctx.Done():
					return
				}
			}
		}()
	}
	return h.app.Run()
}

func (h *terminalUI) message(s string) {
	h.statusFailed = false
	h.status.SetText(" " + cleanTUIText(s))
	h.assetBottom.RemoveItem(h.pager)
	if h.total > tuiPageSize {
		h.assetBottom.AddItem(h.pager, h.pagerWidth(), 0, false)
	} else if h.pager.HasFocus() {
		h.app.SetFocus(h.table)
	}
}

func (h *terminalUI) fail(err error) {
	logger.Errorf("TUI request for user %s: %s", h.user.ID, err)
	h.message(h.tr("加载失败", "Load failed"))
	h.statusFailed = true
}

func (h *terminalUI) loadWorkspace() {
	h.workspaceGeneration++
	generation := h.workspaceGeneration
	update := func(f func()) {
		h.update(func() {
			if generation == h.workspaceGeneration {
				f()
			}
		})
	}
	h.message(h.tr("正在加载工作区…", "Loading workspace…"))
	data := h.data
	h.queue(h.orgJobs, func() {
		started := time.Now()
		setting, err := data.client("ROOT").GetPublicSetting()
		logger.Debugf("TUI edition lookup took %s (xpack=%t, licensed=%t)", time.Since(started), setting.XpackEnabled, setting.ValidLicense)
		if err != nil {
			update(func() { h.fail(err) })
			return
		}
		// Match the Web navigation: installing X-Pack alone does not enable
		// organization switching; a valid enterprise license is also required.
		organizationsEnabled := setting.XpackEnabled && setting.ValidLicense
		update(func() {
			h.organizationsEnabled = organizationsEnabled
			h.organizationsReady = !organizationsEnabled
			h.rebuildNavigation()
			h.refreshLabels()
			if h.organizationsEnabled {
				h.message(h.tr("正在加载组织…", "Loading organizations…"))
			}

			h.setWindowHelp()
			if !h.organizationsEnabled {
				// Community Core uses the default scope internally, but it has no
				// organization UI or membership enumeration on the login path.
				h.scope.Org = tuiOrganization{ID: tuiDefaultOrganizationID}
				h.switchTree(h.scope.Mode)
			}
		})
		if !organizationsEnabled {
			return
		}
		started = time.Now()
		orgs, err := data.organizations(h.ctx, func(first tuiOrganization) {
			update(func() {
				if h.scope.Org.ID == "" {
					h.setOrganizations([]tuiOrganization{first})
				}
			})
		})
		logger.Debugf("TUI membership lookup took %s", time.Since(started))
		update(func() {
			if err != nil {
				h.refreshLabels()
				h.fail(err)
				return
			}
			h.organizationsReady = true
			h.refreshLabels()
			h.setOrganizations(orgs)
			if len(orgs) == 0 {
				h.viewGeneration++
				h.assetGeneration++
				h.detailGeneration++
				h.scope, h.assets, h.total = tuiScope{}, nil, 0
				h.tree.SetRoot(nil).SetCurrentNode(nil)
				h.table.Clear()
				h.message(h.tr("未加入任何组织", "No organization memberships"))
			}
		})
	})
}

func (h *terminalUI) setOrganizations(orgs []tuiOrganization) {
	if h.dropdownNumberTarget == h.org {
		h.clearDropdownNumber()
	}
	// Global scope uses the same per-user authorization API, across joined orgs.
	members := make([]tuiOrganization, 0, len(orgs)+1)
	for _, org := range orgs {
		if org.ID != tuiGlobalOrganizationID {
			members = append(members, org)
		}
	}
	orgs = members
	if h.organizationsEnabled && len(members) > 0 {
		orgs = append([]tuiOrganization{{ID: tuiGlobalOrganizationID, Name: h.tr("全局组织", "Global organization")}}, members...)
	}
	h.orgs = orgs
	labels := make([]string, len(orgs))
	selected := 0
	if h.scope.Org.ID == "" && len(orgs) > 1 && orgs[0].ID == tuiGlobalOrganizationID {
		selected = 1
	}
	for i, org := range orgs {
		labels[i] = cleanTUIText(org.Name)
		if org.ID == h.scope.Org.ID {
			selected = i
		}
	}
	h.org.SetOptions(labels, func(_ string, i int) {
		if i < 0 || i >= len(h.orgs) || h.scope.Org.ID == h.orgs[i].ID {
			return
		}
		h.scope.Org = h.orgs[i]
		if h.activeSession >= 0 {
			h.activateSession(-1)
		}
		h.search.SetText("")
		h.searchQuery = ""
		h.switchTree(h.scope.Mode)
	}).SetCurrentOption(selected)
}

func (h *terminalUI) switchTree(mode int) {
	if h.scope.Org.ID == "" {
		return
	}
	h.viewGeneration++
	h.detailGeneration++
	h.scope = tuiScope{Org: h.scope.Org, Mode: mode, Label: h.tr("全部资产", "All assets")}
	if mode == 2 {
		h.scope.Label = h.tr("全部收藏", "All favorites")
	}
	h.treeCount = 0
	h.countLoading = false
	h.treeLoading = false
	h.treeCollapsed = false
	h.treeActions[0].SetLabel("−")
	h.treeKind.SetCurrentOption(mode)

	depth, topLevel := 0, 0
	if mode == 0 {
		// tview needs one parent to hold the API's top-level siblings. Keep
		// that container out of both the display and keyboard navigation.
		depth, topLevel = -1, 1
	}
	root := h.node(h.scope.Label, &tuiNodeRef{scope: h.scope, depth: depth}).SetSelectable(mode != 0)
	h.tree.SetRoot(root).SetTopLevel(topLevel)
	h.selectFirstTreeNode()
	h.loadTree(root, h.scope, "")
	h.loadAssets(0)
	if !h.sidebarHidden && h.activeSession < 0 {
		h.app.SetFocus(h.tree)
	}
}

type tuiNodeRef struct {
	id             string
	scope          tuiScope
	loaded         bool
	cursor         string
	more           bool
	request        uint64
	count          *int
	countRequested bool
	depth          int
	autoRequested  bool
	defaultExpand  bool
}

func (h *terminalUI) node(label string, ref *tuiNodeRef) *tview.TreeNode {
	n := tview.NewTreeNode(cleanTUIText(label)).SetTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).SetSelectedTextStyle(tui.Selected).SetReference(ref).SetExpanded(false)
	h.refreshNodeLabel(n)
	return n
}

func (h *terminalUI) refreshNodeLabel(node *tview.TreeNode) {
	ref, ok := node.GetReference().(*tuiNodeRef)
	if !ok || ref.more {
		return
	}
	prefix := "  "
	if !ref.loaded || len(node.GetChildren()) > 0 {
		prefix = "▸ "
		if node.IsExpanded() {
			prefix = "▾ "
		}
	}
	suffix := ""
	if ref.count != nil {
		suffix = fmt.Sprintf(" (%d)", *ref.count)
	} else if ref.metricID() != "" {
		suffix = " (…)"
		if ref.countRequested && !h.countLoading {
			suffix = " (?)"
		}
	}
	depth := ref.depth
	width := max(1, h.navigationWidth()-6-depth*3-tview.TaggedStringWidth(prefix+suffix))
	label := runewidth.Truncate(ref.scope.Label, width, "…")
	node.SetText(prefix + cleanTUIText(label) + suffix)
}

func (h *terminalUI) selectNode(node *tview.TreeNode) {
	ref, ok := node.GetReference().(*tuiNodeRef)
	if !ok {
		return
	}
	if ref.more {
		if h.treeLoading {
			return
		}
		ref.autoRequested = true
		// The loader entry belongs to its parent; loading replaces this entry.
		h.tree.GetRoot().Walk(func(parent, _ *tview.TreeNode) bool {
			for _, child := range parent.GetChildren() {
				if child == node {
					h.loadTree(parent, ref.scope, ref.cursor)
					return false
				}
			}
			return true
		})
		return
	}
	h.scope = ref.scope
	h.detailGeneration++
	h.loadAssets(0)
	if !ref.loaded && h.scope.Mode != 1 {
		h.loadTree(node, ref.scope, "")
	} else {
		node.SetExpanded(!node.IsExpanded())
		h.refreshNodeLabel(node)
	}
}

func (h *terminalUI) loadTree(parent *tview.TreeNode, scope tuiScope, cursor string) {
	generation := h.viewGeneration
	parent.SetExpanded(true)
	h.treeRequest++
	treeRequest := h.treeRequest
	h.treeLoading = true
	ref := parent.GetReference().(*tuiNodeRef)
	ref.defaultExpand = false
	ref.request++
	request := ref.request
	data := h.data
	h.queue(h.treeJobs, func() {
		page, err := data.tree(scope, cursor)
		h.update(func() {
			if treeRequest == h.treeRequest {
				h.treeLoading = false
			}
			if generation != h.viewGeneration || request != ref.request {
				return
			}
			if err != nil {
				h.fail(err)
				return
			}
			replaced := 0
			var oldMore *tview.TreeNode
			if cursor == "" {
				parent.Walk(func(n, _ *tview.TreeNode) bool {
					if n != parent && !n.GetReference().(*tuiNodeRef).more {
						replaced++
					}
					return true
				})
			}
			if h.treeCount-replaced+len(page.Results) > 5000 {
				h.message(h.tr("节点过多，请折叠并刷新视图", "Tree limit reached; refresh this view"))
				return
			}
			if cursor == "" {
				h.treeCount -= replaced
				parent.ClearChildren()
			} else {
				children := parent.GetChildren()
				if len(children) > 0 {
					oldMore = children[len(children)-1]
					parent.SetChildren(children[:len(children)-1])
				}
			}
			parent.GetReference().(*tuiNodeRef).loaded = true
			byID := map[string]*tview.TreeNode{}
			parent.Walk(func(n, _ *tview.TreeNode) bool {
				byID[n.GetReference().(*tuiNodeRef).id] = n
				return true
			})
			added := 0
			typeTotal, hasTypeAmount := 0, false
			for _, item := range page.Results {
				if item.Meta.Type == "asset" || item.ID == "favorite-root" || item.ID == "ROOT" {
					continue
				}
				if byID[item.ID] != nil {
					continue
				}
				next := scope
				next.Label = item.Name
				var amount *int
				if scope.Mode == 1 {
					next.Label, amount = typeTreeAmount(item.Name)
					if item.Meta.Type == "category" && amount != nil {
						typeTotal += *amount
						hasTypeAmount = true
					}
				}
				next.NodeID, next.Key = item.Meta.Data.ID, item.Meta.Data.Key
				if next.Key == "" {
					next.Key = item.ID
				}
				if scope.Mode == 1 {
					next.Category = item.Meta.Category
					if item.Meta.Type == "type" {
						next.AssetType = item.Meta.AssetType
					}
					if item.Meta.Type == "platform" {
						next.Platform = item.ID
					}
				}
				if scope.Mode == 2 {
					next.FolderID = item.Meta.Data.ID
				}
				p := parent
				if found := byID[item.Parent]; found != nil {
					p = found
				}
				next.Path = next.Label
				if path := p.GetReference().(*tuiNodeRef).scope.Path; path != "" {
					next.Path = path + " / " + next.Label
				}
				n := h.node(next.Label, &tuiNodeRef{id: item.ID, scope: next, loaded: scope.Mode == 1, count: amount})
				byID[item.ID] = n
				h.treeCount++
				n.GetReference().(*tuiNodeRef).depth = p.GetReference().(*tuiNodeRef).depth + 1
				p.AddChild(n)
				// Expand API top-level nodes once, without recursively opening descendants.
				// Global authorization starts with collapsed organization roots.
				if p == h.tree.GetRoot() && !h.treeCollapsed && (scope.Org.ID != tuiGlobalOrganizationID || scope.Mode != 0) {
					n.SetExpanded(true)
					n.GetReference().(*tuiNodeRef).defaultExpand = scope.Mode != 1
				}
				if added == 0 && oldMore != nil && h.tree.GetCurrentNode() == oldMore {
					h.tree.SetCurrentNode(n)
				}
				added++
			}
			if hasTypeAmount {
				ref.count = &typeTotal
			}
			if next := nextTreeCursor(page.Pagination.Next); next != "" && next != cursor && added > 0 {
				parent.AddChild(h.node(h.tr("加载更多…", "Load more…"), &tuiNodeRef{scope: scope, cursor: next, more: true}))
			}
			if added == 0 && oldMore != nil && h.tree.GetCurrentNode() == oldMore {
				if children := parent.GetChildren(); len(children) > 0 {
					h.tree.SetCurrentNode(children[len(children)-1])
				} else {
					h.tree.SetCurrentNode(parent)
				}
			}
			if parent == h.tree.GetRoot() && scope.Mode == 0 && cursor == "" {
				h.selectFirstTreeNode()
			}
			parent.Walk(func(n, _ *tview.TreeNode) bool { h.refreshNodeLabel(n); return true })
		})
	})
}

func (h *terminalUI) refreshAssets() {
	h.loadAssets(h.offset)
}

func (h *terminalUI) loadAssets(offset int) {
	if h.scope.Org.ID == "" {
		return
	}
	h.assetGeneration++
	h.detailGeneration++
	generation := h.assetGeneration
	// Only Enter commits the input; refresh, paging and node changes retain it.
	scope, search := h.scope, h.searchQuery
	h.assets = nil
	h.table.Clear()
	h.offset = offset
	h.total = 0
	h.message(h.tr("加载资产…", "Loading assets…"))
	data := h.data
	h.queue(h.assetJobs, func() {
		started := time.Now()
		page, err := data.assets(scope, search, offset)
		logger.Debugf("TUI asset lookup took %s", time.Since(started))
		h.update(func() {
			if generation != h.assetGeneration {
				return
			}
			if err != nil {
				h.fail(err)
				return
			}
			h.assets, h.total = page.Data, page.Total
			h.renderAssets()
		})
	})
}

func (h *terminalUI) renderAssets() {
	h.table.Clear()
	h.lastAssetWidth = 0
	headings := []string{"#", h.tr("资产名称", "Asset"), h.tr("地址", "Address"), h.tr("平台", "Platform"), h.tr("备注", "Comment")}
	hidden := make(map[string]bool)
	for _, field := range config.GetConf().HiddenFields {
		hidden[strings.ToLower(strings.TrimSpace(field))] = true
	}
	columns := []string{"id", "name", "address", "platform", "comment"}
	for col, title := range headings {
		if hidden[columns[col]] && !isBuiltinFields(columns[col]) {
			continue
		}
		h.table.SetCell(0, col, tview.NewTableCell(" "+title+" ").SetSelectable(false).SetTextColor(tui.Muted).SetBackgroundColor(tui.Panel))
	}
	for row, asset := range h.assets {
		for col, value := range []string{fmt.Sprint(h.offset + row + 1), asset.Name, asset.Address, asset.Platform.Name, asset.Comment} {
			if hidden[columns[col]] && !isBuiltinFields(columns[col]) {
				continue
			}
			cell := tview.NewTableCell(" " + cleanTUIText(value) + " ").SetTextColor(tui.Foreground)
			if col != 1 && col != 2 {
				cell.SetTextColor(tui.Muted)
			}
			h.table.SetCell(row+1, col, cell)
		}
	}
	if len(h.assets) == 0 {
		h.message(h.tr("没有匹配资产", "No matching assets"))
		return
	}
	h.table.Select(1, 0).ScrollToBeginning()
	h.showAssetStatus()
}

func (h *terminalUI) showAssetStatus() {
	h.message(fmt.Sprintf("%d–%d / %d", h.offset+1, h.offset+len(h.assets), h.total))
}

func (h *terminalUI) changePage(step int) {
	next := h.offset + step*tuiPageSize
	if next < 0 || next >= h.total {
		return
	}
	h.loadAssets(next)
}

var builtinFields = map[string]struct{}{
	"id":      {},
	"name":    {},
	"address": {},
	"comment": {},
}

func isBuiltinFields(field string) bool {
	fieldName := strings.ToLower(field)
	_, ok := builtinFields[fieldName]
	return ok
}
