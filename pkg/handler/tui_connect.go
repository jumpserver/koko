package handler

import (
	"bytes"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
)

// tuiOverlay confines every child (including its mouse hit area) to a centered
// rectangle, recomputed after each outer terminal resize.
type tuiOverlay struct {
	*tview.Box
	child          tview.Primitive
	width, height  int
	anchor         tview.Primitive
	paste          func(string, func(tview.Primitive))
	dismissOutside func()
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
		left, top = max(x, ax+aw-width), max(0, ay+ah)
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
	if o.paste != nil {
		return o.paste
	}
	return o.child.PasteHandler()
}
func (o *tuiOverlay) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(a tview.MouseAction, e *tcell.EventMouse, f func(tview.Primitive)) (bool, tview.Primitive) {
		if o.dismissOutside != nil && a == tview.MouseLeftDown {
			mx, my := e.Position()
			x, y, width, height := o.child.GetRect()
			if mx < x || mx >= x+width || my < y || my >= y+height {
				o.dismissOutside()
				return true, o
			}
		}
		if handler := o.child.MouseHandler(); handler != nil {
			_, capture := handler(a, e, f)
			// Native dropdowns capture the mouse while their list is open. Keep
			// that capture even when the list extends beyond the dialog bounds.
			return true, capture
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
	assetIndex := row - tuiAssetTableHeaderRows
	if assetIndex < 0 || assetIndex >= len(h.assets) || h.modal || h.popup != nil {
		return
	}
	asset, scope := h.assets[assetIndex], h.scope
	orgID := scope.Org.ID
	if orgID == tuiGlobalOrganizationID {
		orgID = asset.OrgID
		if orgID == "" || orgID == tuiGlobalOrganizationID {
			h.fail(fmt.Errorf("asset has no concrete organization"))
			return
		}
	}
	asset.OrgID = orgID
	if !h.assetCanConnect(assetIndex) {
		h.showUnavailableAsset(asset)
		return
	}
	if len(h.sessions) >= maxTUISessions {
		h.message(h.tr("最多同时保留 8 个会话，请先关闭一个", "Maximum 8 sessions; close one first"))
		return
	}
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
			sortTUIAccounts(accounts)
			var protocols []string
			supportedProtocols := srvconn.SupportedProtocols()
			for _, supported := range supportedProtocols {
				for _, p := range detail.PermedProtocols {
					if strings.EqualFold(p.Name, supported) {
						if !slices.Contains(protocols, supported) {
							protocols = append(protocols, supported)
						}
						break
					}
				}
			}
			if len(accounts) == 0 || len(protocols) == 0 {
				h.message(h.tr("没有可用的授权账号或终端协议", "No permitted accounts or terminal protocols"))
				return
			}
			if len(accounts) == 1 && len(protocols) == 1 {
				h.showAssetStatus()
				h.connectPopup(asset, accounts[0], protocols[0])
				return
			}
			h.accountDialog(asset, accounts, protocols)
		})
	})
}

func (h *terminalUI) showUnavailableAsset(asset model.PermAsset) {
	terminalProtocols := fmt.Sprintf(h.tr("终端仅支持 %s 等协议。", "Terminal supports only these protocols: %s."),
		strings.Join(srvconn.SupportedProtocols(), ", "))
	message := func(protocols string) string {
		return fmt.Sprintf(h.tr("当前资产的协议（%s）不支持，无法连接", "This asset's protocols (%s) are unsupported, so it cannot be connected"), protocols) +
			"\n\n" + terminalProtocols
	}
	initial, height := message(h.tr("加载中…", "Loading…")), 11
	if !asset.IsActive {
		initial = h.tr("当前资产已被禁用，无法连接", "This asset is disabled and cannot be connected")
		height = 7
	}
	view := tui.NewTextView().SetDynamicColors(false).SetWrap(true).SetWordWrap(true).
		SetTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).
		SetTextAlign(tview.AlignLeft).SetText(initial)
	view.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			h.dismissModal()
		}
	})
	close := tui.NewButton(h.tr("关闭", "Close") + " · Esc").
		SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).
		SetActivatedStyle(tui.Selected).SetSelectedFunc(h.dismissModal)
	closeRow := tui.NewFlex().AddItem(nil, 0, 1, false).
		AddItem(close, tview.TaggedStringWidth(close.GetLabel())+4, 0, false).
		AddItem(nil, 0, 1, false)
	closeRow.SetBackgroundColor(tui.Panel)
	content := tui.NewFlex().SetDirection(tview.FlexRow).
		AddItem(view, 0, 1, true).AddItem(nil, 1, 0, false).AddItem(closeRow, 1, 0, false)
	content.Box = tview.NewBox()
	content.SetBackgroundColor(tui.Panel)
	tuiDialogBorder(content.Box, h.tr("无法连接", "Connection unavailable")+" · "+cleanTUIText(asset.Name))
	h.openDialog("unavailable-asset", &tuiOverlay{Box: tview.NewBox(), child: content, width: 68, height: height},
		[]tview.Primitive{view, close})
	if !asset.IsActive {
		return
	}
	generation, data := h.detailGeneration, h.data
	h.queue(h.detailJobs, func() {
		detail, err := data.client(asset.OrgID).GetUserPermAssetDetailById(h.user.ID, asset.ID)
		h.update(func() {
			if generation != h.detailGeneration {
				return
			}
			if err != nil || detail.ID != asset.ID || detail.OrgID != "" && detail.OrgID != asset.OrgID {
				if err == nil {
					err = fmt.Errorf("asset detail does not match selected organization")
				}
				h.fail(err)
				view.SetText(message(h.tr("暂时无法获取", "Unavailable")))
				return
			}
			var protocols []string
			for _, protocol := range detail.PermedProtocols {
				name := cleanTUIText(strings.TrimSpace(protocol.Name))
				if name != "" && !slices.Contains(protocols, name) {
					protocols = append(protocols, name)
				}
			}
			separator := ", "
			if language := i18n.NewLang(h.data.lang); language == i18n.ZH || language == i18n.ZHHant {
				separator = "、"
			}
			current := h.tr("无可用协议", "No available protocols")
			if len(protocols) > 0 {
				current = strings.Join(protocols, separator)
			}
			view.SetText(tview.Unescape(message(current)))
			view.ScrollToBeginning()
		})
	})
}

func (h *terminalUI) accountDialog(asset model.PermAsset, accounts []model.PermAccount, protocols []string) {
	accountPicker := tuiDropdown().SetLabel(h.tr("账号", "Account")+" ").
		SetTextOptions(" ", " ", " ", "", " "+h.tr("请选择账号", "Select account"))
	accountLabels := make([]string, 0, len(accounts))
	search := &tuiAccountSearch{picker: accountPicker, field: tui.NewInputField().SetLabel(h.tr("搜索", "Search") + " ").
		SetPlaceholder(h.tr("账号名称 / 用户名", "Account name / username")).
		SetLabelStyle(tcell.StyleDefault.Foreground(tui.Accent).Background(tui.Panel)).
		SetFieldStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).
		SetPlaceholderStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel))}
	search.field.SetMaxLength(tuiSearchMaxLength)
	search.field.SetBackgroundColor(tui.Panel)
	searchText := make([]string, 0, len(accounts))
	for i, account := range accounts {
		label := account.Username
		if account.Name != "" {
			label = account.Name + " (" + account.Username + ")"
		}
		accountLabels = append(accountLabels, cleanTUIText(label))
		searchText = append(searchText, strings.ToLower(account.Name+" "+account.Username))
		search.matches = append(search.matches, i)
	}
	protocolPicker := tuiDropdown().SetLabel(h.tr("协议", "Protocol")+" ").
		SetTextOptions(" ", " ", " ", "", " "+h.tr("请选择协议", "Select protocol"))
	labelWidth := max(tview.TaggedStringWidth(accountPicker.GetLabel()), tview.TaggedStringWidth(protocolPicker.GetLabel()))
	var pressedButton *tui.Button
	for _, picker := range []*tui.DropDown{accountPicker, protocolPicker} {
		picker.SetLabelWidth(labelWidth)
		picker.SetFocusedStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel))
		tuiBorder(picker.Box, "", tui.Border)
		picker.SetBorderPadding(0, 0, 1, 1)
		pinDialogDropdownIndicator(picker)
		picker.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if action == tview.MouseLeftDown {
				pressedButton = nil
			}
			if !picker.IsOpen() && action == tview.MouseLeftDown && picker.InRect(ev.Position()) {
				h.app.SetFocus(picker)
				h.openFocusedDropdown()
				return tview.MouseConsumed, nil
			}
			// A captured menu can receive mouse events after focus moves to
			// another control. Resolve the list owned by this picker.
			var list *tui.List
			if picker.IsOpen() {
				picker.Focus(func(p tview.Primitive) { list = p.(*tui.List) })
			}
			if picker == accountPicker && list != nil {
				if search.field.InRect(ev.Position()) {
					if consumed, _ := search.field.MouseHandler()(action, ev, func(tview.Primitive) {}); consumed {
						return tview.MouseConsumed, nil
					}
				}
				if len(search.matches) == 0 && action == tview.MouseLeftDown && list.InRect(ev.Position()) {
					return tview.MouseConsumed, nil
				}
			}
			if list != nil && (action == tview.MouseScrollUp || action == tview.MouseScrollDown) {
				step := 1
				if action == tview.MouseScrollUp {
					step = -1
				}
				list.SetCurrentItem(max(0, min(list.GetItemCount()-1, list.GetCurrentItem()+step)))
				return tview.MouseConsumed, nil
			}
			if list != nil && action == tview.MouseLeftDown {
				x, y := ev.Position()
				lx, _, lw, _ := list.GetRect()
				_, row, _, visible := list.GetInnerRect()
				if visible > 1 && list.GetItemCount() > visible && x == lx+lw-2 && y >= row && y < row+visible {
					list.SetCurrentItem((y - row) * (list.GetItemCount() - 1) / (visible - 1))
					return tview.MouseConsumed, nil
				}
			}
			return action, ev
		})
	}
	connect := tui.NewButton(h.tr("连接", "Connect") + " · Enter").
		SetStyle(tcell.StyleDefault.Foreground(tui.Accent).Background(tui.Panel)).SetActivatedStyle(tui.Selected).
		SetDisabledStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetDisabled(true)
	selectedAccount := -1
	connect.SetSelectedFunc(func() {
		index, protocol := protocolPicker.GetCurrentOption()
		if selectedAccount < 0 || selectedAccount >= len(accounts) || index < 0 || index >= len(protocols) {
			return
		}
		h.dismissModal()
		h.connectPopup(asset, accounts[selectedAccount], protocol)
	})
	updateConnect := func() {
		protocol, _ := protocolPicker.GetCurrentOption()
		connect.SetDisabled(selectedAccount < 0 || protocol < 0)
	}
	selectAccount := func(_ string, index int) {
		selectedAccount = -1
		if index >= 0 && index < len(search.matches) {
			selectedAccount = search.matches[index]
		}
		updateConnect()
		if selectedAccount >= 0 && accountPicker.HasFocus() {
			h.app.SetFocus(protocolPicker)
		}
	}
	accountPicker.SetOptions(accountLabels, selectAccount)
	search.field.SetChangedFunc(func(query string) {
		query = strings.ToLower(strings.TrimSpace(query))
		search.matches = search.matches[:0]
		var labels []string
		for i, text := range searchText {
			if strings.Contains(text, query) {
				search.matches = append(search.matches, i)
				labels = append(labels, accountLabels[i])
			}
		}
		if len(labels) == 0 {
			labels = []string{h.tr("没有匹配的账号", "No matching accounts")}
		}
		accountPicker.SetOptions(labels, selectAccount).SetCurrentOption(-1)
	})
	protocolPicker.SetOptions(protocols, func(_ string, index int) {
		updateConnect()
		if !connect.IsDisabled() && protocolPicker.HasFocus() {
			h.app.SetFocus(connect)
		}
	})
	accountOption, protocolOption := h.preferredConnectionOptions(asset, accounts, protocols)
	accountPicker.SetCurrentOption(accountOption)
	if protocolOption >= 0 {
		protocolPicker.SetCurrentOption(protocolOption)
	} else if len(protocols) == 1 {
		protocolPicker.SetCurrentOption(0)
	}
	close := tui.NewButton(h.tr("取消", "Cancel") + " · Esc").
		SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).SetActivatedStyle(tui.Selected).SetSelectedFunc(h.dismissModal)
	// A click that closes a dropdown must not activate a button underneath it.
	for _, button := range []*tui.Button{close, connect} {
		button.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if !button.InRect(ev.Position()) {
				return action, ev
			}
			switch action {
			case tview.MouseLeftDown:
				pressedButton = button
			case tview.MouseLeftClick:
				if pressedButton != button {
					return tview.MouseConsumed, nil
				}
				pressedButton = nil
			}
			return action, ev
		})
	}
	buttons := tui.NewFlex().AddItem(close, 0, 1, false).AddItem(connect, 0, 1, false)
	buttons.SetBackgroundColor(tui.Panel)
	content := tui.NewFlex().SetDirection(tview.FlexRow).AddItem(accountPicker, 3, 0, true).
		AddItem(nil, 1, 0, false).AddItem(protocolPicker, 3, 0, false).AddItem(nil, 1, 0, false).AddItem(buttons, 1, 0, false)
	content.Box = tview.NewBox()
	tuiDialogBorder(content.Box, h.tr("选择账号", "Select account")+" · "+cleanTUIText(asset.Name))
	overlay := &tuiOverlay{Box: tview.NewBox(), child: content, width: 62, height: 13}
	overlay.paste = func(text string, setFocus func(tview.Primitive)) {
		if accountPicker.IsOpen() && accountPicker.HasFocus() {
			search.field.PasteHandler()(text, func(tview.Primitive) {})
		} else {
			content.PasteHandler()(text, setFocus)
		}
	}
	h.openDialog("dialog", overlay, []tview.Primitive{accountPicker, protocolPicker, close, connect})
	h.dialogs[len(h.dialogs)-1].boundedDropdowns = true
	h.dialogs[len(h.dialogs)-1].accountSearch = search
	h.dialogs[len(h.dialogs)-1].defaultButton = connect
	h.showAssetStatus()
}

type tuiAssetConnection struct {
	*tui.Terminal
	id, remoteAddr   string
	screen           *tui.ThemeScreen
	onPassthroughEnd func()
	zmodemMu         sync.Mutex
	zmodemStartBuf   bytes.Buffer
	zmodemActive     bool
	zmodemFinishing  bool
	zmodemDraining   bool
	zmodemFinish     *time.Timer
}

var _ proxy.UserConnection = (*tuiAssetConnection)(nil)

func (c *tuiAssetConnection) ID() string         { return c.id }
func (c *tuiAssetConnection) LoginFrom() string  { return "ST" }
func (c *tuiAssetConnection) RemoteAddr() string { return c.remoteAddr }
func (c *tuiAssetConnection) Pty() ssh.Pty {
	return ssh.Pty{Term: "xterm-256color", Window: c.Window()}
}
func (c *tuiAssetConnection) HandleRoomEvent(event string, msg *exchange.RoomMessage) {
	if event != exchange.ActionEvent {
		return
	}
	switch string(msg.Body) {
	case exchange.ZmodemStartEvent:
		c.startZmodemPassthrough()
	case exchange.ZmodemEndEvent:
		c.finishZmodemPassthroughAfterOutput(false)
	case exchange.ZmodemAbortEvent:
		c.finishZmodemPassthroughAfterOutput(true)
	}
}

const (
	zmodemStartFrameLimit     = 64
	zmodemFinishFallbackDelay = 250 * time.Millisecond
	zmodemAbortDrainDelay     = 3250 * time.Millisecond
)

var zmodemHexHeaderPrefix = []byte{0x2a, 0x2a, 0x18, 0x42}

func (c *tuiAssetConnection) startZmodemPassthrough() {
	c.zmodemMu.Lock()
	defer c.zmodemMu.Unlock()
	if c.zmodemActive || c.screen == nil {
		return
	}
	if c.screen.BeginPassthrough(c.Terminal, c.Terminal.SendRawInput) {
		c.zmodemActive = true
		c.zmodemFinishing = false
		c.zmodemDraining = false
	}
}

func (c *tuiAssetConnection) finishZmodemPassthroughAfterOutput(draining bool) {
	c.zmodemMu.Lock()
	defer c.zmodemMu.Unlock()
	if !c.zmodemActive || c.zmodemFinishing {
		return
	}
	c.zmodemFinishing = true
	c.zmodemDraining = draining
	delay := zmodemFinishFallbackDelay
	if draining {
		delay = zmodemAbortDrainDelay
	}
	c.zmodemFinish = time.AfterFunc(delay, func() {
		c.endZmodemPassthrough(!draining)
	})
}

func (c *tuiAssetConnection) endZmodemPassthrough(redrawPrompt bool) {
	c.zmodemMu.Lock()
	if !c.zmodemActive {
		c.zmodemMu.Unlock()
		return
	}
	c.zmodemActive = false
	c.zmodemFinishing = false
	c.zmodemDraining = false
	if c.zmodemFinish != nil {
		c.zmodemFinish.Stop()
		c.zmodemFinish = nil
	}
	c.zmodemStartBuf.Reset()
	c.screen.EndPassthrough(c.Terminal)
	c.zmodemMu.Unlock()
	if redrawPrompt {
		c.Terminal.SendInput([]byte{'\r'})
	}
	if c.onPassthroughEnd != nil {
		c.onPassthroughEnd()
	}
}

func (c *tuiAssetConnection) Write(p []byte) (int, error) {
	c.zmodemMu.Lock()
	if c.zmodemActive {
		data := p
		if c.zmodemStartBuf.Len() > 0 {
			data = make([]byte, 0, c.zmodemStartBuf.Len()+len(p))
			data = append(data, c.zmodemStartBuf.Bytes()...)
			data = append(data, p...)
			c.zmodemStartBuf.Reset()
		}
		_, err := c.screen.WritePassthrough(c.Terminal, data)
		finishing := c.zmodemFinishing && !c.zmodemDraining
		c.zmodemMu.Unlock()
		if finishing {
			c.endZmodemPassthrough(true)
		}
		return len(p), err
	}

	visible := c.bufferZmodemStart(p)
	c.zmodemMu.Unlock()
	if len(visible) == 0 {
		return len(p), nil
	}
	_, err := c.Terminal.Write(visible)
	return len(p), err
}

func (c *tuiAssetConnection) bufferZmodemStart(p []byte) []byte {
	data := p
	if c.zmodemStartBuf.Len() > 0 {
		c.zmodemStartBuf.Write(p)
		data = bytes.Clone(c.zmodemStartBuf.Bytes())
		c.zmodemStartBuf.Reset()
	}
	if start := bytes.Index(data, zmodemHexHeaderPrefix); start >= 0 {
		header := data[start:]
		if bytes.IndexAny(header, "\r\n") < 0 && len(header) <= zmodemStartFrameLimit {
			c.zmodemStartBuf.Write(header)
			return bytes.Clone(data[:start])
		}
		return data
	}
	for size := min(len(data), len(zmodemHexHeaderPrefix)-1); size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], zmodemHexHeaderPrefix[:size]) {
			c.zmodemStartBuf.Write(data[len(data)-size:])
			return bytes.Clone(data[:len(data)-size])
		}
	}
	return data
}

func (c *tuiAssetConnection) Close() error {
	c.endZmodemPassthrough(false)
	return c.Terminal.Close()
}

const (
	maxTUISessions               = 9
	maxTUIManualPasswordAttempts = 3
)

func isTUIVirtualAccount(account model.PermAccount) bool {
	return account.Username == model.InputUser || account.Username == model.DynamicUser
}

func sortTUIAccounts(accounts []model.PermAccount) {
	slices.SortStableFunc(accounts, func(a, b model.PermAccount) int {
		if aVirtual, bVirtual := isTUIVirtualAccount(a), isTUIVirtualAccount(b); aVirtual != bVirtual {
			if aVirtual {
				return 1
			}
			return -1
		}
		if result := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); result != 0 {
			return result
		}
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})
}

func tuiAssetPreferenceKey(asset model.PermAsset) string {
	return asset.OrgID + "\x00" + asset.ID
}

func tuiAccountPreferenceKey(account model.PermAccount) string {
	if account.Alias != "" {
		return account.Alias
	}
	return account.Username + "\x00" + account.Name
}

func tuiPasswordAttemptKey(asset model.PermAsset, account model.PermAccount, protocol string) string {
	return tuiAssetPreferenceKey(asset) + "\x00" + tuiAccountPreferenceKey(account) + "\x00" + protocol
}

func (h *terminalUI) rememberConnection(asset model.PermAsset, account model.PermAccount, protocol string) {
	if h.preferences == nil || h.user == nil {
		return
	}
	h.preferences.storeConnection(h.user.ID, tuiAssetPreferenceKey(asset), tuiConnectionPreference{
		Account: tuiAccountPreferenceKey(account), Protocol: protocol,
	})
}

func (h *terminalUI) preferredConnectionOptions(asset model.PermAsset, accounts []model.PermAccount, protocols []string) (int, int) {
	if h.preferences == nil || h.user == nil {
		return 0, -1
	}
	preference, ok := h.preferences.connection(h.user.ID, tuiAssetPreferenceKey(asset))
	if !ok {
		return 0, -1
	}
	accountOption := -1
	for i := range accounts {
		if tuiAccountPreferenceKey(accounts[i]) == preference.Account {
			accountOption = i
			break
		}
	}
	if accountOption < 0 {
		return 0, -1
	}
	for i := range protocols {
		if protocols[i] == preference.Protocol {
			return accountOption, i
		}
	}
	return accountOption, -1
}

type tuiSession struct {
	terminal           *tui.Terminal
	controls           []tview.Primitive
	duplicate          func()
	name, page         string
	done, closing      bool
	closePromptPending bool
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
	h.rememberConnection(asset, account, protocol)
	terminal, err := tui.NewTerminal(h.ctx, func() { h.dirty.Store(true) })
	if err == nil {
		err = terminal.SetPalette(tui.ThemePaletteForProfile(h.lightTheme, h.accentColor, h.colorProfile))
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
	conn := &tuiAssetConnection{
		Terminal: terminal, id: session.page, remoteAddr: remoteAddr, screen: h.themeScreen,
		onPassthroughEnd: func() { h.themeScreen.RequestSync(); h.dirty.Store(true) },
	}
	api := h.data.client(asset.OrgID)
	lang := h.data.lang
	passwordAttemptKey := tuiPasswordAttemptKey(asset, account, protocol)
	passwordLimitError := fmt.Errorf(h.tr(
		"手动密码最多允许输入 %d 次",
		"Manual password can be entered at most %d times",
	), maxTUIManualPasswordAttempts)
	// One goroutine per session; at most eight, including pending approvals and
	// closing connections. Selecting another tab does not cancel this context.
	go func() {
		closeOnFinish := false
		if err := srvconn.IsSupportedProtocol(protocol); err != nil {
			_, _ = fmt.Fprintf(terminal, "\r\n%s\r\n", err)
		} else {
			closeOnFinish = connectSelectedAsset(conn, api, h.user, asset, account, protocol, lang, func() error {
				if h.manualPasswordAttempts.acquire(passwordAttemptKey) {
					return nil
				}
				return passwordLimitError
			})
		}
		_ = terminal.Close()
		h.update(func() {
			h.finishSession(session, title, closeOnFinish)
		})
	}()
}

func (h *terminalUI) finishSession(session *tuiSession, title string, closeOnFinish bool) {
	session.done = true
	if h.popup == session.terminal {
		h.setFullscreen(false)
	}
	if session.closing {
		h.removeSession(session)
		return
	}
	session.closePromptPending = closeOnFinish
	session.terminal.SetTitle(" " + title + " · " + h.tr("连接已结束", "Session ended") + " ")
	h.refreshSessionTabs()
	h.confirmCloseFinishedTab(session)
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
		session.controls = append(session.controls, tui.NewButton("").SetStyle(style).SetActivatedStyle(style.Foreground(tui.Foreground)).SetDisabledStyle(style.Dim(true)).SetSelectedFunc(action))
	}
}

func (h *terminalUI) drawSessionControls(screen tcell.Screen) {
	if h.activeSession < 0 {
		return
	}
	session := h.sessions[h.activeSession]
	controls := session.controls
	if h.fullscreen {
		controls = controls[2:]
	}
	x, y, w, _ := session.terminal.GetRect()
	right := x + w - 3
	tview.Print(screen, " ", right, y, 1, tview.AlignLeft, tui.Muted)
	for i := len(controls) - 1; i >= 0; i-- {
		button := controls[i].(*tui.Button)
		width := min(tview.TaggedStringWidth(button.GetLabel()), max(1, (w-5-3*(len(controls)-1))/len(controls)))
		right -= width
		button.SetRect(right, y, width, 1)
		button.Draw(screen)
		if i > 0 {
			right -= 3
			tview.Print(screen, " · ", right, y, 3, tview.AlignLeft, tui.Muted)
		}
	}
	if right > x {
		tview.Print(screen, " ", right-1, y, 1, tview.AlignLeft, tui.Muted)
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
	h.sessionTabs.SetCell(0, 0, tui.NewTableCell(h.sessionTabLabel(-1)).SetTextColor(tui.Muted).SetClickedFunc(func() bool { h.activateSession(-1); return true }))
	for i := range h.sessions {
		h.sessionTabs.SetCell(0, i+1, tui.NewTableCell(h.sessionTabLabel(i)).SetMaxWidth(36).SetTextColor(tui.Muted).SetClickedFunc(func() bool { h.activateSession(i); return true }))
	}
	h.sessionTabs.Select(0, selected)
}

func (h *terminalUI) drawSessionTabs(screen tcell.Screen) {
	h.drawCharmTabs(screen)
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
	available, used := w-4, 0
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
	h.sessionTabs.SetBorderPadding(0, 0, 1, padding)
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
	label := "▦ " + h.tr("资产面板", "Asset panel")
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
	list := tui.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).
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
	tuiDialogBorder(list.Box, h.tr("窗口栏", "Window bar"))
	h.openDialog("sessions", &tuiOverlay{Box: tview.NewBox(), child: list, width: 54, height: list.GetItemCount() + 4, anchor: h.sessionTabs}, []tview.Primitive{list})
}

func (h *terminalUI) showUserMenu() {
	h.closeDropdown()
	list := tui.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).
		SetMainTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).SetSelectedStyle(tui.Selected)
	list.AddItem(h.tr("退出", "Quit"), "", 0, h.quit)
	tuiDialogBorder(list.Box, "")
	list.SetBorderPadding(0, 0, 2, 2)
	width := max(18, min(36, tview.TaggedStringWidth(h.identity.GetLabel())+2))
	h.openDialog("user", &tuiOverlay{Box: tview.NewBox(), child: list, width: width, height: 3, anchor: h.identity}, []tview.Primitive{list})
}

// Fullscreen keeps the session border and reserves one bottom row for shortcuts.
// Its exit button occupies the border, leaving remote output unobstructed.
func (h *terminalUI) setFullscreen(enabled bool) {
	if h.fullscreen == enabled || enabled && (h.popup == nil || h.modal) {
		return
	}
	h.fullscreen = enabled
	if enabled {
		h.footer.SetBorderPadding(0, 0, 1, 1)
		root := tui.NewFlex().SetDirection(tview.FlexRow).AddItem(h.popup, 0, 1, true).AddItem(h.footer, 1, 0, false)
		root.SetBackgroundColor(tui.Background)
		root.SetMouseCapture(h.captureRootMouse)
		h.app.SetRoot(root, true)
	} else {
		h.footer.SetBorderPadding(0, 0, 1, 1)
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
	if index >= 0 {
		h.confirmCloseFinishedTab(h.sessions[index])
	}
}

func (h *terminalUI) confirmCloseFinishedTab(session *tuiSession) {
	if !session.closePromptPending || h.modal || h.popup != session.terminal {
		return
	}
	session.closePromptPending = false
	message := fmt.Sprintf(h.tr("会话“%s”已结束，是否关闭此标签页？", "Session \"%s\" has ended. Close this tab?"), cleanTUIText(session.name))
	dialog := tui.NewModal().SetText(message).
		AddButtons([]string{h.tr("保留标签页", "Keep tab"), h.tr("关闭标签页", "Close tab")}).
		SetDoneFunc(func(index int, _ string) {
			h.dismissModal()
			if index == 1 {
				h.removeSession(session)
			}
		})
	dialog.SetBackgroundColor(tui.Panel).SetTextColor(tui.Foreground).
		SetButtonStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Raised)).SetButtonActivatedStyle(tui.Selected).
		SetBorderColor(tui.FocusBorder)
	dialog.Box.SetBackgroundColor(tui.Panel)
	dialog.SetTitle(" " + h.tr("连接已结束", "Session ended") + " ").SetTitleAlign(tview.AlignLeft).SetTitleColor(tui.Accent)
	tui.RoundedBorder(dialog.Box)
	h.openDialog("session-ended", dialog, nil)
}

func (h *terminalUI) closeSession(session *tuiSession) {
	if !session.done {
		h.confirmDisconnect(session)
		return
	}
	h.disconnectSession(session)
}

func (h *terminalUI) confirmDisconnect(session *tuiSession) {
	message := fmt.Sprintf(h.tr("确定断开会话“%s”？", "Disconnect session \"%s\"?"), cleanTUIText(session.name))
	dialog := tui.NewModal().SetText(message).
		AddButtons([]string{h.tr("返回", "Back"), h.tr("断开", "Disconnect")}).
		SetDoneFunc(func(index int, _ string) {
			h.dismissModal()
			if index == 1 {
				h.disconnectSession(session)
			}
		})
	dialog.SetBackgroundColor(tui.Panel).SetTextColor(tui.Foreground).
		SetButtonStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Raised)).SetButtonActivatedStyle(tui.Selected).
		SetBorderColor(tui.FocusBorder)
	dialog.Box.SetBackgroundColor(tui.Panel)
	dialog.SetTitle(" " + h.tr("断开确认", "Confirm disconnect") + " ").SetTitleAlign(tview.AlignLeft).SetTitleColor(tui.Accent)
	tui.RoundedBorder(dialog.Box)
	h.openDialog("disconnect", dialog, nil)
}

func (h *terminalUI) disconnectSession(session *tuiSession) {
	session.closing = true
	session.terminal.Dispose()
	if h.popup == session.terminal {
		h.activateSession(h.previousSession(h.activeSession))
	}
	if session.done {
		h.removeSession(session)
	} else {
		h.refreshSessionTabs()
	}
}

func (h *terminalUI) previousSession(index int) int {
	for index--; index >= 0; index-- {
		if !h.sessions[index].closing {
			return index
		}
	}
	return -1
}

func (h *terminalUI) removeSession(session *tuiSession) {
	active := (*tuiSession)(nil)
	if h.activeSession >= 0 && h.activeSession < len(h.sessions) {
		active = h.sessions[h.activeSession]
	}
	previous := -1
	for i, s := range h.sessions {
		if s == session {
			previous = h.previousSession(i)
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
		h.activateSession(previous)
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
	message := h.tr("确定退出本次 SSH 会话吗？", "Are you sure you want to quit this SSH session?")
	if len(h.sessions) > 0 {
		message += "\n\n" + h.tr("退出后，所有已连接的资产会话都将断开。", "All connected asset sessions will be disconnected.")
	}
	dialog := tui.NewModal().SetText(message).
		AddButtons([]string{h.tr("取消", "Cancel"), h.tr("退出会话", "Quit session")}).
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
	dialog.SetTitle(" " + h.tr("退出 SSH 会话", "Quit SSH session") + " ").SetTitleAlign(tview.AlignLeft).SetTitleColor(tui.Accent)
	tui.RoundedBorder(dialog.Box)
	h.openDialog("quit", dialog, nil)
}

// tview handles vertical wheel/PageUp/PageDown and keyboard horizontal scrolling.
// Add horizontal wheel gestures too; row selection is unchanged while scrolling.
func enableTableScroll(table *tui.Table) {
	previousCapture := table.GetMouseCapture()
	table.SetMouseCapture(func(a tview.MouseAction, e *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if previousCapture != nil {
			a, e = previousCapture(a, e)
			if e == nil {
				return a, nil
			}
		}
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
