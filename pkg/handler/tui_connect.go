package handler

import (
	"fmt"
	"net"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
)

// tuiOverlay confines every child (including its mouse hit area) to a centered
// rectangle, recomputed after each outer terminal resize.
type tuiOverlay struct {
	*tview.Box
	child         tview.Primitive
	width, height int
	anchor        tview.Primitive
}

func (o *tuiOverlay) Draw(s tcell.Screen) {
	x, y, w, h := o.GetRect()
	width, height := min(o.width, max(1, w-4)), min(o.height, max(1, h-2))
	if o.width == 0 {
		width = max(1, w-4)
	}
	if o.height == 0 {
		height = max(1, h-2)
	}
	left, top := x+(w-width)/2, y+(h-height)/2
	if o.anchor != nil {
		ax, ay, aw, ah := o.anchor.GetRect()
		left, top = max(x, ax+aw-width), max(y, ay+ah)
	}
	o.child.SetRect(left, top, width, height)
	o.child.Draw(s)
}
func (o *tuiOverlay) Focus(f func(tview.Primitive)) { f(o.child) }
func (o *tuiOverlay) HasFocus() bool                { return o.child.HasFocus() }
func (o *tuiOverlay) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return o.child.InputHandler()
}
func (o *tuiOverlay) PasteHandler() func(string, func(tview.Primitive)) {
	return o.child.PasteHandler()
}
func (o *tuiOverlay) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(a tview.MouseAction, e *tcell.EventMouse, f func(tview.Primitive)) (bool, tview.Primitive) {
		if handler := o.child.MouseHandler(); handler != nil {
			handler(a, e, f)
		}
		// The underlying asset list must never receive clicks through a modal.
		return true, nil
	}
}

func (h *terminalUI) dismissModal() {
	if len(h.dialogs) == 0 {
		return
	}
	h.detailGeneration++
	last := h.dialogs[len(h.dialogs)-1]
	h.pages.RemovePage(last.page)
	h.dialogs = h.dialogs[:len(h.dialogs)-1]
	h.modal = len(h.dialogs) > 0
	if last.returnFocus != nil {
		h.app.SetFocus(last.returnFocus)
	}
	h.setWindowHelp()
}

func (h *terminalUI) showAccounts(row int) {
	if row < 1 || row > len(h.assets) || h.modal || h.popup != nil {
		return
	}
	if len(h.sessions) >= maxTUISessions {
		h.message(h.tr("最多同时保留 8 个会话，请先关闭一个", "Maximum 8 sessions; close one first"))
		return
	}
	asset, scope := h.assets[row-1], h.scope
	orgID := scope.Org.ID
	if orgID == tuiGlobalOrganizationID {
		orgID = asset.OrgID
		if orgID == "" || orgID == tuiGlobalOrganizationID {
			h.fail(fmt.Errorf("asset has no concrete organization"))
			return
		}
	}
	asset.OrgID = orgID
	h.detailGeneration++
	generation := h.detailGeneration
	h.message(h.tr("加载授权账号…", "Loading permitted accounts…"))
	data := h.data
	h.queue(h.detailJobs, func() {
		detail, err := data.client(orgID).GetUserPermAssetDetailById(h.user.ID, asset.ID)
		h.update(func() {
			if generation != h.detailGeneration {
				return
			}
			if err != nil {
				h.fail(err)
				return
			}
			if detail.ID != asset.ID || detail.OrgID != "" && detail.OrgID != orgID {
				h.fail(fmt.Errorf("asset detail does not match selected organization"))
				return
			}
			var accounts []model.PermAccount
			for _, account := range detail.PermedAccounts {
				if !account.IsAnonymous() {
					accounts = append(accounts, account)
				}
			}
			var protocols []string
			for _, p := range detail.PermedProtocols {
				for _, supported := range srvconn.SupportedProtocols() {
					if strings.EqualFold(p.Name, supported) {
						protocols = append(protocols, p.Name)
						break
					}
				}
			}
			if len(accounts) == 0 || len(protocols) == 0 {
				h.message(h.tr("没有可用的授权账号或终端协议", "No permitted accounts or terminal protocols"))
				return
			}
			if len(accounts) == 1 {
				h.showAssetStatus()
				h.connectPopup(asset, accounts[0], protocols[0])
				return
			}
			h.accountDialog(asset, accounts, protocols)
		})
	})
}

func (h *terminalUI) accountDialog(asset model.PermAsset, accounts []model.PermAccount, protocols []string) {
	protocol := protocols[0]
	protocolPicker := tview.NewDropDown().SetLabel(" "+h.tr("协议", "Protocol")+" ").SetLabelStyle(tcell.StyleDefault.Foreground(tui.Accent).Background(tui.Panel)).
		SetFieldTextColor(tui.Foreground).SetFieldBackgroundColor(tui.Panel).
		SetListStyles(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel), tui.Selected).SetFocusedStyle(tui.Selected)
	protocolPicker.SetOptions(protocols, func(_ string, i int) {
		if i >= 0 {
			protocol = protocols[i]
		}
	}).SetCurrentOption(0)
	protocolPicker.SetBackgroundColor(tui.Panel)
	accountsTable := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(tui.Selected)
	accountsTable.SetBackgroundColor(tui.Panel)
	for col, label := range []string{"#", h.tr("账号名称", "Account"), h.tr("用户名", "Username")} {
		accountsTable.SetCell(0, col, tview.NewTableCell(" "+label+" ").SetTextColor(tui.Muted).SetBackgroundColor(tui.Panel).SetSelectable(false))
	}
	for i, a := range accounts {
		for col, text := range []string{fmt.Sprint(i + 1), a.Name, a.Username} {
			cell := tview.NewTableCell(" " + cleanTUIText(text) + " ").SetTextColor(tui.Foreground)
			if col == 1 {
				cell.SetExpansion(1)
			}
			accountsTable.SetCell(i+1, col, cell)
		}
	}
	accountsTable.Select(1, 0)
	enableTableScroll(accountsTable)
	accountsTable.SetSelectedFunc(func(row, _ int) {
		if row < 1 || row > len(accounts) {
			return
		}
		h.dismissModal()
		h.connectPopup(asset, accounts[row-1], protocol)
	})
	close := tview.NewButton(h.tr("取消", "Cancel") + " · Esc").
		SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected).SetSelectedFunc(h.dismissModal)
	content := tview.NewFlex().SetDirection(tview.FlexRow)
	focus := []tview.Primitive{accountsTable}
	if len(protocols) > 1 {
		content.AddItem(protocolPicker, 1, 0, false)
		focus = append(focus, protocolPicker)
	}
	content.AddItem(accountsTable, 0, 1, true).AddItem(close, 1, 0, false)
	content.Box = tview.NewBox()
	tuiBorder(content.Box, h.tr("选择账号", "Select account")+" · "+cleanTUIText(asset.Name), tui.FocusBorder)
	content.SetTitleColor(tui.Accent).SetBorderPadding(1, 1, 1, 1)
	height := len(accounts) + 6
	if len(protocols) > 1 {
		height++
	}
	h.openDialog("dialog", &tuiOverlay{Box: tview.NewBox(), child: content, width: 76, height: min(24, height)}, append(focus, close))
	h.showAssetStatus()
}

type tuiAssetConnection struct {
	*tui.Terminal
	id, remoteAddr string
}

var _ proxy.UserConnection = (*tuiAssetConnection)(nil)

func (c *tuiAssetConnection) ID() string         { return c.id }
func (c *tuiAssetConnection) LoginFrom() string  { return "ST" }
func (c *tuiAssetConnection) RemoteAddr() string { return c.remoteAddr }
func (c *tuiAssetConnection) Pty() ssh.Pty {
	return ssh.Pty{Term: "xterm-256color", Window: c.Window()}
}
func (c *tuiAssetConnection) HandleRoomEvent(string, *exchange.RoomMessage) {}

const maxTUISessions = 9

type tuiSession struct {
	terminal      *tui.Terminal
	controls      []tview.Primitive
	duplicate     func()
	name, page    string
	done, closing bool
}

func (h *terminalUI) hasRunningSession() bool {
	for _, session := range h.sessions {
		if !session.done {
			return true
		}
	}
	return false
}

func (h *terminalUI) connectPopup(asset model.PermAsset, account model.PermAccount, protocol string) {
	if len(h.sessions) >= maxTUISessions {
		return
	}
	terminal, err := tui.NewTerminal(h.ctx, func() { h.dirty.Store(true) })
	if err == nil {
		err = terminal.SetPalette(tui.ThemePalette(h.lightTheme, h.accentColor))
		if err != nil {
			terminal.Dispose()
		}
	}
	if err != nil {
		h.fail(err)
		return
	}
	title := cleanTUIText(account.Username + " @ " + asset.Name + " · " + protocol)
	terminal.SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft)
	session := &tuiSession{terminal: terminal, name: asset.Name, page: common.UUID()}
	for n := 2; ; n++ {
		found := false
		for _, existing := range h.sessions {
			found = found || existing.name == session.name
		}
		if !found {
			break
		}
		session.name = fmt.Sprintf("%s #%d", asset.Name, n)
	}
	// A duplicate runs the normal token and ACL flow again, using the same
	// asset/account/protocol rather than sharing the existing terminal stream.
	session.duplicate = func() { h.connectPopup(asset, account, protocol) }
	h.buildSessionControls(session)
	h.pages.AddPage(session.page, terminal, true, false)
	h.sessions = append(h.sessions, session)
	h.activateSession(len(h.sessions) - 1)
	// Match the separate session window, excluding global header/tabs/footer.
	x, y, w, ht := h.pages.GetRect()
	terminal.SetRect(x, y, max(3, w), max(3, ht))
	remoteAddr, _, _ := net.SplitHostPort(h.session.RemoteAddr().String())
	conn := &tuiAssetConnection{Terminal: terminal, id: session.page, remoteAddr: remoteAddr}
	api := h.data.client(asset.OrgID)
	lang := h.data.lang
	// One goroutine per session; at most eight, including pending approvals and
	// closing connections. Selecting another tab does not cancel this context.
	go func() {
		if err := srvconn.IsSupportedProtocol(protocol); err != nil {
			_, _ = fmt.Fprintf(terminal, "\r\n%s\r\n", err)
		} else {
			connectSelectedAsset(conn, api, h.user, asset, account, protocol, lang)
		}
		_ = terminal.Close()
		h.update(func() {
			session.done = true
			if h.popup == terminal {
				h.setFullscreen(false)
			}
			if session.closing {
				h.removeSession(session)
				return
			}
			terminal.SetTitle(" " + title + " · " + h.tr("连接已结束", "Session ended") + " ")
			h.refreshSessionTabs()
		})
	}()
}

func (h *terminalUI) buildSessionControls(session *tuiSession) {
	style := tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)
	for _, action := range []func(){
		func() {
			if len(h.sessions) < maxTUISessions && session.duplicate != nil {
				session.duplicate()
			}
		},
		func() { h.closeSession(session) },
		func() { h.setFullscreen(!h.fullscreen) },
	} {
		session.controls = append(session.controls, tview.NewButton("").SetStyle(style).SetActivatedStyle(style.Underline(true)).SetDisabledStyle(style.Dim(true)).SetSelectedFunc(action))
	}
}

func (h *terminalUI) drawSessionControls(screen tcell.Screen) {
	if h.activeSession < 0 {
		return
	}
	session := h.sessions[h.activeSession]
	x, y, w, _ := session.terminal.GetRect()
	right := x + w - 1
	for i := len(session.controls) - 1; i >= 0; i-- {
		button := session.controls[i].(*tview.Button)
		width := min(tview.TaggedStringWidth(button.GetLabel())+2, max(1, (w-8)/len(session.controls)))
		right -= width
		button.SetRect(right, y, width, 1)
		button.Draw(screen)
		if i > 0 {
			right -= 3
			tview.Print(screen, " · ", right, y, 3, tview.AlignLeft, tui.Muted)
		}
	}
}

func (h *terminalUI) refreshSessionTabs() {
	selected := h.activeSession + 1
	if h.sessionTabs.HasFocus() {
		_, selected = h.sessionTabs.GetSelection()
		selected = min(selected, len(h.sessions))
	}
	_, offset := h.sessionTabs.GetOffset()
	h.sessionTabs.Clear().SetOffset(0, offset)
	h.sessionTabs.SetCell(0, 0, tview.NewTableCell(h.sessionTabLabel(-1)).SetTextColor(tui.Muted).SetClickedFunc(func() bool { h.activateSession(-1); return true }))
	for i := range h.sessions {
		h.sessionTabs.SetCell(0, i+1, tview.NewTableCell(h.sessionTabLabel(i)).SetMaxWidth(36).SetTextColor(tui.Muted).SetClickedFunc(func() bool { h.activateSession(i); return true }))
	}
	h.sessionTabs.Select(0, selected)
}

// Restore inline icon/key colors after Table's selected-cell recoloring pass.
func (h *terminalUI) drawSessionTabs(screen tcell.Screen) {
	_, selected := h.sessionTabs.GetSelection()
	if selected < 0 || selected >= h.sessionTabs.GetColumnCount() {
		return
	}
	cell := h.sessionTabs.GetCell(0, selected)
	x, y, width := cell.GetLastPosition()
	// Off-screen cells retain their previous coordinates after scrolling.
	if row, col := h.sessionTabs.CellAt(x, y); row != 0 || col != selected || width <= 0 {
		return
	}
	tview.Print(screen, cell.Text, x, y, width, tview.AlignLeft, tui.Muted)
	if tview.TaggedStringWidth(cell.Text) > width {
		tview.Print(screen, "…", x+width-1, y, 1, tview.AlignLeft, tui.Muted)
	}
	if h.sessionTabs.HasFocus() {
		for col := x; col < x+width; col++ {
			r, combining, style, _ := screen.GetContent(col, y)
			screen.SetContent(col, y, r, combining, style.Underline(true))
		}
	}
}

// Keep the selected label fully visible, rather than truncating the active tab
// at the right edge. The same viewport serves keys, the wheel and tab changes.
func (h *terminalUI) scrollSessionTabs() {
	count := h.sessionTabs.GetColumnCount()
	if count == 0 {
		return
	}
	_, selected := h.sessionTabs.GetSelection()
	_, first := h.sessionTabs.GetOffset()
	selected = max(0, min(selected, count-1))
	first = max(0, min(first, selected))
	w, _ := h.screen.Size()
	available, used := w-5, 0
	available = max(1, available)
	width := func(col int) int {
		cell := h.sessionTabs.GetCell(0, col)
		n := tview.TaggedStringWidth(cell.Text)
		if cell.MaxWidth > 0 {
			n = min(n, cell.MaxWidth)
		}
		return n + 1
	}
	total := 0
	for col := 0; col < count; col++ {
		total += width(col)
	}
	h.tabsOverflow = total > available
	padding := 1
	if h.tabsOverflow {
		available = max(1, available-3)
		padding += 3
	}
	h.sessionTabs.SetBorderPadding(0, 0, 2, padding)
	for col := first; col <= selected; col++ {
		used += width(col)
	}
	for used > available && first < selected {
		used -= width(first)
		first++
	}
	for first > 0 && used+width(first-1) <= available {
		first--
		used += width(first)
	}
	h.sessionTabs.SetOffset(0, first)
}

func (h *terminalUI) sessionTabLabel(index int) string {
	label := "▤ " + h.tr("资产面板", "Asset panel")
	if index >= 0 && index < len(h.sessions) {
		session := h.sessions[index]
		state := "●"
		if session.done {
			state = "○"
		}
		if session.closing {
			state = "…"
		}
		label = state + " " + cleanTUIText(session.name)
	}
	return " " + label + " "
}

func (h *terminalUI) showSessionMenu() {
	h.closeDropdown()
	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).
		SetMainTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).SetSelectedStyle(tui.Selected)
	for i := -1; i < len(h.sessions); i++ {
		list.AddItem(fmt.Sprintf("%d %s", i+1, strings.TrimSpace(h.sessionTabLabel(i))), "", 0, func() { h.activateSession(i) })
	}
	list.SetCurrentItem(h.activeSession + 1)
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune && ev.Rune() >= '0' && ev.Rune() < '0'+rune(list.GetItemCount()) {
			h.activateSession(int(ev.Rune()-'0') - 1)
			return nil
		}
		return ev
	})
	tuiBorder(list.Box, h.tr("窗口栏", "Window bar"), tui.FocusBorder)
	list.SetBorderPadding(0, 0, 1, 1)
	h.openDialog("sessions", &tuiOverlay{Box: tview.NewBox(), child: list, width: 54, height: list.GetItemCount() + 2, anchor: h.sessionTabs}, []tview.Primitive{list})
}

func (h *terminalUI) focusSessionTabs() {
	h.app.SetFocus(h.sessionTabs)
}

// Fullscreen reserves only one control row, so the exit button cannot cover
// remote output. The workspace keeps its tree, table and session state intact.
func (h *terminalUI) setFullscreen(enabled bool) {
	if h.fullscreen == enabled || enabled && (h.popup == nil || h.modal) {
		return
	}
	h.fullscreen = enabled
	h.popup.SetBorder(!enabled)
	if enabled {
		h.popup.SetDrawFunc(nil)
		button := h.sessions[h.activeSession].controls[2].(*tview.Button)
		button.SetLabel(tuiMnemonic(h.tr("退出全屏", "Exit fullscreen"), "Ctrl+] f"))
		bar := tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(button, tview.TaggedStringWidth(button.GetLabel())+2, 0, false)
		bar.SetBackgroundColor(tui.Panel)
		root := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(bar, 1, 0, false).AddItem(h.popup, 0, 1, true)
		root.SetMouseCapture(h.captureRootMouse)
		h.app.SetRoot(root, true)
	} else {
		tui.RoundedBorder(h.popup.Box)
		h.app.SetRoot(h.main, true)
	}
	h.app.SetFocus(h.popup)
	h.dirty.Store(true)
}

func (h *terminalUI) activateSession(index int) {
	if index < -1 || index >= len(h.sessions) {
		return
	}
	h.setFullscreen(false)
	for h.modal {
		h.dismissModal()
	}
	h.activeSession = index
	h.popup = nil
	if index == -1 {
		h.pages.SwitchToPage("assets")
		h.app.SetFocus(h.table)
	} else {
		session := h.sessions[index]
		h.pages.SwitchToPage(session.page)
		h.popup = session.terminal
		h.app.SetFocus(h.popup)
	}
	h.setWindowHelp()
	h.refreshSessionTabs()
}

func (h *terminalUI) closeSession(session *tuiSession) {
	session.closing = true
	session.terminal.Dispose()
	h.activateSession(-1)
	if session.done {
		h.removeSession(session)
	} else {
		h.refreshSessionTabs()
	}
}

func (h *terminalUI) removeSession(session *tuiSession) {
	active := (*tuiSession)(nil)
	if h.activeSession >= 0 && h.activeSession < len(h.sessions) {
		active = h.sessions[h.activeSession]
	}
	for i, s := range h.sessions {
		if s == session {
			h.sessions = append(h.sessions[:i], h.sessions[i+1:]...)
			break
		}
	}
	h.pages.RemovePage(session.page)
	session.terminal.Dispose()
	index := -1
	for i, s := range h.sessions {
		if s == active {
			index = i
		}
	}
	if active == session {
		h.activateSession(-1)
	} else {
		// Completion of a background close must not dismiss a dialog or move
		// keyboard focus away from the control the user is operating.
		h.activeSession = index
		h.refreshSessionTabs()
		h.setWindowHelp()
	}
}

func (h *terminalUI) quit() {
	h.windowPrefix = false
	message := h.tr("确定退出本次 Koko SSH 会话？", "Quit this Koko SSH session?")
	if len(h.sessions) > 0 {
		message += "\n\n" + h.tr("退出将断开所有资产会话。", "Quitting will disconnect all asset sessions.")
	}
	dialog := tview.NewModal().SetText(message).
		AddButtons([]string{h.tr("返回", "Back"), h.tr("断开并退出", "Disconnect and quit")}).
		SetDoneFunc(func(index int, _ string) {
			if index == 1 {
				h.app.Stop()
			} else {
				h.dismissModal()
			}
		})
	dialog.SetBackgroundColor(tui.Panel).SetTextColor(tui.Foreground).
		SetButtonStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Raised)).SetButtonActivatedStyle(tui.Selected).
		SetBorderColor(tui.FocusBorder)
	dialog.Box.SetBackgroundColor(tui.Panel)
	tui.RoundedBorder(dialog.Box)
	h.openDialog("quit", dialog, nil)
}

// tview handles vertical wheel/PageUp/PageDown and keyboard horizontal scrolling.
// Add horizontal wheel gestures too; row selection is unchanged while scrolling.
func enableTableScroll(table *tview.Table) {
	table.SetMouseCapture(func(a tview.MouseAction, e *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := e.Position()
		if !table.InRect(x, y) {
			return a, e
		}
		if a == tview.MouseLeftDoubleClick {
			row, col := table.CellAt(x, y)
			if row > 0 {
				table.Select(row, col)
				table.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(tview.Primitive) {})
			}
			return tview.MouseConsumed, nil
		}
		delta := 0
		if a == tview.MouseScrollLeft {
			delta = -1
		}
		if a == tview.MouseScrollRight {
			delta = 1
		}
		if e.Modifiers()&tcell.ModShift != 0 {
			if a == tview.MouseScrollUp {
				delta = -1
			}
			if a == tview.MouseScrollDown {
				delta = 1
			}
		}
		if delta != 0 {
			row, col := table.GetOffset()
			table.SetOffset(row, min(max(0, table.GetColumnCount()-1), max(0, col+delta)))
			return tview.MouseConsumed, nil
		}
		return a, e
	})
}
