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

const (
	tuiCursorMarkerPrefix = "\x1b]99;koko-cursor;"
	tuiCursorMarkerEnd    = "\x07"
	tuiEnterAltScreen     = "\x1b[?1049h"
	tuiExitAltScreen      = "\x1b[?1049l"
	tuiShowCursor         = "\x1b[?25h"
	tuiHideCursor         = "\x1b[?25l"
)

const (
	tuiSelectedStyle         = "\x1b[7m"
	tuiHeaderStyle           = tuiSelectedStyle
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
	err         error
}

type assetChoicesMsg struct {
	asset     model.PermAsset
	accounts  []model.PermAccount
	protocols []string
	err       error
}

type assetTUIDialog struct {
	asset            model.PermAsset
	accounts         []model.PermAccount
	protocols        []string
	accountSearch    []rune
	accountMatches   []int
	searchingAccount bool
	accountIndex     int
	protocolIndex    int
}

type assetTUIConnection struct {
	asset    model.PermAsset
	account  model.PermAccount
	protocol string
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

	assets         []model.PermAsset
	connectable    []bool
	hasPrev        bool
	hasNext        bool
	query          string
	searchInput    []rune
	searching      bool
	loading        bool
	loadingChoices bool
	status         string
	switchText     bool
	resume         bool
	dialog         *assetTUIDialog
	connection     *assetTUIConnection
	lastClickRow   int
	lastClickAt    time.Time
}

func newAssetTUI(handler *InteractiveHandler) *assetTUI {
	width, height := handler.GetPtySize()
	model := &assetTUI{
		handler: handler, selector: handler.selectHandler,
		width: width, height: height,
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
		width := min(500, max(1, msg.Width))
		height := min(200, max(1, msg.Height))
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
			m.assets, m.connectable = nil, nil
			m.total, m.hasPrev, m.hasNext = 0, false, false
			m.status = userFacingErrorMessage(m.tr("资产加载失败", "Failed to load assets"), msg.err)
			return m, nil
		}
		m.assets = msg.assets
		m.connectable = msg.connectable
		m.offset, m.total = msg.offset, msg.total
		m.hasPrev, m.hasNext = msg.hasPrev, msg.hasNext
		m.cursor = 0
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
			m.status = m.tr("没有匹配的资产", "No matching assets")
		} else {
			m.status = ""
		}
	case assetChoicesMsg:
		m.loadingChoices = false
		if msg.err != nil {
			m.status = userFacingErrorMessage(m.tr("无法获取连接选项", "Failed to load connection options"), msg.err)
			return m, nil
		}
		m.status = ""
		m.dialog = newAssetTUIDialog(msg.asset, msg.accounts, msg.protocols)
	case tea.MouseMsg:
		return m.updateMouse(tea.MouseEvent(msg))
	case tea.KeyMsg:
		if m.dialog != nil {
			return m.updateDialogKey(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		if m.loading || m.loadingChoices {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				switch msg.Runes[0] {
				case 't', 'T':
					m.switchText = true
					return m, tea.Quit
				case 'q', 'Q':
					return m, tea.Quit
				}
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor+1 < len(m.assets) {
				m.cursor++
			}
		case tea.KeyHome:
			m.cursor = 0
		case tea.KeyEnd:
			if len(m.assets) > 0 {
				m.cursor = len(m.assets) - 1
			}
		case tea.KeyLeft, tea.KeyPgUp:
			if m.hasPrev {
				return m, m.loadPage(max(0, m.offset-m.pageSize))
			}
		case tea.KeyRight, tea.KeyPgDown:
			if m.hasNext {
				return m, m.loadPage(m.offset + len(m.assets))
			}
		case tea.KeyEnter:
			return m.openAssetDialog(m.cursor)
		case tea.KeyRunes:
			if len(msg.Runes) != 1 {
				return m, nil
			}
			switch msg.Runes[0] {
			case '/':
				m.searching = true
				m.searchInput = []rune(m.query)
				m.status = m.tr("输入关键字，回车搜索，Esc 取消", "Type a keyword, Enter to search, Esc to cancel")
			case 'k':
				if m.cursor > 0 {
					m.cursor--
				}
			case 'j':
				if m.cursor+1 < len(m.assets) {
					m.cursor++
				}
			case 'h':
				if m.hasPrev {
					return m, m.loadPage(max(0, m.offset-m.pageSize))
				}
			case 'l':
				if m.hasNext {
					return m, m.loadPage(m.offset + len(m.assets))
				}
			case 't', 'T':
				m.switchText = true
				return m, tea.Quit
			case 'q', 'Q':
				return m, tea.Quit
			case 'p', 'P':
				if m.hasPrev {
					return m, m.loadPage(max(0, m.offset-m.pageSize))
				}
			case 'n', 'N':
				if m.hasNext {
					return m, m.loadPage(m.offset + len(m.assets))
				}
			}
		}
	}
	return m, nil
}

func (m *assetTUI) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searching = false
		m.searchInput = nil
		m.status = ""
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
	case tea.KeyBackspace, tea.KeyDelete:
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

func (m *assetTUI) loadPage(offset int) tea.Cmd {
	m.loading = true
	m.status = m.tr("正在加载资产…", "Loading assets…")
	pageSize := m.pageSize
	query := m.query
	selector := m.selector
	return func() tea.Msg {
		var searches []string
		if query != "" {
			searches = []string{query}
		}
		assets := selector.Retrieve(pageSize, offset, searches...)
		connectable := append([]bool(nil), selector.connectable...)
		return assetPageMsg{
			assets: append([]model.PermAsset(nil), assets...), connectable: connectable,
			offset: offset, total: selector.TotalCount(), hasPrev: selector.HasPrev(),
			hasNext: selector.HasNext(), pageSize: pageSize, err: selector.loadErr,
		}
	}
}

func (m *assetTUI) openAssetDialog(index int) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.assets) || m.loadingChoices {
		return m, nil
	}
	if index >= len(m.connectable) || !m.connectable[index] {
		m.status = m.tr("该资产当前不可连接", "This asset is unavailable")
		return m, nil
	}
	m.loadingChoices = true
	m.status = m.tr("正在加载账号和协议…", "Loading accounts and protocols…")
	asset := m.assets[index]
	selector := m.selector
	mismatch := m.tr("资产信息不匹配", "Asset details do not match the selected asset")
	noChoice := m.tr("没有可用的授权账号或终端协议", "No permitted accounts or terminal protocols")
	return m, func() tea.Msg {
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
		text := strings.ToLower(account.Name + " " + account.Username + " " + account.Alias)
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

func (m *assetTUI) updateDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dialog := m.dialog
	if dialog.searchingAccount {
		return m.updateAccountSearch(msg)
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.dialog = nil
		m.status = ""
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
	case tea.KeyBackspace, tea.KeyDelete:
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
	m.connection = &assetTUIConnection{
		asset: dialog.asset, account: account,
		protocol: dialog.protocols[dialog.protocolIndex],
	}
	m.dialog = nil
	return m, tea.Quit
}

func (m *assetTUI) updateMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if m.dialog != nil {
		return m.updateDialogMouse(event)
	}
	if event.Button == tea.MouseButtonLeft && event.Y == 0 && event.X >= 0 && event.X < m.width {
		if !m.searching {
			m.searching = true
			m.searchInput = []rune(m.query)
			m.status = m.tr("输入关键字，回车搜索，Esc 取消", "Type a keyword, Enter to search, Esc to cancel")
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
		event.X < 0 || event.X >= m.width || event.Y < 2 {
		return m, nil
	}
	row := event.Y - 2
	if row < 0 || row >= len(m.assets) || row >= m.visibleAssetRows() {
		return m, nil
	}
	m.cursor = row
	now := time.Now()
	if m.lastClickRow == row && now.Sub(m.lastClickAt) <= tuiDoubleClickInterval {
		m.lastClickRow = -1
		m.lastClickAt = time.Time{}
		return m.openAssetDialog(row)
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
	lines[m.height-1] = m.footerLine()
	if m.height == 3 {
		lines[1] = tuiRightAlign(m.pageLine(), m.width)
		return m.viewWithCursor(lines)
	}

	columns := m.columns()
	lines[1] = tuiHeaderStyle + m.renderColumns(columns, nil, -1) + tuiStyleReset
	rowCount := min(len(m.assets), m.visibleAssetRows())
	for i := 0; i < rowCount; i++ {
		row := m.renderColumns(columns, &m.assets[i], i)
		if i == m.cursor {
			row = tuiSelectedStyle + row + tuiStyleReset
		} else if i >= len(m.connectable) || !m.connectable[i] {
			row = "\x1b[2m" + row + tuiStyleReset
		}
		lines[i+2] = row
	}
	if m.hasPagination() {
		lines[rowCount+2] = tuiRightAlign(m.pageLine(), m.width)
	}
	if m.dialog != nil {
		m.renderDialog(lines)
	}
	return m.viewWithCursor(lines)
}

func (m *assetTUI) viewWithCursor(lines []string) string {
	x, y, visible := m.terminalCursor()
	return fmt.Sprintf("%s%d;%d;%d%s", tuiCursorMarkerPrefix, boolInt(visible), x, y, tuiCursorMarkerEnd) +
		strings.Join(lines, "\n")
}

func (m *assetTUI) terminalCursor() (x, y int, visible bool) {
	if m.dialog != nil {
		geometry := m.dialogGeometry()
		if !m.dialog.searchingAccount || geometry.width < 36 || geometry.height < 10 ||
			geometry.y < 0 || geometry.y+geometry.height > max(1, m.height) {
			return 0, 0, false
		}
		_, cursor := tuiSearchField(m.tr("账号", "Account"), m.dialog.accountSearch, geometry.width-4)
		return geometry.x + 3 + cursor, geometry.y + 5, true
	}
	if !m.searching || m.height < 2 {
		return 0, 0, false
	}
	_, cursor := tuiSearchField(m.tr("我的资产", "My Assets"), m.searchInput, m.width)
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
	height := min(max(12, min(8, len(m.dialog.accounts))+9), max(1, m.height-2))
	return assetTUIDialogGeometry{
		x: (m.width - width) / 2, y: (m.height - height) / 2,
		width: width, height: height, rows: max(1, height-9),
	}
}

func (m *assetTUI) renderDialog(lines []string) {
	geometry := m.dialogGeometry()
	if geometry.width < 36 || geometry.height < 10 || geometry.y < 0 || geometry.y+geometry.height > len(lines) {
		return
	}
	dialog := m.dialog
	title := fmt.Sprintf("%s - %s", m.tr("连接", "Connect"), dialog.asset.Name)
	popup := tuiDialogFrame("", geometry.width, geometry.height)
	popup[1] = "│" + tuiDialogLeft(title, geometry.width-2) + "│"
	popup[2] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	protocols, _ := tuiDialogProtocolLine(dialog.protocols, dialog.protocolIndex, geometry.width-4)
	popup[3] = "│  " + protocols + "│"
	search := m.tr("账号", "Account")
	if len(dialog.accountSearch) > 0 || dialog.searchingAccount {
		search, _ = tuiSearchField(m.tr("账号", "Account"), dialog.accountSearch, geometry.width-4)
		popup[5] = "│  " + search + "│"
	} else {
		popup[5] = "│" + tuiDialogLeft(search, geometry.width-2) + "│"
	}
	accountStart := tuiDialogListStart(dialog.accountIndex, len(dialog.accountMatches), geometry.rows)
	for row := 0; row < geometry.rows; row++ {
		account := ""
		if position := accountStart + row; position < len(dialog.accountMatches) {
			item := dialog.accounts[dialog.accountMatches[position]]
			account = item.Username
			if item.Name != "" {
				account = item.Name + " (" + item.Username + ")"
			}
			if position == dialog.accountIndex {
				account = "▶ " + account
			} else {
				account = "  " + account
			}
		} else if row == 0 && len(dialog.accountMatches) == 0 {
			account = m.tr("没有匹配的账号", "No matching accounts")
		}
		popup[row+6] = "│" + tuiDialogLeft(account, geometry.width-2) + "│"
	}
	hint := m.tr("↑/↓ 账号  ·  ←/→ 协议  ·  / 搜索  ·  Enter 连接  ·  Esc 取消",
		"↑/↓ Account  ·  ←/→ Protocol  ·  / Search  ·  Enter Connect  ·  Esc Cancel")
	popup[geometry.height-2] = "│" + tuiDialogLeft(hint, geometry.width-2) + "│"
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
	for _, candidate := range []string{tuiSelectedStyle, "\x1b[2m"} {
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

func (m *assetTUI) updateDialogMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	geometry := m.dialogGeometry()
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
		m.dialog.accountIndex = max(0, min(len(m.dialog.accountMatches)-1, m.dialog.accountIndex+step))
		return m, nil
	}
	if event.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if event.Y == geometry.y+3 {
		_, hits := tuiDialogProtocolLine(m.dialog.protocols, m.dialog.protocolIndex, geometry.width-4)
		column := event.X - geometry.x - 3
		for _, hit := range hits {
			if column >= hit.start && column < hit.end {
				m.dialog.protocolIndex = hit.index
				m.dialog.searchingAccount = false
				break
			}
		}
		return m, nil
	}
	if event.Y == geometry.y+5 {
		m.dialog.searchingAccount = true
		return m, nil
	}
	row := event.Y - geometry.y - 6
	if row < 0 || row >= geometry.rows {
		return m, nil
	}
	m.dialog.searchingAccount = false
	start := tuiDialogListStart(m.dialog.accountIndex, len(m.dialog.accountMatches), geometry.rows)
	if position := start + row; position < len(m.dialog.accountMatches) {
		m.dialog.accountIndex = position
	}
	return m, nil
}

func (m *assetTUI) searchLine() string {
	label := m.tr("我的资产", "My Assets")
	if !m.searching {
		if m.query == "" {
			return tuiFit(label, m.width)
		}
		return tuiFit(label+": "+m.query, m.width)
	}
	line, _ := tuiSearchField(label, m.searchInput, m.width)
	return line
}

func tuiSearchField(label string, input []rune, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	prompt := label + ": "
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

func (m *assetTUI) footerLine() string {
	if m.searching {
		shortcuts := m.tr("Enter 搜索  Esc 取消  Backspace/Delete 删除  Ctrl+U 清空",
			"Enter Search  Esc Cancel  Backspace/Delete Delete  Ctrl+U Clear")
		return tuiFit(shortcuts, m.width)
	}
	shortcuts := m.tr("/ 搜索  t 纯文本  ↑/↓ 选择  双击/Enter 连接  ←/→ 翻页  q 退出",
		"/ Search  t Text mode  ↑/↓ Select  Double-click/Enter Connect  ←/→ Page  q Quit")
	if m.status != "" {
		shortcuts += "  ·  " + m.status
	}
	return tuiFit(shortcuts, m.width)
}

func (m *assetTUI) assetRows() int {
	return max(1, m.visibleAssetRows())
}

func (m *assetTUI) visibleAssetRows() int {
	rows := max(0, m.height-3)
	if m.hasPagination() {
		rows = max(0, rows-1)
	}
	return rows
}

func (m *assetTUI) hasPagination() bool {
	return m.total > max(1, m.height-3)
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
			case <-sess.Context().Done():
				return
			case <-routingDone:
				return
			}
		}
	}()

	handler := newInteractiveHandler(input, user, s.jmsService, termConf, false)
	handler.initializeAssetSelector()
	handler.selectHandler.SetSelectType(TypeAsset)
	model := newAssetTUI(handler)
	classicStarted := false
	for {
		err := runAssetTUI(handler, model)
		_ = input.Close()
		if err != nil {
			logger.Errorf("TUI session %s: %s", sess.User(), err)
			utils.IgnoreErrWriteString(sess, handler.tr("TUI 不可用，已切换到纯文本模式。", "TUI is unavailable; switched to text mode.")+utils.CharNewLine)
			model.switchText = true
		}
		if model.switchText {
			handler.displayHelp()
			if !classicStarted {
				handler.firstLoadData()
				classicStarted = true
			}
			handler.Dispatch()
			if !handler.switchTUI {
				return
			}
			handler.classicNavigation = false
			handler.treeOrigin = 0
			handler.selectHandler.SetSelectType(TypeAsset)
			model.switchText = false
			model.loading = false
			model.loadingChoices = false
			model.dialog = nil
			continue
		}
		if model.connection == nil {
			return
		}
		connection := *model.connection
		model.connection = nil
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
	output := &assetTUICursorWriter{output: handler.sess}
	program := tea.NewProgram(model,
		tea.WithInput(handler.sess),
		tea.WithOutput(output),
		tea.WithAltScreen(),
		tea.WithContext(handler.sess.Context()),
		tea.WithoutSignalHandler(),
		tea.WithMouseCellMotion(),
	)
	resizeDone := make(chan struct{})
	defer close(resizeDone)
	go func() {
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
		w.altScreen = true
	}
	if strings.Contains(output, tuiExitAltScreen) {
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
		cursor = fmt.Sprintf("\x1b[%d;%dH%s", w.y+1, w.x+1, tuiShowCursor)
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
