package handler

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/mattn/go-runewidth"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

const tuiSearchMaxLength = 256
const tuiDoubleClickInterval = 500 * time.Millisecond
const tuiStatusPrefix = "▶ "

const (
	tuiTerminalMaxWidth  = 500
	tuiTerminalMaxHeight = 1000
)

const (
	tuiCursorMarkerPrefix = "\x1b]99;koko-cursor;"
	tuiCursorMarkerEnd    = "\x07"
	tuiEnterAltScreen     = "\x1b[?1049h"
	tuiExitAltScreen      = "\x1b[?1049l"
	tuiResetViewport      = "\x1b[?6l\x1b[r\x1b[H"
	tuiShowCursor         = "\x1b[?25h"
	tuiHideCursor         = "\x1b[?25l"
	tuiCursorBlinkRestore = "\x1b[?12h\x1b[0 q"
)

const (
	tuiSelectedStyle         = "\x1b[7m"
	tuiBoldStyle             = "\x1b[1m"
	tuiSelectedProtocolStyle = tuiSelectedStyle
	tuiStyleReset            = "\x1b[0m"
)

type assetPageMsg struct {
	assets      []model.PermAsset
	connectable []bool
	offset      int
	total       int
	hasPrev     bool
	hasNext     bool
	pageSize    int
	refresh     bool
	err         error
}

type assetChoicesMsg struct {
	asset     model.PermAsset
	accounts  []model.PermAccount
	protocols []string
	err       error
}

type assetUnavailableMsg struct {
	message string
}

type assetTUIDialog struct {
	asset             model.PermAsset
	accounts          []model.PermAccount
	protocols         []string
	accountSearch     []rune
	accountMatches    []int
	searchingAccount  bool
	accountIndex      int
	accountScroll     int
	protocolIndex     int
	scrollbarDragging bool
	scrollbarGrab     int
}

type assetTUILanguageDialog struct {
	index        int
	lastClickRow int
	lastClickAt  time.Time
}

type assetTUIHelpRow struct {
	key         string
	description string
	separator   bool
	fullWidth   string
	divider     bool
}

type assetTUIConnection struct {
	asset       model.PermAsset
	account     model.PermAccount
	protocol    string
	multiWindow bool
}

type assetTUI struct {
	handler  *InteractiveHandler
	selector *UserSelectHandler

	width    int
	height   int
	pageSize int
	offset   int
	total    int
	cursor   int

	assets             []model.PermAsset
	connectable        []bool
	hasPrev            bool
	hasNext            bool
	query              string
	searchInput        []rune
	searching          bool
	loading            bool
	loadingChoices     bool
	status             string
	switchText         bool
	persistTextMode    bool
	resume             bool
	dialog             *assetTUIDialog
	languageDialog     *assetTUILanguageDialog
	helpDialog         bool
	helpScroll         int
	helpDragging       bool
	helpScrollbarGrab  int
	quitDialog         bool
	detailDialog       *assetTUIDetailDialog
	treeDialog         *assetTUITreeDialog
	treeDialogStates   map[assetTUITreeKind]assetTUITreeDialog
	lastTreeDialog     assetTUITreeKind
	trees              map[assetTUITreeKind]*assetTUITreeCache
	selectedTree       assetTUITreeKind
	selectedTreeID     string
	selectedPath       string
	connection         *assetTUIConnection
	pendingMultiWindow bool
	multiSessionCount  int
	unfinishedSessions int
	showMultiSessions  bool
	lastClickRow       int
	lastClickAt        time.Time
}

func newAssetTUI(handler *InteractiveHandler) *assetTUI {
	width, height := handler.GetPtySize()
	model := &assetTUI{
		handler: handler, selector: handler.selectHandler,
		width: width, height: height,
		trees:            make(map[assetTUITreeKind]*assetTUITreeCache),
		treeDialogStates: make(map[assetTUITreeKind]assetTUITreeDialog),
	}
	model.pageSize = model.assetRows()
	return model
}

func (m *assetTUI) Init() tea.Cmd {
	if m.resume {
		m.resume = false
		return nil
	}
	return m.loadPage(m.offset)
}

func (m *assetTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		width, height := assetTUITerminalSize(msg.Width, msg.Height)
		oldPageSize := m.pageSize
		m.width, m.height = width, height
		m.pageSize = m.assetRows()
		if oldPageSize != m.pageSize && !m.loading {
			offset := m.offset
			if !m.hasPagination() {
				offset = 0
			}
			return m, m.loadPage(offset)
		}
	case assetPageMsg:
		m.loading = false
		if msg.err != nil {
			if !msg.refresh {
				m.assets, m.connectable = nil, nil
				m.total, m.hasPrev, m.hasNext = 0, false, false
			}
			return m, m.showStatus(userFacingErrorMessage(m.tr("资产加载失败", "Failed to load assets"), msg.err))
		}
		m.assets = msg.assets
		m.connectable = msg.connectable
		m.offset, m.total = msg.offset, msg.total
		m.hasPrev, m.hasNext = msg.hasPrev, msg.hasNext
		if msg.refresh {
			m.cursor = min(m.cursor, max(0, len(m.assets)-1))
		} else {
			m.cursor = 0
		}
		desiredPageSize := m.assetRows()
		if msg.pageSize != desiredPageSize {
			m.pageSize = desiredPageSize
			offset := m.offset
			if !m.hasPagination() {
				offset = 0
			}
			return m, m.loadPage(offset)
		}
		if len(m.assets) == 0 {
			return m, m.showStatus(m.tr("没有匹配的资产", "No matching assets"))
		} else {
			m.clearStatus()
		}
	case assetChoicesMsg:
		m.loadingChoices = false
		if msg.err != nil {
			return m, m.showStatus(userFacingErrorMessage(m.tr("无法获取连接选项", "Failed to load connection options"), msg.err))
		}
		m.clearStatus()
		m.prioritizeConnectionChoices(msg.asset, msg.accounts, msg.protocols)
		if len(msg.accounts) == 1 && len(msg.protocols) == 1 {
			return m.selectConnection(msg.asset, msg.accounts[0], msg.protocols[0])
		}
		m.dialog = newAssetTUIDialog(msg.asset, msg.accounts, msg.protocols)
	case assetTUIAssetDetailMsg:
		if m.detailDialog == nil || m.detailDialog.assetID != msg.asset.ID {
			return m, nil
		}
		if msg.err != nil {
			m.detailDialog.loading = false
			return m, m.showStatus(userFacingErrorMessage(m.tr("资产详情加载失败", "Failed to load asset details"), msg.err))
		}
		m.clearStatus()
		m.detailDialog = m.assetDetailDialog(msg.asset, msg.protocols)
	case assetUnavailableMsg:
		m.loadingChoices = false
		return m, m.showStatus(msg.message)
	case assetTUITreeNodesMsg:
		return m, m.updateTreeNodes(msg)
	case assetTUITreeCountsMsg:
		m.updateTreeCounts(msg)
	case tea.MouseMsg:
		return m.updateMouse(tea.MouseEvent(msg))
	case tea.KeyMsg:
		if m.quitDialog {
			return m.updateQuitDialogKey(msg)
		}
		if m.detailDialog != nil {
			return m.updateDetailDialogKey(msg)
		}
		if m.helpDialog {
			return m.updateHelpDialogKey(msg)
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '?' &&
			!m.searching && (m.dialog == nil || !m.dialog.searchingAccount) {
			m.helpDialog = true
			m.helpScroll = 0
			return m, nil
		}
		if m.treeDialog != nil {
			return m.updateTreeDialogKey(msg)
		}
		if m.languageDialog != nil {
			return m.updateLanguageDialogKey(msg)
		}
		if m.dialog != nil {
			return m.updateDialogKey(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		if m.loading || m.loadingChoices {
			if msg.Type == tea.KeyCtrlC {
				m.quitDialog = true
				return m, nil
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				switch msg.Runes[0] {
				case 'w':
					if m.multiSessionCount > 0 {
						m.showMultiSessions = true
						return m, tea.Quit
					}
				case 'g', 'G':
					if !m.loadingChoices {
						return m.reopenTreeDialog()
					}
				case 's', 'S':
					if !m.loadingChoices {
						m.openLanguageDialog()
					}
				case 'v', 'V':
					return m.toggleMouseMode()
				case 't', 'T':
					m.switchText = true
					m.persistTextMode = true
					return m, tea.Quit
				case 'q', 'Q':
					m.quitDialog = true
					return m, nil
				}
			}
			return m, nil
		}
		if tuiIsSpaceKey(msg) {
			return m, m.openAssetDetail()
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			m.quitDialog = true
			return m, nil
		case tea.KeyUp:
			m.moveAssetCursor(-1)
		case tea.KeyDown:
			m.moveAssetCursor(1)
		case tea.KeyHome:
			m.cursor = 0
		case tea.KeyEnd:
			if len(m.assets) > 0 {
				m.cursor = len(m.assets) - 1
			}
		case tea.KeyLeft, tea.KeyPgUp:
			if m.canGoPreviousPage() {
				return m, m.loadPage(max(0, m.offset-m.pageSize))
			}
		case tea.KeyRight, tea.KeyPgDown:
			if m.canGoNextPage() {
				return m, m.loadPage(m.offset + len(m.assets))
			}
		case tea.KeyEnter:
			return m.openAssetDialog(m.cursor, true)
		case tea.KeyRunes:
			if len(msg.Runes) != 1 {
				return m, nil
			}
			switch msg.Runes[0] {
			case 'w':
				if m.multiSessionCount > 0 {
					m.showMultiSessions = true
					return m, tea.Quit
				}
			case '/':
				m.focusSearch()
				return m, nil
			case 'x', 'X':
				if m.query != "" {
					m.query = ""
					m.searchInput = nil
					return m, m.loadPage(0)
				}
			case 'r', 'R':
				return m, m.refreshPage()
			case 's', 'S':
				m.openLanguageDialog()
			case 'v', 'V':
				return m.toggleMouseMode()
			case 'g', 'G':
				return m.reopenTreeDialog()
			case 'c', 'C':
				return m.openAssetDialog(m.cursor, false)
			case 'd', 'D':
				if m.selectedTree != 0 {
					return m.clearTreeSelection()
				}
			case 'k':
				m.moveAssetCursor(-1)
			case 'j':
				m.moveAssetCursor(1)
			case 'h':
				if m.canGoPreviousPage() {
					return m, m.loadPage(max(0, m.offset-m.pageSize))
				}
			case 'l':
				if m.canGoNextPage() {
					return m, m.loadPage(m.offset + len(m.assets))
				}
			case 't', 'T':
				m.switchText = true
				m.persistTextMode = true
				return m, tea.Quit
			case 'q', 'Q':
				m.quitDialog = true
				return m, nil
			}
		}
	}
	return m, nil
}

func (m *assetTUI) toggleMouseMode() (tea.Model, tea.Cmd) {
	mouseCmd := tea.Cmd(tea.DisableMouse)
	if m.handler.mouseMode == terminalMouseModeClient {
		m.handler.mouseMode = terminalMouseModeKoko
		mouseCmd = tea.EnableMouseCellMotion
	} else {
		m.handler.mouseMode = terminalMouseModeClient
	}
	return m, mouseCmd
}

func (m *assetTUI) moveAssetCursor(delta int) {
	m.cursor = tuiBoundedSelection(m.cursor, delta, len(m.assets))
}

func tuiBoundedSelection(index, delta, total int) int {
	if total <= 0 {
		return 0
	}
	index = max(0, min(index, total-1))
	return max(0, min(index+delta, total-1))
}

func (m *assetTUI) canGoPreviousPage() bool {
	return m.hasPrev || m.offset > 0
}

func (m *assetTUI) canGoNextPage() bool {
	return m.hasNext || m.total > 0 && m.offset+len(m.assets) < m.total
}

func (m *assetTUI) clearTreeSelection() (tea.Model, tea.Cmd) {
	m.selector.SetSelectType(TypeAsset)
	m.selector.selectedNode = model.Node{}
	m.selector.selectedType = classicTypeNode{}
	m.selector.selectedFavorite = classicFavoriteNode{}
	m.selector.selectedPath = ""
	m.handler.treeOrigin = 0
	m.handler.treeSelected = false
	m.selectedTree = 0
	m.selectedTreeID = ""
	m.selectedPath = ""
	return m, m.loadPage(0)
}

func (m *assetTUI) showStatus(message string) tea.Cmd {
	m.status = message
	return nil
}

func (m *assetTUI) clearStatus() {
	m.status = ""
}

func (m *assetTUI) focusSearch() {
	m.searching = true
	m.searchInput = []rune(m.query)
	m.clearStatus()
}

func (m *assetTUI) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searching = false
		m.searchInput = nil
		m.clearStatus()
	case tea.KeyEnter:
		m.searching = false
		m.query = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, string(m.searchInput)))
		m.searchInput = nil
		return m, m.loadPage(0)
	case tea.KeyBackspace, tea.KeyDelete, tea.KeyCtrlH:
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
	case tea.KeyCtrlU:
		m.searchInput = nil
	case tea.KeyRunes:
		remaining := tuiSearchMaxLength - len(m.searchInput)
		if remaining > 0 {
			runes := msg.Runes
			if len(runes) > remaining {
				runes = runes[:remaining]
			}
			m.searchInput = append(m.searchInput, runes...)
		}
	}
	return m, nil
}

func (m *assetTUI) updateQuitDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.quitDialog = false
	case tea.KeyEnter:
		return m, tea.Quit
	}
	return m, nil
}

func (m *assetTUI) loadPage(offset int) tea.Cmd {
	return m.loadPageWithRefresh(offset, false)
}

func (m *assetTUI) refreshPage() tea.Cmd {
	return m.loadPageWithRefresh(m.offset, true)
}

func (m *assetTUI) loadPageWithRefresh(offset int, refresh bool) tea.Cmd {
	m.loading = true
	status := m.tr("正在加载资产…", "Loading assets…")
	if refresh {
		status = m.tr("正在刷新资产…", "Refreshing assets…")
	}
	statusCmd := m.showStatus(status)
	pageSize := m.pageSize
	query := m.query
	selector := m.selector
	loadCmd := func() tea.Msg {
		if refresh && selector.loadingPolicy == loadingFromLocal {
			assets, err := selector.h.jmsService.GetAllUserPermsAssets(selector.user.ID)
			if err != nil {
				return assetPageMsg{pageSize: pageSize, refresh: true, err: err}
			}
			selector.SetAllLocalData(assets)
		}
		var searches []string
		if query != "" {
			searches = []string{query}
		}
		assets := selector.Retrieve(pageSize, offset, searches...)
		connectable := append([]bool(nil), selector.connectable...)
		return assetPageMsg{
			assets: append([]model.PermAsset(nil), assets...), connectable: connectable,
			offset: offset, total: selector.TotalCount(), hasPrev: selector.HasPrev(),
			hasNext: selector.HasNext(), pageSize: pageSize, refresh: refresh, err: selector.loadErr,
		}
	}
	return tea.Batch(loadCmd, statusCmd)
}

func (m *assetTUI) openAssetDialog(index int, multiWindow bool) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.assets) || m.loadingChoices {
		return m, nil
	}
	m.pendingMultiWindow = multiWindow
	if index >= len(m.connectable) || !m.connectable[index] {
		m.loadingChoices = true
		asset := m.assets[index]
		selector := m.selector
		statusCmd := m.showStatus(i18n.NewLang(m.handler.i18nLang).T("Cannot connect to this asset."))
		reasonCmd := func() tea.Msg {
			return assetUnavailableMsg{message: selector.unavailableAssetMessage(asset)}
		}
		return m, tea.Batch(reasonCmd, statusCmd)
	}
	m.loadingChoices = true
	statusCmd := m.showStatus(m.tr("正在加载账号和协议…", "Loading accounts and protocols…"))
	asset := m.assets[index]
	selector := m.selector
	mismatch := m.tr("资产信息不匹配", "Asset details do not match the selected asset")
	noChoice := m.tr("没有可用的授权账号或终端协议", "No permitted accounts or terminal protocols")
	loadCmd := func() tea.Msg {
		client := selector.h.assetClient(asset.OrgID)
		detail, err := client.GetUserPermAssetDetailById(selector.user.ID, asset.ID)
		if err != nil {
			return assetChoicesMsg{err: err}
		}
		if detail.ID != asset.ID || detail.OrgID != "" && asset.OrgID != "" &&
			asset.OrgID != globalOrganizationID && detail.OrgID != asset.OrgID {
			return assetChoicesMsg{err: fmt.Errorf("%s", mismatch)}
		}
		if detail.OrgID != "" {
			asset.OrgID = detail.OrgID
		}

		accounts := selector.filterValidAccount(detail.PermedAccounts)
		sort.Sort(model.PermAccountList(accounts))
		var protocols []string
		for _, supported := range srvconn.SupportedProtocols() {
			for _, permitted := range detail.PermedProtocols {
				if !strings.EqualFold(supported, permitted.Name) {
					continue
				}
				protocols = append(protocols, supported)
				break
			}
		}
		if len(accounts) == 0 || len(protocols) == 0 {
			return assetChoicesMsg{err: fmt.Errorf("%s", noChoice)}
		}
		return assetChoicesMsg{asset: asset, accounts: accounts, protocols: protocols}
	}
	return m, tea.Batch(loadCmd, statusCmd)
}

func newAssetTUIDialog(asset model.PermAsset, accounts []model.PermAccount, protocols []string) *assetTUIDialog {
	dialog := &assetTUIDialog{asset: asset, accounts: accounts, protocols: protocols}
	dialog.filterAccounts()
	return dialog
}

func (d *assetTUIDialog) filterAccounts() {
	query := strings.ToLower(strings.TrimSpace(string(d.accountSearch)))
	d.accountMatches = d.accountMatches[:0]
	for i := range d.accounts {
		account := d.accounts[i]
		text := strings.ToLower(account.Name + " " + account.Username)
		if query == "" || strings.Contains(text, query) {
			d.accountMatches = append(d.accountMatches, i)
		}
	}
	if len(d.accountMatches) == 0 {
		d.accountIndex = -1
	} else {
		d.accountIndex = max(0, min(d.accountIndex, len(d.accountMatches)-1))
	}
}

func (d *assetTUIDialog) selectedAccount() (model.PermAccount, bool) {
	if d.accountMatches == nil && len(d.accounts) > 0 {
		d.filterAccounts()
	}
	if d.accountIndex < 0 || d.accountIndex >= len(d.accountMatches) {
		return model.PermAccount{}, false
	}
	index := d.accountMatches[d.accountIndex]
	if index < 0 || index >= len(d.accounts) {
		return model.PermAccount{}, false
	}
	return d.accounts[index], true
}

func (d *assetTUIDialog) accountListStart(rows int) int {
	total := len(d.accountMatches)
	d.accountScroll = max(0, min(d.accountScroll, max(0, total-rows)))
	if d.accountIndex < 0 || rows <= 0 {
		return d.accountScroll
	}
	if d.accountIndex < d.accountScroll {
		d.accountScroll = d.accountIndex
	} else if d.accountIndex >= d.accountScroll+rows {
		d.accountScroll = d.accountIndex - rows + 1
	}
	return d.accountScroll
}

func (m *assetTUI) updateDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dialog := m.dialog
	if dialog.searchingAccount {
		return m.updateAccountSearch(msg)
	}
	if tuiIsSpaceKey(msg) {
		m.openAccountDetail()
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.dialog = nil
		m.clearStatus()
	case tea.KeyLeft:
		if dialog.protocolIndex > 0 {
			dialog.protocolIndex--
		}
	case tea.KeyRight:
		if dialog.protocolIndex+1 < len(dialog.protocols) {
			dialog.protocolIndex++
		}
	case tea.KeyUp:
		if dialog.accountIndex > 0 {
			dialog.accountIndex--
		}
	case tea.KeyDown:
		if dialog.accountIndex+1 < len(dialog.accountMatches) {
			dialog.accountIndex++
		}
	case tea.KeyEnter:
		return m.chooseDialogConnection()
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return m, nil
		}
		switch msg.Runes[0] {
		case '/':
			dialog.searchingAccount = true
		case 'h':
			if dialog.protocolIndex > 0 {
				dialog.protocolIndex--
			}
		case 'l':
			if dialog.protocolIndex+1 < len(dialog.protocols) {
				dialog.protocolIndex++
			}
		case 'k':
			if dialog.accountIndex > 0 {
				dialog.accountIndex--
			}
		case 'j':
			if dialog.accountIndex+1 < len(dialog.accountMatches) {
				dialog.accountIndex++
			}
		}
	}
	return m, nil
}

func (m *assetTUI) updateAccountSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dialog := m.dialog
	switch msg.Type {
	case tea.KeyEsc, tea.KeyEnter:
		dialog.searchingAccount = false
	case tea.KeyBackspace, tea.KeyDelete, tea.KeyCtrlH:
		if len(dialog.accountSearch) > 0 {
			dialog.accountSearch = dialog.accountSearch[:len(dialog.accountSearch)-1]
			dialog.accountIndex = 0
			dialog.filterAccounts()
		}
	case tea.KeyCtrlU:
		dialog.accountSearch = nil
		dialog.accountIndex = 0
		dialog.filterAccounts()
	case tea.KeyRunes:
		remaining := tuiSearchMaxLength - len(dialog.accountSearch)
		if remaining > 0 {
			runes := msg.Runes
			if len(runes) > remaining {
				runes = runes[:remaining]
			}
			for _, r := range runes {
				if unicode.IsControl(r) {
					r = ' '
				}
				dialog.accountSearch = append(dialog.accountSearch, r)
			}
			dialog.accountIndex = 0
			dialog.filterAccounts()
		}
	}
	return m, nil
}

func (m *assetTUI) chooseDialogConnection() (tea.Model, tea.Cmd) {
	dialog := m.dialog
	if dialog == nil || dialog.protocolIndex < 0 || dialog.protocolIndex >= len(dialog.protocols) {
		return m, nil
	}
	account, ok := dialog.selectedAccount()
	if !ok {
		return m, nil
	}
	return m.selectConnection(dialog.asset, account, dialog.protocols[dialog.protocolIndex])
}

func (m *assetTUI) selectConnection(asset model.PermAsset, account model.PermAccount, protocol string) (tea.Model, tea.Cmd) {
	m.connection = &assetTUIConnection{
		asset: asset, account: account, protocol: protocol, multiWindow: m.pendingMultiWindow,
	}
	if m.handler != nil {
		m.handler.saveLastConnectionPreference(asset, account, protocol)
	}
	m.dialog = nil
	return m, tea.Quit
}

func (m *assetTUI) prioritizeConnectionChoices(asset model.PermAsset,
	accounts []model.PermAccount, protocols []string) {
	if m.handler == nil {
		return
	}
	recent := m.handler.loadRecentConnectionPreferences(asset)
	if len(recent) == 0 {
		return
	}
	prioritizeConnectionChoiceLists(recent, accounts, protocols)
}

func prioritizeConnectionChoiceLists(recent []terminalConnectionPreference,
	accounts []model.PermAccount, protocols []string) {
	for index, preference := range recent {
		if findPermAccount(accounts, preference.account()) >= 0 &&
			findProtocol(protocols, preference.Protocol) >= 0 {
			if index > 0 {
				copy(recent[1:index+1], recent[:index])
				recent[0] = preference
			}
			break
		}
	}
	accountTarget := 0
	protocolTarget := 0
	for _, preference := range recent {
		if index := findPermAccount(accounts[accountTarget:], preference.account()); index >= 0 {
			index += accountTarget
			account := accounts[index]
			copy(accounts[accountTarget+1:index+1], accounts[accountTarget:index])
			accounts[accountTarget] = account
			accountTarget++
		}
		if index := findProtocol(protocols[protocolTarget:], preference.Protocol); index >= 0 {
			index += protocolTarget
			protocol := protocols[index]
			copy(protocols[protocolTarget+1:index+1], protocols[protocolTarget:index])
			protocols[protocolTarget] = protocol
			protocolTarget++
		}
	}
}

func findPermAccount(accounts []model.PermAccount, expected model.PermAccount) int {
	for index := range accounts {
		if samePermAccount(accounts[index], expected) {
			return index
		}
	}
	return -1
}

func findProtocol(protocols []string, expected string) int {
	for index := range protocols {
		if strings.EqualFold(protocols[index], expected) {
			return index
		}
	}
	return -1
}

func samePermAccount(left, right model.PermAccount) bool {
	if left.Alias != "" && right.Alias != "" {
		return left.Alias == right.Alias
	}
	return left.Name == right.Name && left.Username == right.Username
}

func (m *assetTUI) openLanguageDialog() {
	current := i18n.NewLang(m.handler.i18nLang)
	index := 0
	for i, code := range i18n.AllCodes {
		if code == current {
			index = i
			break
		}
	}
	m.languageDialog = &assetTUILanguageDialog{index: index, lastClickRow: -1}
}

func (m *assetTUI) updateLanguageDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dialog := m.languageDialog
	dialog.lastClickRow = -1
	dialog.lastClickAt = time.Time{}
	switch msg.Type {
	case tea.KeyEsc:
		m.languageDialog = nil
	case tea.KeyUp:
		if dialog.index > 0 {
			dialog.index--
		}
	case tea.KeyDown:
		if dialog.index+1 < len(i18n.AllCodes) {
			dialog.index++
		}
	case tea.KeyHome:
		dialog.index = 0
	case tea.KeyEnd:
		dialog.index = len(i18n.AllCodes) - 1
	case tea.KeyEnter:
		return m.chooseLanguage()
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return m, nil
		}
		switch msg.Runes[0] {
		case 'k':
			if dialog.index > 0 {
				dialog.index--
			}
		case 'j':
			if dialog.index+1 < len(i18n.AllCodes) {
				dialog.index++
			}
		}
	}
	return m, nil
}

func (m *assetTUI) chooseLanguage() (tea.Model, tea.Cmd) {
	dialog := m.languageDialog
	if dialog == nil || dialog.index < 0 || dialog.index >= len(i18n.AllCodes) {
		return m, nil
	}
	language := i18n.AllCodes[dialog.index]
	m.languageDialog = nil
	if language.String() == m.handler.i18nLang {
		return m, nil
	}
	m.handler.i18nLang = language.String()
	if m.handler.jmsService != nil {
		setAPIClientLang(m.handler.jmsService, m.handler.i18nLang)
	}
	m.handler.saveTerminalLanguage(m.handler.i18nLang)
	return m, m.showStatus(language.T("Switch language successfully"))
}

func (m *assetTUI) updateMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	if m.quitDialog {
		return m, nil
	}
	if m.detailDialog != nil {
		return m.updateDetailDialogMouse(event)
	}
	if m.helpDialog {
		return m.updateHelpDialogMouse(event)
	}
	if m.treeDialog != nil {
		return m.updateTreeDialogMouse(event)
	}
	if m.languageDialog != nil {
		if event.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.updateLanguageDialogMouse(event)
	}
	if m.dialog != nil {
		return m.updateDialogMouse(event)
	}
	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if event.Button == tea.MouseButtonLeft && event.Y == 0 && event.X >= 0 && event.X < m.width {
		if !m.searching {
			m.focusSearch()
		}
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelUp {
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelDown {
		if m.cursor+1 < len(m.assets) {
			m.cursor++
		}
		return m, nil
	}
	if event.Button != tea.MouseButtonLeft || m.loading || m.loadingChoices ||
		event.X < 0 || event.X >= m.width || event.Y < 3 {
		return m, nil
	}
	row := event.Y - 3
	if row < 0 || row >= len(m.assets) || row >= m.visibleAssetRows() {
		return m, nil
	}
	m.cursor = row
	now := time.Now()
	if m.lastClickRow == row && now.Sub(m.lastClickAt) <= tuiDoubleClickInterval {
		m.lastClickRow = -1
		m.lastClickAt = time.Time{}
		return m.openAssetDialog(row, true)
	}
	m.lastClickRow = row
	m.lastClickAt = now
	return m, nil
}

func (m *assetTUI) View() string {
	lines := make([]string, max(1, m.height))
	for i := range lines {
		lines[i] = strings.Repeat(" ", m.width)
	}
	if m.height == 1 {
		lines[0] = m.footerLine()
		return m.viewWithCursor(lines)
	}
	lines[0] = m.searchLine()
	if m.height == 2 {
		lines[1] = m.footerLine()
		return m.viewWithCursor(lines)
	}
	if m.height == 3 {
		lines[1] = m.statusPageLine()
		lines[2] = m.footerLine()
		return m.viewWithCursor(lines)
	}
	lines[1] = strings.Repeat("─", m.width)
	if m.height == 4 {
		lines[2] = m.statusPageLine()
		lines[3] = m.footerLine()
		return m.viewWithCursor(lines)
	}

	columns := m.columns()
	lines[2] = tuiBoldStyle + m.renderColumns(columns, nil, -1) + tuiStyleReset
	rowCount := min(len(m.assets), m.visibleAssetRows())
	for i := 0; i < rowCount; i++ {
		row := m.renderColumns(columns, &m.assets[i], i)
		if i == m.cursor {
			row = tuiSelectedStyle + row + tuiStyleReset
		} else if i >= len(m.connectable) || !m.connectable[i] {
			row = "\x1b[2m" + row + tuiStyleReset
		}
		lines[i+3] = row
	}
	infoRow := rowCount + 3
	if m.hasPagination() && infoRow+2 < m.height {
		lines[infoRow] = strings.Repeat("─", m.width)
	}
	lines[m.height-2] = m.statusPageLine()
	lines[m.height-1] = m.footerLine()
	if m.dialog != nil {
		m.renderDialog(lines)
	}
	if m.languageDialog != nil {
		m.renderLanguageDialog(lines)
	}
	if m.treeDialog != nil {
		m.renderTreeDialog(lines)
	}
	if m.helpDialog {
		m.renderHelpDialog(lines)
	}
	if m.detailDialog != nil {
		m.renderDetailDialog(lines)
	}
	if m.quitDialog {
		m.renderQuitDialog(lines)
	}
	return m.viewWithCursor(lines)
}

func (m *assetTUI) viewWithCursor(lines []string) string {
	x, y, visible := m.terminalCursor()
	return fmt.Sprintf("%s%d;%d;%d%s", tuiCursorMarkerPrefix, boolInt(visible), x, y, tuiCursorMarkerEnd) +
		tuiResetViewport + strings.Join(lines, "\n")
}

func (m *assetTUI) terminalCursor() (x, y int, visible bool) {
	if m.quitDialog || m.detailDialog != nil || m.helpDialog || m.treeDialog != nil || m.languageDialog != nil {
		return 0, 0, false
	}
	if m.dialog != nil {
		geometry := m.dialogGeometry()
		if !m.dialog.searchingAccount || geometry.width < 36 || geometry.height < 11 ||
			geometry.y < 0 || geometry.y+geometry.height > max(1, m.height) {
			return 0, 0, false
		}
		_, cursor := tuiSearchField(m.tr("账号", "Account"), m.tr("搜索", "Search"),
			m.dialog.accountSearch, geometry.width-4)
		return geometry.x + 3 + cursor, geometry.y + 6, true
	}
	if !m.searching || m.height < 2 {
		return 0, 0, false
	}
	searchWidth, _ := m.topLineLayout()
	search := m.tr("搜索", "Search")
	_, cursor := tuiSearchField(m.searchLabel(), search, m.searchInput, searchWidth)
	return cursor, 0, true
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type assetTUIDialogGeometry struct {
	x, y, width, height int
	rows                int
}

func (m *assetTUI) dialogGeometry() assetTUIDialogGeometry {
	width := min(72, max(1, m.width-4))
	height := min(max(13, min(8, len(m.dialog.accounts))+10), max(1, m.height-2))
	return assetTUIDialogGeometry{
		x: (m.width - width) / 2, y: (m.height - height) / 2,
		width: width, height: height, rows: max(1, height-10),
	}
}

func (m *assetTUI) languageDialogGeometry() assetTUIDialogGeometry {
	width := min(52, max(1, m.width-8))
	height := min(len(i18n.AllCodes)+6, max(1, m.height-4))
	return assetTUIDialogGeometry{
		x: (m.width - width) / 2, y: (m.height - height) / 2,
		width: width, height: height, rows: max(1, height-6),
	}
}

func (m *assetTUI) quitDialogGeometry() assetTUIDialogGeometry {
	values := []string{
		m.tr("退出 Koko", "Quit Koko"),
		m.tr("确定退出本次 SSH 会话吗？", "Quit this SSH session?"),
		"enter:" + m.tr("确认", "Confirm") + " · esc:" + m.tr("取消", "Cancel"),
	}
	height := 7
	if m.unfinishedSessions > 0 {
		values = append(values, fmt.Sprintf(m.tr(
			"仍有 %d 个多会话未结束，退出后将一并结束。",
			"%d multi-sessions are still active; quitting will end them.",
		), m.unfinishedSessions))
		height++
	}
	width := 36
	for _, value := range values {
		width = max(width, runewidth.StringWidth(value)+4)
	}
	width = min(width, min(64, max(1, m.width-2)))
	height = min(height, max(1, m.height-2))
	return assetTUIDialogGeometry{
		x: max(0, (m.width-width)/2), y: max(0, (m.height-height)/2),
		width: width, height: height,
	}
}

func (m *assetTUI) helpDialogGeometry(rows []assetTUIHelpRow) assetTUIDialogGeometry {
	keyWidth, descriptionWidth := 0, 0
	for _, row := range rows {
		keyWidth = max(keyWidth, runewidth.StringWidth(row.key))
		descriptionWidth = max(descriptionWidth, runewidth.StringWidth(row.description))
	}
	maxWidth := min(64, max(1, m.width-1))
	width := min(max(30, keyWidth+descriptionWidth+9), maxWidth)
	if m.showHelpProtocols() {
		width = maxWidth
	}
	protocolLines := m.helpProtocolLines(max(0, width-6))
	maxHeight := max(1, m.height-1)
	if m.helpDialogUsesUnifiedScroll(protocolLines) {
		displayRows := wrappedHelpDialogRows(rows, width)
		displayRows = appendHelpProtocolRows(displayRows, protocolLines)
		height := min(len(displayRows)+6, maxHeight)
		return assetTUIDialogGeometry{
			x: max(0, m.width-width-1), y: max(0, m.height-height-1),
			width: width, height: height, rows: max(0, height-6),
		}
	}
	protocolRows := 0
	if len(protocolLines) > 0 {
		protocolRows = len(protocolLines) + 1
	}
	displayRows := wrappedHelpDialogRows(rows, width)
	height := min(len(displayRows)+6+protocolRows, maxHeight)
	return assetTUIDialogGeometry{
		x: max(0, m.width-width-1), y: max(0, m.height-height-1),
		width: width, height: height, rows: max(0, height-6-protocolRows),
	}
}

func (m *assetTUI) helpDialogUsesUnifiedScroll(protocolLines []string) bool {
	if len(protocolLines) == 0 {
		return false
	}
	// Keep at least one shortcut row visible when protocols fit. On shorter
	// terminals, scroll the whole body so no protocol line is discarded.
	return len(protocolLines)+1 > max(0, max(1, m.height-1)-7)
}

func appendHelpProtocolRows(rows []assetTUIHelpRow, protocolLines []string) []assetTUIHelpRow {
	if len(protocolLines) == 0 {
		return rows
	}
	rows = append(rows, assetTUIHelpRow{divider: true})
	for _, line := range protocolLines {
		rows = append(rows, assetTUIHelpRow{fullWidth: line})
	}
	return rows
}

func (m *assetTUI) helpDialogScrollableRows(rows []assetTUIHelpRow,
	geometry assetTUIDialogGeometry) []assetTUIHelpRow {
	displayRows := wrappedHelpDialogRows(rows, geometry.width)
	protocolLines := m.helpProtocolLines(max(0, geometry.width-6))
	if m.helpDialogUsesUnifiedScroll(protocolLines) {
		displayRows = appendHelpProtocolRows(displayRows, protocolLines)
	}
	return displayRows
}

func helpDialogRows(rows []assetTUIHelpRow) []assetTUIHelpRow {
	displayRows := make([]assetTUIHelpRow, 0, max(0, len(rows)*2-1))
	for index, row := range rows {
		displayRows = append(displayRows, row)
		if index+1 < len(rows) {
			displayRows = append(displayRows, assetTUIHelpRow{separator: true})
		}
	}
	return displayRows
}

func helpDialogColumnWidths(rows []assetTUIHelpRow, width int) (int, int) {
	keyWidth := 0
	for _, row := range rows {
		keyWidth = max(keyWidth, runewidth.StringWidth(row.key))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	return keyWidth, max(1, width-keyWidth-8)
}

func wrappedHelpDialogRows(rows []assetTUIHelpRow, width int) []assetTUIHelpRow {
	_, descriptionWidth := helpDialogColumnWidths(rows, width)
	displayRows := make([]assetTUIHelpRow, 0, max(0, len(rows)*2-1))
	for rowIndex, row := range rows {
		lines := strings.Split(runewidth.Wrap(row.description, descriptionWidth), "\n")
		if len(lines) == 0 {
			lines = []string{""}
		}
		for lineIndex, line := range lines {
			key := ""
			if lineIndex == 0 {
				key = row.key
			}
			displayRows = append(displayRows, assetTUIHelpRow{key: key, description: line})
		}
		if rowIndex+1 < len(rows) {
			displayRows = append(displayRows, assetTUIHelpRow{separator: true})
		}
	}
	return displayRows
}

func (m *assetTUI) updateHelpDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	shortcutRows := m.helpShortcutRows()
	geometry := m.helpDialogGeometry(shortcutRows)
	rows := m.helpDialogScrollableRows(shortcutRows, geometry)
	visible := max(1, geometry.rows)
	maxScroll := max(0, len(rows)-visible)
	switch msg.Type {
	case tea.KeyEsc:
		m.helpDialog = false
	case tea.KeyUp:
		m.helpScroll = max(0, m.helpScroll-1)
	case tea.KeyDown:
		m.helpScroll = min(maxScroll, m.helpScroll+1)
	case tea.KeyPgUp:
		m.helpScroll = max(0, m.helpScroll-visible)
	case tea.KeyPgDown:
		m.helpScroll = min(maxScroll, m.helpScroll+visible)
	case tea.KeyHome:
		m.helpScroll = 0
	case tea.KeyEnd:
		m.helpScroll = maxScroll
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] == 'k' {
			m.helpScroll = max(0, m.helpScroll-1)
		} else if len(msg.Runes) == 1 && msg.Runes[0] == 'j' {
			m.helpScroll = min(maxScroll, m.helpScroll+1)
		}
	}
	return m, nil
}

func (m *assetTUI) updateHelpDialogMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	shortcutRows := m.helpShortcutRows()
	geometry := m.helpDialogGeometry(shortcutRows)
	rows := m.helpDialogScrollableRows(shortcutRows, geometry)
	listY := geometry.y + 3
	if m.helpDragging {
		if event.Action == tea.MouseActionRelease {
			m.helpDragging = false
			return m, nil
		}
		if event.Action == tea.MouseActionMotion {
			row := max(0, min(event.Y-listY, geometry.rows-1))
			m.helpScroll = tuiScrollbarStartAt(row, m.helpScrollbarGrab, len(rows), geometry.rows)
		}
		return m, nil
	}
	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelUp {
		m.helpScroll = max(0, m.helpScroll-1)
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelDown {
		m.helpScroll = min(max(0, len(rows)-geometry.rows), m.helpScroll+1)
		return m, nil
	}
	if event.Button == tea.MouseButtonLeft && len(rows) > geometry.rows &&
		event.X == geometry.x+geometry.width-2 && event.Y >= listY && event.Y < listY+geometry.rows {
		thumbStart, thumbSize, _, _ := tuiScrollbarMetrics(m.helpScroll, len(rows), geometry.rows)
		row := event.Y - listY
		m.helpScrollbarGrab = thumbSize / 2
		if row >= thumbStart && row < thumbStart+thumbSize {
			m.helpScrollbarGrab = row - thumbStart
		}
		m.helpDragging = true
		m.helpScroll = tuiScrollbarStartAt(row, m.helpScrollbarGrab, len(rows), geometry.rows)
	}
	return m, nil
}

func (m *assetTUI) renderDialog(lines []string) {
	geometry := m.dialogGeometry()
	if geometry.width < 36 || geometry.height < 11 || geometry.y < 0 || geometry.y+geometry.height > len(lines) {
		return
	}
	dialog := m.dialog
	lang := i18n.NewLang(m.handler.i18nLang)
	title := fmt.Sprintf("%s - %s", m.tr("连接", "Connect"), dialog.asset.Name)
	popup := tuiDialogFrame("", geometry.width, geometry.height)
	popup[1] = "│" + tuiCenter(title, geometry.width-2) + "│"
	popup[2] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[3] = "│" + tuiDialogLeft(lang.T("Protocol"), geometry.width-2) + "│"
	protocols, _ := tuiDialogProtocolLine(dialog.protocols, dialog.protocolIndex, geometry.width-6)
	popup[4] = "│    " + protocols + "│"
	search := m.tr("账号", "Account")
	if len(dialog.accountSearch) > 0 || dialog.searchingAccount {
		search, _ = tuiSearchField(m.tr("账号", "Account"), m.tr("搜索", "Search"),
			dialog.accountSearch, geometry.width-4)
		popup[6] = "│  " + search + "│"
	} else {
		popup[6] = "│" + tuiDialogLeft(search, geometry.width-2) + "│"
	}
	accountStart := dialog.accountListStart(geometry.rows)
	showScrollbar := len(dialog.accountMatches) > geometry.rows
	accountWidth := geometry.width - 6
	if showScrollbar {
		accountWidth--
	}
	for row := 0; row < geometry.rows; row++ {
		account := ""
		selected := false
		if position := accountStart + row; position < len(dialog.accountMatches) {
			item := dialog.accounts[dialog.accountMatches[position]]
			account = item.Username
			if item.Name != "" {
				account = item.Name + " (" + item.Username + ")"
			}
			selected = position == dialog.accountIndex
		} else if row == 0 && len(dialog.accountMatches) == 0 {
			account = m.tr("没有匹配的账号", "No matching accounts")
		}
		content := tuiFit(account, accountWidth)
		if selected {
			content = tuiSelectedStyle + content + tuiStyleReset
		}
		scrollbar := ""
		if showScrollbar {
			scrollbar = tuiScrollbarCell(row, accountStart, len(dialog.accountMatches), geometry.rows)
		}
		popup[row+7] = "│    " + content + scrollbar + "│"
	}
	hint := tuiShortcutLine(geometry.width-4, []string{
		"/:" + lang.T("Search"), "space:" + lang.T("Details"),
		"enter:" + lang.T("Connect"), "esc:" + lang.T("Cancel"),
	}, "?:"+lang.T("View help"))
	if dialog.searchingAccount {
		hint = strings.Join([]string{
			"enter:" + lang.T("Confirm"), "esc:" + lang.T("Cancel"),
			"backspace:" + lang.T("Delete"), "ctrl+u:" + lang.T("Clear"),
		}, " · ")
	}
	popup[geometry.height-3] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[geometry.height-2] = "│" + tuiDialogLeft(hint, geometry.width-2) + "│"
	m.overlayDialog(lines, popup, geometry)
}

func (m *assetTUI) renderLanguageDialog(lines []string) {
	geometry := m.languageDialogGeometry()
	if geometry.width < 20 || geometry.height < 8 || geometry.y < 0 || geometry.y+geometry.height > len(lines) {
		return
	}
	dialog := m.languageDialog
	popup := tuiDialogFrame("", geometry.width, geometry.height)
	title := m.tr("切换语言", "language switch")
	popup[1] = "│" + tuiCenter(title, geometry.width-2) + "│"
	popup[2] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	start := tuiDialogListStart(dialog.index, len(i18n.AllCodes), geometry.rows)
	for row := 0; row < geometry.rows; row++ {
		content := ""
		if position := start + row; position < len(i18n.AllLangCodesStr) {
			prefix := "  "
			if position == dialog.index {
				prefix = "▶ "
			}
			content = tuiDialogLeft(prefix+i18n.AllLangCodesStr[position], geometry.width-2)
			if position == dialog.index {
				content = tuiSelectedStyle + content + tuiStyleReset
			}
		} else {
			content = strings.Repeat(" ", geometry.width-2)
		}
		popup[row+3] = "│" + content + "│"
	}
	lang := i18n.NewLang(m.handler.i18nLang)
	hint := tuiShortcutLine(geometry.width-4, []string{
		"enter:" + lang.T("Confirm"), "esc:" + lang.T("Cancel"),
	}, "?:"+lang.T("View help"))
	popup[geometry.height-3] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[geometry.height-2] = "│" + tuiDialogLeft(hint, geometry.width-2) + "│"
	m.overlayDialog(lines, popup, geometry)
}

func (m *assetTUI) renderQuitDialog(lines []string) {
	geometry := m.quitDialogGeometry()
	if geometry.width < 20 || geometry.height < 7 || geometry.y < 0 ||
		geometry.y+geometry.height > len(lines) {
		return
	}
	title := m.tr("退出 Koko", "Quit Koko")
	message := m.tr("确定退出本次 SSH 会话吗？", "Quit this SSH session?")
	shortcuts := "enter:" + m.tr("确认", "Confirm") + " · esc:" + m.tr("取消", "Cancel")
	popup := tuiDialogFrame(title, geometry.width, geometry.height)
	popup[2] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[3] = "│  " + tuiFit(message, geometry.width-4) + "│"
	if m.unfinishedSessions > 0 {
		warning := fmt.Sprintf(m.tr(
			"仍有 %d 个多会话未结束，退出后将一并结束。",
			"%d multi-sessions are still active; quitting will end them.",
		), m.unfinishedSessions)
		popup[4] = "│  " + tuiFit(warning, geometry.width-4) + "│"
	}
	popup[geometry.height-3] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[geometry.height-2] = "│  " + tuiFit(shortcuts, geometry.width-4) + "│"
	m.overlayDialog(lines, popup, geometry)
}

func (m *assetTUI) renderHelpDialog(lines []string) {
	rows := m.helpShortcutRows()
	geometry := m.helpDialogGeometry(rows)
	if geometry.width < 20 || geometry.height < 7 || geometry.y < 0 || geometry.y+geometry.height > len(lines) {
		return
	}
	popup := tuiDialogFrame("", geometry.width, geometry.height)
	lang := i18n.NewLang(m.handler.i18nLang)
	popup[1] = "│" + tuiCenter(lang.T("View help"), geometry.width-2) + "│"
	popup[2] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	protocolLines := m.helpProtocolLines(max(0, geometry.width-6))
	unifiedScroll := m.helpDialogUsesUnifiedScroll(protocolLines)
	displayRows := m.helpDialogScrollableRows(rows, geometry)
	m.helpScroll = max(0, min(m.helpScroll, max(0, len(displayRows)-geometry.rows)))
	keyWidth, descriptionWidth := helpDialogColumnWidths(rows, geometry.width)
	showScrollbar := len(displayRows) > geometry.rows
	for index := 0; index < geometry.rows; index++ {
		row := assetTUIHelpRow{}
		if position := m.helpScroll + index; position < len(displayRows) {
			row = displayRows[position]
		}
		scrollbar := " "
		if showScrollbar {
			scrollbar = tuiScrollbarCell(index, m.helpScroll, len(displayRows), geometry.rows)
		}
		if row.divider {
			popup[index+3] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
			continue
		}
		if row.fullWidth != "" {
			popup[index+3] = "│  " + tuiFit(row.fullWidth, geometry.width-6) + " " + scrollbar + "│"
			continue
		}
		if row.separator {
			popup[index+3] = "│  " + strings.Repeat(" ", keyWidth) + " │ " +
				strings.Repeat(" ", descriptionWidth) + scrollbar + "│"
			continue
		}
		if row.key == "" && row.description == "" {
			popup[index+3] = "│" + strings.Repeat(" ", geometry.width-3) + scrollbar + "│"
			continue
		}
		popup[index+3] = "│  " + tuiFit(row.key, keyWidth) + " │ " +
			tuiFit(row.description, descriptionWidth) + scrollbar + "│"
	}
	protocolStart := geometry.rows + 3
	if !unifiedScroll && len(protocolLines) > 0 {
		popup[protocolStart] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
		for index, line := range protocolLines {
			popup[protocolStart+index+1] = "│  " + tuiFit(line, geometry.width-6) + "  │"
		}
	}
	popup[geometry.height-3] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[geometry.height-2] = "│" + tuiDialogLeft("esc:"+lang.T("Cancel"), geometry.width-2) + "│"
	m.overlayDialog(lines, popup, geometry)
}

func (m *assetTUI) showHelpProtocols() bool {
	return m.treeDialog == nil && m.languageDialog == nil && m.dialog == nil
}

func (m *assetTUI) helpProtocolLines(width int) []string {
	if !m.showHelpProtocols() || width <= 0 {
		return nil
	}
	label := i18n.NewLang(m.handler.i18nLang).T("Protocols supported by the current terminal")
	items := srvconn.SupportedProtocols()
	if len(items) == 0 {
		return []string{label + ":", "-"}
	}
	lines := []string{label + ":"}
	line := ""
	for _, item := range items {
		separator := ""
		if line != "" {
			separator = " · "
		}
		if runewidth.StringWidth(line+separator+item) > width && line != "" {
			lines = append(lines, line)
			line = item
			continue
		}
		line += separator + item
	}
	return append(lines, line)
}

func (m *assetTUI) helpShortcutRows() []assetTUIHelpRow {
	lang := i18n.NewLang(m.handler.i18nLang)
	row := func(key, description string) assetTUIHelpRow {
		return assetTUIHelpRow{key: key, description: lang.T(description)}
	}
	if m.treeDialog != nil {
		return []assetTUIHelpRow{
			row("↑, ↓, j, k", "Move the selection up or down"),
			row("←, →, h, l", "Collapse or expand the selected node"),
			row("space", "View complete information for the selected item"),
			row("enter", "Use the selected node to filter assets"),
			row("tab, shift+tab", "Switch the asset tree type"), row("r", "Reload the current asset tree"),
			row("?", "Open shortcut help"),
		}
	}
	if m.languageDialog != nil {
		return []assetTUIHelpRow{
			row("↑, ↓, j, k", "Move the selection up or down"),
			row("enter", "Apply the selected language"), row("?", "Open shortcut help"),
		}
	}
	if m.dialog != nil {
		return []assetTUIHelpRow{
			row("↑, ↓, j, k", "Select an authorized account"),
			row("←, →, h, l", "Select a connection protocol"),
			row("space", "View complete information for the selected item"),
			row("/", "Search authorized accounts"), row("enter", "Connect using the selected account and protocol"),
			row("?", "Open shortcut help"),
		}
	}
	if m.loading || m.loadingChoices {
		rows := []assetTUIHelpRow{}
		if m.multiSessionCount > 0 {
			rows = append(rows, row("w", "Return to the multi-session workspace"))
		}
		if !m.loadingChoices {
			rows = append(rows, row("g", "Open the asset tree"), row("s", "Switch the interface language"))
		}
		return append(rows,
			assetTUIHelpRow{key: "v", description: m.mouseModeHelpDescription()},
			row("t", "Switch to text mode"), row("ctrl+c, q", "Quit"),
			row("?", "Open shortcut help"),
		)
	}
	rows := []assetTUIHelpRow{row("/", "Search assets in the current scope")}
	if m.query != "" {
		rows = append(rows, row("x", "Clear the current asset search"))
	}
	rows = append(rows,
		row("↑, ↓, j, k", "Move the selection up or down"),
		row("←, →, h, l", "Go to the previous or next page"),
		row("space", "View complete information for the selected item"),
		row("enter", "Connect to the selected asset"),
		row("c", "Connect to the selected asset in a native single session"),
		row("g", "Open the asset tree"),
		row("r", "Refresh the current asset list and keep search and tree filters"),
	)
	if m.multiSessionCount > 0 {
		rows = append(rows, row("w", "Return to the multi-session workspace"))
	}
	if m.selectedTree != 0 {
		rows = append(rows, row("d", "Clear the selected tree node"))
	}
	return append(rows,
		row("s", "Switch the interface language"),
		assetTUIHelpRow{key: "v", description: m.mouseModeHelpDescription()},
		row("t", "Switch to text mode"),
		row("ctrl+c, q", "Quit"),
		row("?", "Open shortcut help"),
	)
}

func (m *assetTUI) overlayDialog(lines, popup []string, geometry assetTUIDialogGeometry) {
	maskX := max(0, geometry.x-4)
	maskRight := min(m.width, geometry.x+geometry.width+4)
	maskWidth := maskRight - maskX
	for row := max(0, geometry.y-2); row < geometry.y; row++ {
		lines[row] = tuiOverlayLine(lines[row], strings.Repeat(" ", maskWidth), maskWidth, maskX, m.width)
	}
	for row := range popup {
		foreground := strings.Repeat(" ", geometry.x-maskX) + popup[row] +
			strings.Repeat(" ", maskRight-geometry.x-geometry.width)
		lines[geometry.y+row] = tuiOverlayLine(lines[geometry.y+row], foreground, maskWidth, maskX, m.width)
	}
	for row := geometry.y + geometry.height; row < min(len(lines), geometry.y+geometry.height+2); row++ {
		lines[row] = tuiOverlayLine(lines[row], strings.Repeat(" ", maskWidth), maskWidth, maskX, m.width)
	}
}

func tuiOverlayLine(background, foreground string, foregroundWidth, x, width int) string {
	style := ""
	for _, candidate := range []string{tuiSelectedStyle, tuiBoldStyle, "\x1b[2m"} {
		if strings.HasPrefix(background, candidate) {
			style = candidate
			background = strings.TrimPrefix(background, candidate)
			background = strings.TrimSuffix(background, tuiStyleReset)
			break
		}
	}

	left := tuiFit(runewidth.Truncate(background, x, ""), x)
	rightStart := x + foregroundWidth
	rightWidth := max(0, width-rightStart)
	right := tuiFit(runewidth.TruncateLeft(background, rightStart, ""), rightWidth)
	if style != "" {
		left = style + left + tuiStyleReset
		right = style + right + tuiStyleReset
	}
	return left + foreground + right
}

// tuiDialogFrame keeps titles inside every dialog and leaves the border intact.
func tuiDialogFrame(title string, width, height int) []string {
	lines := make([]string, max(0, height))
	if width < 2 || height == 0 {
		return lines
	}
	lines[0] = "┌" + strings.Repeat("─", width-2) + "┐"
	for row := 1; row < height-1; row++ {
		lines[row] = "│" + strings.Repeat(" ", width-2) + "│"
	}
	if height > 1 {
		lines[height-1] = "└" + strings.Repeat("─", width-2) + "┘"
	}
	if height > 2 {
		lines[1] = "│" + tuiCenter(title, width-2) + "│"
	}
	return lines
}

type tuiDialogProtocolHit struct {
	index      int
	start, end int
}

func tuiDialogProtocolLine(protocols []string, selected, width int) (string, []tuiDialogProtocolHit) {
	if len(protocols) == 0 || width <= 0 {
		return strings.Repeat(" ", max(0, width)), nil
	}
	selected = max(0, min(selected, len(protocols)-1))
	separator := " · "
	rangeWidth := func(start, end int) int {
		result := 0
		for i := start; i < end; i++ {
			if i > start {
				result += runewidth.StringWidth(separator)
			}
			result += runewidth.StringWidth(protocols[i])
		}
		return result
	}

	start, end := selected, selected+1
	for {
		added := false
		if start > 0 && rangeWidth(start-1, end) <= width {
			start--
			added = true
		}
		if end < len(protocols) && rangeWidth(start, end+1) <= width {
			end++
			added = true
		}
		if !added {
			break
		}
	}

	position := 0
	var line strings.Builder
	hits := make([]tuiDialogProtocolHit, 0, end-start)
	for i := start; i < end; i++ {
		if i > start {
			line.WriteString(separator)
			position += runewidth.StringWidth(separator)
		}
		label := runewidth.Truncate(protocols[i], max(0, width-position), "")
		labelWidth := runewidth.StringWidth(label)
		hits = append(hits, tuiDialogProtocolHit{index: i, start: position, end: position + labelWidth})
		if i == selected {
			line.WriteString(tuiSelectedProtocolStyle)
			line.WriteString(label)
			line.WriteString(tuiStyleReset)
		} else {
			line.WriteString(label)
		}
		position += labelWidth
	}
	line.WriteString(strings.Repeat(" ", max(0, width-position)))
	return line.String(), hits
}

func tuiCenter(value string, width int) string {
	value = runewidth.Truncate(value, width, "")
	padding := max(0, width-runewidth.StringWidth(value))
	left := padding / 2
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", padding-left)
}

func tuiDialogLeft(value string, width int) string {
	padding := min(2, max(0, width))
	return strings.Repeat(" ", padding) + tuiFit(value, width-padding)
}

func tuiDialogListStart(selected, total, rows int) int {
	start := max(0, selected-rows/2)
	return min(start, max(0, total-rows))
}

func tuiScrollbarCell(row, start, total, visible int) string {
	thumbStart, thumbSize, _, ok := tuiScrollbarMetrics(start, total, visible)
	if !ok || row < 0 || row >= visible {
		return ""
	}
	if row >= thumbStart && row < thumbStart+thumbSize {
		return "█"
	}
	return "░"
}

func tuiScrollbarMetrics(start, total, visible int) (thumbStart, thumbSize, maxStart int, ok bool) {
	if total <= visible || visible <= 0 {
		return 0, 0, 0, false
	}
	maxStart = total - visible
	thumbSize = max(1, visible*visible/total)
	start = max(0, min(start, maxStart))
	if visible > thumbSize {
		thumbStart = (start*(visible-thumbSize) + maxStart/2) / maxStart
	}
	return thumbStart, thumbSize, maxStart, true
}

func tuiScrollbarStartAt(row, grab, total, visible int) int {
	_, thumbSize, maxStart, ok := tuiScrollbarMetrics(0, total, visible)
	if !ok || visible <= thumbSize {
		return 0
	}
	thumbStart := max(0, min(row-grab, visible-thumbSize))
	return (thumbStart*maxStart + (visible-thumbSize)/2) / (visible - thumbSize)
}

func (m *assetTUI) updateLanguageDialogMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	geometry := m.languageDialogGeometry()
	if event.X < geometry.x || event.X >= geometry.x+geometry.width ||
		event.Y < geometry.y || event.Y >= geometry.y+geometry.height {
		if event.Button == tea.MouseButtonLeft {
			m.languageDialog = nil
		}
		return m, nil
	}
	dialog := m.languageDialog
	if event.Button == tea.MouseButtonWheelUp || event.Button == tea.MouseButtonWheelDown {
		step := 1
		if event.Button == tea.MouseButtonWheelUp {
			step = -1
		}
		dialog.index = max(0, min(len(i18n.AllCodes)-1, dialog.index+step))
		return m, nil
	}
	if event.Button != tea.MouseButtonLeft {
		return m, nil
	}
	row := event.Y - geometry.y - 3
	if row < 0 || row >= geometry.rows {
		dialog.lastClickRow = -1
		dialog.lastClickAt = time.Time{}
		return m, nil
	}
	start := tuiDialogListStart(dialog.index, len(i18n.AllCodes), geometry.rows)
	position := start + row
	if position >= len(i18n.AllCodes) {
		return m, nil
	}
	dialog.index = position
	now := time.Now()
	if dialog.lastClickRow == position && now.Sub(dialog.lastClickAt) <= tuiDoubleClickInterval {
		return m.chooseLanguage()
	}
	dialog.lastClickRow = position
	dialog.lastClickAt = now
	return m, nil
}

func (m *assetTUI) updateDialogMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	geometry := m.dialogGeometry()
	dialog := m.dialog
	total := len(dialog.accountMatches)
	listY := geometry.y + 7
	if dialog.scrollbarDragging {
		if event.Action == tea.MouseActionRelease {
			dialog.scrollbarDragging = false
			return m, nil
		}
		if event.Action == tea.MouseActionMotion {
			row := max(0, min(event.Y-listY, geometry.rows-1))
			start := tuiScrollbarStartAt(row, dialog.scrollbarGrab, total, geometry.rows)
			dialog.accountScroll = start
			dialog.accountIndex = min(start, total-1)
		}
		return m, nil
	}
	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if event.X < geometry.x || event.X >= geometry.x+geometry.width ||
		event.Y < geometry.y || event.Y >= geometry.y+geometry.height {
		if event.Button == tea.MouseButtonLeft {
			m.dialog = nil
		}
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelUp || event.Button == tea.MouseButtonWheelDown {
		step := 1
		if event.Button == tea.MouseButtonWheelUp {
			step = -1
		}
		dialog.accountIndex = max(0, min(total-1, dialog.accountIndex+step))
		dialog.accountListStart(geometry.rows)
		return m, nil
	}
	if event.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if total > geometry.rows && event.X == geometry.x+geometry.width-2 &&
		event.Y >= listY && event.Y < listY+geometry.rows {
		start := dialog.accountListStart(geometry.rows)
		thumbStart, thumbSize, _, _ := tuiScrollbarMetrics(start, total, geometry.rows)
		row := event.Y - listY
		dialog.scrollbarGrab = thumbSize / 2
		if row >= thumbStart && row < thumbStart+thumbSize {
			dialog.scrollbarGrab = row - thumbStart
		}
		dialog.scrollbarDragging = true
		newStart := tuiScrollbarStartAt(row, dialog.scrollbarGrab, total, geometry.rows)
		if newStart != start {
			dialog.accountScroll = newStart
			dialog.accountIndex = newStart
		}
		return m, nil
	}
	if event.Y == geometry.y+4 {
		_, hits := tuiDialogProtocolLine(dialog.protocols, dialog.protocolIndex, geometry.width-6)
		column := event.X - geometry.x - 5
		for _, hit := range hits {
			if column >= hit.start && column < hit.end {
				dialog.protocolIndex = hit.index
				dialog.searchingAccount = false
				break
			}
		}
		return m, nil
	}
	if event.Y == geometry.y+6 {
		dialog.searchingAccount = true
		return m, nil
	}
	row := event.Y - geometry.y - 7
	if row < 0 || row >= geometry.rows {
		return m, nil
	}
	dialog.searchingAccount = false
	start := dialog.accountListStart(geometry.rows)
	if position := start + row; position < total {
		dialog.accountIndex = position
	}
	return m, nil
}

func (m *assetTUI) searchLine() string {
	width, info := m.topLineLayout()
	line := m.searchLineWithWidth(width)
	if info == "" {
		return line
	}
	return line + strings.Repeat(" ", m.width-width-runewidth.StringWidth(info)) + info
}

func (m *assetTUI) searchLineWithWidth(width int) string {
	label := m.searchLabel()
	if !m.searching {
		if m.query == "" {
			return tuiFit(label, width)
		}
		search := m.tr("搜索", "Search")
		line, _ := tuiSearchField(label, search, []rune(m.query), width)
		return line
	}
	search := m.tr("搜索", "Search")
	line, _ := tuiSearchField(label, search, m.searchInput, width)
	return line
}

func (m *assetTUI) topLineLayout() (int, string) {
	if m.searching {
		return m.width, ""
	}
	info := m.topRightInfo()
	if info == "" || m.width <= 0 {
		return m.width, ""
	}
	const gap = 2
	const minSearchWidth = 12
	maxInfoWidth := m.width - minSearchWidth - gap
	if maxInfoWidth <= 0 {
		return m.width, ""
	}
	if runewidth.StringWidth(info) > maxInfoWidth {
		info = runewidth.Truncate(info, maxInfoWidth, "…")
	}
	return max(1, m.width-runewidth.StringWidth(info)-gap), info
}

func (m *assetTUI) topRightInfo() string {
	if m.handler == nil || m.handler.user == nil {
		return ""
	}
	name := strings.TrimSpace(m.handler.user.Name)
	productName := ""
	if m.handler.publicSetting != nil {
		productName = strings.TrimSpace(m.handler.publicSetting.Interface.LoginTitle)
	}
	if productName == "" {
		return name
	}
	if name == "" {
		return productName
	}
	return name + " | " + productName
}

func (m *assetTUI) searchLabel() string {
	label := m.tr("我的资产", "My Assets")
	if m.selectedTree == 0 {
		return label
	}
	title := m.treeTitle(m.selectedTree)
	if m.selectedPath == "" {
		return label + " · " + title
	}
	return label + " · " + title + ":" + m.selectedPath
}

func (m *assetTUI) mouseModeShortcut() string {
	if m.handler.mouseMode == terminalMouseModeClient {
		return "v:" + m.tr("界面操作(鼠标)", "Interface control (mouse)")
	}
	return "v:" + m.tr("文本选择(鼠标)", "Text selection (mouse)")
}

func (m *assetTUI) mouseModeHelpDescription() string {
	if m.handler.mouseMode == terminalMouseModeClient {
		return m.tr(
			"切换到界面操作，由 Koko 接管鼠标，可点击、滚动和操作界面",
			"Switch to interface control; Koko handles mouse clicks, scrolling, and UI actions",
		)
	}
	return m.tr(
		"切换到文本选择，由本地终端接管鼠标，可拖动选择和复制文本",
		"Switch to text selection; the local terminal handles dragging and copying text",
	)
}

func tuiSearchField(label, search string, input []rune, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	inputWidth := runewidth.StringWidth(string(input))
	reservedInput := min(inputWidth, max(1, width/2))
	suffix := " · " + search + ":"
	labelWidth := max(0, width-runewidth.StringWidth(suffix)-reservedInput-1)
	visibleLabel := label
	if runewidth.StringWidth(visibleLabel) > labelWidth {
		if labelWidth <= 1 {
			visibleLabel = strings.Repeat("…", labelWidth)
		} else {
			drop := runewidth.StringWidth(visibleLabel) - labelWidth + 1
			visibleLabel = "…" + runewidth.TruncateLeft(visibleLabel, drop, "")
		}
	}
	prompt := visibleLabel + suffix
	available := max(0, width-runewidth.StringWidth(prompt)-1)
	visibleInput := runewidth.TruncatePrefix(string(input), available, "")
	cursor := min(width-1, runewidth.StringWidth(prompt)+runewidth.StringWidth(visibleInput))
	return tuiFit(prompt+visibleInput, width), max(0, cursor)
}

func (m *assetTUI) pageLine() string {
	if !m.hasPagination() {
		return ""
	}
	if m.total == 0 {
		return "0-0/0"
	}
	return fmt.Sprintf("%d-%d/%d", m.offset+1, min(m.offset+len(m.assets), m.total), m.total)
}

func (m *assetTUI) statusPageLine() string {
	status := ""
	if m.status != "" {
		status = tuiStatusPrefix + m.status
	}
	page := m.pageLine()
	if page == "" {
		return tuiFit(status, m.width)
	}
	pageWidth := runewidth.StringWidth(page)
	if status == "" || pageWidth >= m.width {
		return tuiRightAlign(page, m.width)
	}
	return tuiFit(status, max(0, m.width-pageWidth-1)) + " " + page
}

func (m *assetTUI) footerLine() string {
	lang := i18n.NewLang(m.handler.i18nLang)
	if m.searching {
		shortcuts := strings.Join([]string{
			"enter:" + lang.T("Search"), "esc:" + lang.T("Cancel"),
			"backspace:" + lang.T("Delete"), "ctrl+u:" + lang.T("Clear"),
		}, " · ")
		return tuiFit(shortcuts, m.width)
	}
	items := []string{
		"/:" + lang.T("Search"), "enter:" + lang.T("Connect"),
		"c:" + lang.T("Direct connect"),
	}
	if m.multiSessionCount > 0 {
		items = append(items, fmt.Sprintf("w:%s(%d/%d)",
			lang.T("Sessions"), m.multiSessionCount, assetTUIMaxMultiSessions))
	}
	items = append(items, "space:"+lang.T("Details"), "g:"+lang.T("Asset tree"))
	if m.query != "" {
		items = append(items, "x:"+lang.T("Clear search"))
	}
	if m.selectedTree != 0 {
		items = append(items, "d:"+lang.T("Clear node"))
	}
	items = append(items, m.mouseModeShortcut(), "t:"+lang.T("Text mode"), "q:"+lang.T("Quit"))
	return tuiShortcutLine(m.width, items, "?:"+lang.T("View help"))
}

func tuiShortcutLine(width int, items []string, help string) string {
	visible := make([]string, 0, len(items)+1)
	for _, item := range items {
		candidate := append(append([]string(nil), visible...), item, help)
		if runewidth.StringWidth(strings.Join(candidate, " · ")) <= width {
			visible = append(visible, item)
		}
	}
	visible = append(visible, help)
	return tuiFit(strings.Join(visible, " · "), width)
}

func (m *assetTUI) assetRows() int {
	return max(1, m.visibleAssetRows())
}

func (m *assetTUI) visibleAssetRows() int {
	rows := max(0, m.height-5)
	if m.hasPagination() {
		rows = max(0, rows-1)
	}
	return rows
}

func (m *assetTUI) hasPagination() bool {
	return m.total > max(1, m.height-5)
}

type assetTUIColumn struct {
	label  string
	width  int
	weight int
	value  func(model.PermAsset) string
}

func (m *assetTUI) columns() []assetTUIColumn {
	lang := i18n.NewLang(m.handler.i18nLang)
	numberWidth := max(runewidth.StringWidth(lang.T("Number")), len(fmt.Sprintf("%d", max(1, m.total))))
	columns := []assetTUIColumn{{
		label: lang.T("Number"), width: numberWidth, value: func(asset model.PermAsset) string { return "" },
	}}
	columns = append(columns, assetTUIColumn{label: lang.T("Name"), weight: 3, value: func(asset model.PermAsset) string { return asset.Name }})
	if !m.selector.isHiddenField("Address") {
		columns = append(columns, assetTUIColumn{label: lang.T("Address"), weight: 2, value: func(asset model.PermAsset) string { return asset.Address }})
	}
	if m.width >= 70 && !m.selector.isHiddenField("Platform") {
		columns = append(columns, assetTUIColumn{label: lang.T("Platform"), weight: 2, value: func(asset model.PermAsset) string { return asset.Platform.Name }})
	}
	if m.width >= 100 && !m.selector.isHiddenField("Organization") {
		columns = append(columns, assetTUIColumn{label: lang.T("Organization"), weight: 2, value: func(asset model.PermAsset) string { return asset.OrgName }})
	}
	if m.width >= 125 && !m.selector.isHiddenField("Comment") {
		columns = append(columns, assetTUIColumn{label: lang.T("Comment"), weight: 3, value: func(asset model.PermAsset) string { return asset.Comment }})
	}

	remaining := max(0, m.width-numberWidth-(len(columns)-1)*2)
	weight := 0
	for i := 1; i < len(columns); i++ {
		weight += columns[i].weight
	}
	for i := 1; i < len(columns); i++ {
		columns[i].width = remaining * columns[i].weight / max(1, weight)
		remaining -= columns[i].width
		weight -= columns[i].weight
	}
	return columns
}

func (m *assetTUI) renderColumns(columns []assetTUIColumn, asset *model.PermAsset, index int) string {
	parts := make([]string, len(columns))
	for i := range columns {
		value := columns[i].label
		if asset != nil {
			if i == 0 {
				value = fmt.Sprintf("%d", m.offset+index+1)
			} else {
				value = columns[i].value(*asset)
			}
		}
		parts[i] = tuiFit(value, columns[i].width)
	}
	return tuiFit(strings.Join(parts, "  "), m.width)
}

func (m *assetTUI) tr(zh, en string) string {
	return m.handler.tr(zh, en)
}

func tuiFit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, value)
	if runewidth.StringWidth(value) > width {
		if width == 1 {
			return "…"
		}
		return runewidth.Truncate(value, width-1, "") + "…"
	}
	return value + strings.Repeat(" ", width-runewidth.StringWidth(value))
}

func tuiRightAlign(value string, width int) string {
	value = strings.TrimSpace(tuiFit(value, width))
	return strings.Repeat(" ", max(0, width-runewidth.StringWidth(value))) + value
}

func (s *Server) runTerminalModes(sess ssh.Session, user *model.User, termConf model.TerminalConfig,
	winChan <-chan ssh.Window) {
	input := NewWrapperSession(sess)
	handler := newInteractiveHandler(input, user, s.jmsService, termConf, false)
	handler.initializeAssetSelector()
	handler.selectHandler.SetSelectType(TypeAsset)
	multiSessions := newAssetTUIMultiSessionManager(handler, input)
	defer multiSessions.Close()

	routingDone := make(chan struct{})
	defer close(routingDone)
	go func() {
		for {
			select {
			case win, ok := <-winChan:
				if !ok {
					return
				}
				input.SetWin(win)
				multiSessions.Resize(win)
			case <-sess.Context().Done():
				return
			case <-routingDone:
				return
			}
		}
	}()

	model := newAssetTUI(handler)
	window := input.Pty().Window
	if terminalShouldUseTextMode(window.Width, window.Height, handler.interfaceMode) {
		model.switchText = true
	}
	classicStarted := false
	for {
		model.multiSessionCount = multiSessions.Count()
		model.unfinishedSessions = multiSessions.UnfinishedCount()
		if !model.switchText {
			err := runAssetTUI(handler, model)
			_ = input.Close()
			if err != nil {
				logger.Errorf("TUI session %s: %s", sess.User(), err)
				utils.IgnoreErrWriteString(sess, handler.tr("TUI 不可用，已切换到纯文本模式。", "TUI is unavailable; switched to text mode.")+utils.CharNewLine)
				model.switchText = true
			}
		}
		if model.switchText {
			if model.persistTextMode {
				handler.interfaceMode = terminalInterfaceModeText
				handler.saveTerminalPreference(handler.interfaceMode)
				model.persistTextMode = false
			}
			handler.displayHelp()
			if !classicStarted {
				handler.firstLoadData()
				classicStarted = true
			}
			handler.Dispatch()
			if !handler.switchTUI {
				return
			}
			window = input.Pty().Window
			if !terminalWindowSupportsTUI(window.Width, window.Height) {
				continue
			}
			handler.interfaceMode = terminalInterfaceModeTUI
			handler.saveTerminalPreference(handler.interfaceMode)
			handler.classicNavigation = false
			handler.treeOrigin = 0
			handler.selectHandler.SetSelectType(TypeAsset)
			model.selectedTree = 0
			model.selectedTreeID = ""
			model.selectedPath = ""
			model.switchText = false
			model.loading = false
			model.loadingChoices = false
			model.dialog = nil
			model.languageDialog = nil
			model.treeDialog = nil
			continue
		}
		if model.showMultiSessions {
			model.showMultiSessions = false
			if err := multiSessions.Run(true); err != nil && sess.Context().Err() == nil {
				model.status = userFacingErrorMessage(handler.tr(
					"多会话管理页已关闭", "Multi-session workspace closed",
				), err)
			}
			_ = input.Close()
			model.resume = true
			continue
		}
		if model.connection == nil {
			return
		}
		connection := *model.connection
		model.connection = nil
		if connection.multiWindow {
			if err := multiSessions.Start(connection); err != nil {
				model.status = userFacingErrorMessage(handler.tr(
					"无法打开多会话", "Failed to open multi-session",
				), err)
				model.resume = true
				continue
			}
			if err := multiSessions.Run(false); err != nil && sess.Context().Err() == nil {
				model.status = userFacingErrorMessage(handler.tr(
					"多会话管理页已关闭", "Multi-session workspace closed",
				), err)
			}
			_ = input.Close()
			model.resume = true
			continue
		}
		utils.IgnoreErrWriteString(sess, utils.CharClear)
		handler.resizeTerminal()
		handler.selectHandler.selectedAsset = &connection.asset
		handler.selectHandler.selectedAccount = &connection.account
		passwordKey := manualPasswordAttemptKey(connection.asset, connection.account, connection.protocol)
		passwordLimitError := fmt.Errorf(handler.tr(
			"手动密码最多允许输入 %d 次",
			"Manual password can be entered at most %d times",
		), maxManualPasswordAttempts)
		failure := ""
		shown := false
		if err := srvconn.IsSupportedProtocol(connection.protocol); err != nil {
			failure = err.Error()
		} else {
			_, failure, shown = connectSelectedAsset(
				handler.sess, handler.assetClient(connection.asset.OrgID), handler.user,
				connection.asset, connection.account, connection.protocol, handler.i18nLang,
				func() error {
					if handler.manualPasswords.acquire(passwordKey) {
						return nil
					}
					return passwordLimitError
				},
			)
		}
		handler.classicNavigation = false
		model.loading = false
		if handler.exitRequested {
			return
		}
		if failure != "" {
			// Discard input entered while the connection was in progress, then
			// keep the failure visible until the user explicitly returns.
			_ = input.Close()
			if !shown {
				utils.IgnoreErrWriteString(sess, utils.WrapperWarn(failure))
			}
			prompt := handler.tr("按任意键返回资产列表…", "Press any key to return to the asset list…")
			utils.IgnoreErrWriteString(sess, prompt)
			var key [256]byte
			if _, readErr := input.Read(key[:]); readErr != nil {
				return
			}
			utils.IgnoreErrWriteString(sess, utils.CharNewLine)
			_ = input.Close()
			model.status = failure
			model.resume = true
			continue
		}
		model.status = ""
		model.resume = true
	}
}

func runAssetTUI(handler *InteractiveHandler, model *assetTUI) error {
	window := handler.sess.Pty().Window
	width, height := assetTUITerminalSize(window.Width, window.Height)
	model.width, model.height = width, height

	output := &assetTUICursorWriter{output: handler.sess}
	options := []tea.ProgramOption{
		tea.WithInput(handler.sess),
		tea.WithOutput(output),
		tea.WithAltScreen(),
		tea.WithContext(handler.sess.Context()),
		tea.WithoutSignalHandler(),
	}
	if handler.mouseMode == terminalMouseModeKoko {
		options = append(options, tea.WithMouseCellMotion())
	}
	program := tea.NewProgram(model, options...)
	resizeDone := make(chan struct{})
	defer close(resizeDone)
	go func() {
		program.Send(tea.WindowSizeMsg{Width: width, Height: height})
		for {
			select {
			case win := <-handler.sess.WinCh():
				program.Send(tea.WindowSizeMsg{Width: win.Width, Height: win.Height})
			case <-handler.sess.Context().Done():
				return
			case <-resizeDone:
				return
			}
		}
	}()
	_, err := program.Run()
	return err
}

func assetTUITerminalSize(width, height int) (int, int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return min(tuiTerminalMaxWidth, width), min(tuiTerminalMaxHeight, height)
}

type assetTUICursorWriter struct {
	output io.Writer
	mu     sync.Mutex

	altScreen bool
	visible   bool
	x, y      int
}

func (w *assetTUICursorWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	output := w.consumeCursorMarker(string(p))
	if strings.Contains(output, tuiEnterAltScreen) {
		output = strings.ReplaceAll(output, tuiEnterAltScreen, tuiEnterAltScreen+tuiResetViewport)
		w.altScreen = true
	}
	if strings.Contains(output, tuiExitAltScreen) {
		output = strings.ReplaceAll(output, tuiExitAltScreen, tuiResetViewport+tuiExitAltScreen)
		w.altScreen = false
	}
	if err := writeTUIOutput(w.output, output); err != nil {
		return 0, err
	}
	if !w.altScreen {
		return len(p), nil
	}

	cursor := tuiHideCursor
	if w.visible {
		cursor = fmt.Sprintf("\x1b[%d;%dH%s%s", w.y+1, w.x+1,
			tuiCursorBlinkRestore, tuiShowCursor)
	}
	if err := writeTUIOutput(w.output, cursor); err != nil {
		return len(p), err
	}
	return len(p), nil
}

func (w *assetTUICursorWriter) consumeCursorMarker(output string) string {
	for {
		start := strings.Index(output, tuiCursorMarkerPrefix)
		if start < 0 {
			return output
		}
		valueStart := start + len(tuiCursorMarkerPrefix)
		end := strings.Index(output[valueStart:], tuiCursorMarkerEnd)
		if end < 0 {
			return output
		}
		end += valueStart
		var visible, x, y int
		if _, err := fmt.Sscanf(output[valueStart:end], "%d;%d;%d", &visible, &x, &y); err != nil {
			return output
		}
		w.visible = visible == 1
		w.x, w.y = max(0, x), max(0, y)
		output = output[:start] + output[end+len(tuiCursorMarkerEnd):]
	}
}

func writeTUIOutput(output io.Writer, value string) error {
	written, err := io.WriteString(output, value)
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}
