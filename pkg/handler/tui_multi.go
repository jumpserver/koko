package handler

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/LeeEirc/terminalparser"
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/mattn/go-runewidth"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

const (
	assetTUIMaxMultiSessions = 9
	assetTUIMultiChromeRows  = 4
	assetTUIMultiPrefix      = byte(0x02) // ctrl+b
	assetTUIMouseEnable      = "\x1b[?1000h\x1b[?1006h"
	assetTUIMouseDisable     = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l"
)

type assetTUIMultiTabHit struct {
	index       int
	start, end  int
	sessionList bool
}

type assetTUIMultiMouseEvent struct {
	button int
	x, y   int
	press  bool
}

type assetTUIMultiSessionListGeometry struct {
	left  int
	top   int
	width int
	rows  int
}

type assetTUIMultiSessionManager struct {
	handler  *InteractiveHandler
	physical *WrapperSession

	mu                 sync.RWMutex
	outputMu           sync.Mutex
	sessions           []*assetTUIMultiSession
	active             int
	viewActive         bool
	commandMode        bool
	helpVisible        bool
	helpScroll         int
	sessionListVisible bool
	sessionListIndex   int
	sessionListScroll  int
	closed             bool
	width              int
	height             int
}

type assetTUIMultiSession struct {
	id           string
	title        string
	connection   assetTUIConnection
	conn         *assetTUIMultiUserConnection
	screen       *terminalparser.TerminalVT
	scrollOffset int
	done         bool
	failed       bool
}

type assetTUIMultiUserConnection struct {
	manager *assetTUIMultiSessionManager
	session *assetTUIMultiSession
	parent  *WrapperSession
	input   chan []byte
	pending []byte
	ctx     context.Context
	cancel  context.CancelFunc

	closeOnce sync.Once
	winMu     sync.RWMutex
	window    ssh.Window
	winch     chan ssh.Window
}

func newAssetTUIMultiSessionManager(handler *InteractiveHandler, physical *WrapperSession) *assetTUIMultiSessionManager {
	width, height := handler.GetPtySize()
	return &assetTUIMultiSessionManager{
		handler: handler, physical: physical,
		width: max(1, width), height: max(1, height),
	}
}

func (m *assetTUIMultiSessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *assetTUIMultiSessionManager) Start(connection assetTUIConnection) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return io.ErrClosedPipe
	}
	if len(m.sessions) >= assetTUIMaxMultiSessions {
		m.mu.Unlock()
		return fmt.Errorf(m.handler.tr(
			"多会话最多同时打开 %d 个资产",
			"Multi-session mode supports at most %d assets",
		), assetTUIMaxMultiSessions)
	}
	width, height := m.width, assetTUIMultiContentHeight(m.height)
	screen, err := newAssetTUIMultiScreen(width, height)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	session := &assetTUIMultiSession{
		id: common.UUID(), title: connection.asset.Name,
		connection: connection, screen: screen,
	}
	session.conn = newAssetTUIMultiUserConnection(m, session, m.physical, ssh.Window{
		Width: width, Height: height,
	})
	m.sessions = append(m.sessions, session)
	m.active = len(m.sessions) - 1
	i18nLang := m.handler.i18nLang
	client := m.handler.assetClient(connection.asset.OrgID)
	m.mu.Unlock()

	go m.connect(session, client, i18nLang)
	return nil
}

func (m *assetTUIMultiSessionManager) connect(session *assetTUIMultiSession, client *service.JMService, i18nLang string) {
	defer session.conn.Close()
	connection := session.connection
	passwordKey := manualPasswordAttemptKey(connection.asset, connection.account, connection.protocol)
	lang := i18n.NewLang(i18nLang)
	passwordLimitMessage := lang.T("Manual password can be entered at most %d times")
	if lang == i18n.ZH {
		passwordLimitMessage = "手动密码最多允许输入 %d 次"
	}
	passwordLimitError := fmt.Errorf(passwordLimitMessage,
		maxManualPasswordAttempts)
	failure := ""
	shown := false
	if err := srvconn.IsSupportedProtocol(connection.protocol); err != nil {
		failure = err.Error()
	} else {
		_, failure, shown = connectSelectedAsset(
			session.conn, client, m.handler.user,
			connection.asset, connection.account, connection.protocol, i18nLang,
			func() error {
				if m.handler.manualPasswords.acquire(passwordKey) {
					return nil
				}
				return passwordLimitError
			},
		)
	}
	if failure != "" && !shown && m.hasSession(session) {
		m.appendOutput(session, []byte(utils.CharNewLine+utils.WrapperWarn(failure)))
	}
	m.finish(session, failure != "")
}

func (m *assetTUIMultiSessionManager) hasSession(session *assetTUIMultiSession) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, current := range m.sessions {
		if current == session {
			return true
		}
	}
	return false
}

func (m *assetTUIMultiSessionManager) finish(session *assetTUIMultiSession, failed bool) {
	m.mu.Lock()
	for _, current := range m.sessions {
		if current == session {
			current.done = true
			current.failed = failed
			break
		}
	}
	m.mu.Unlock()
	m.redrawChrome()
}

func (m *assetTUIMultiSessionManager) Run() error {
	if m.Count() == 0 {
		return nil
	}
	m.activate()
	defer m.deactivate()

	buffer := make([]byte, 8192)
	for {
		n, err := m.physical.Read(buffer)
		if n > 0 {
			pending := make([]byte, 0, n)
			flush := func() {
				if len(pending) == 0 {
					return
				}
				m.forward(pending)
				pending = pending[:0]
			}
			for index := 0; index < n; index++ {
				value := buffer[index]
				if event, consumed := assetTUIMultiMouse(buffer[index:n]); consumed > 0 {
					flush()
					index += consumed - 1
					m.handleMouse(event)
					continue
				}
				if m.isSessionListVisible() {
					flush()
					if direction, consumed := assetTUIMultiArrow(buffer[index:n]); consumed > 0 {
						index += consumed - 1
						switch direction {
						case 'u':
							m.moveSessionList(-1)
						case 'd':
							m.moveSessionList(1)
						}
					} else if value == 0x1b {
						m.closeSessionList()
					} else if value == '\r' || value == '\n' {
						m.activateSessionListSelection()
					} else if value == 'k' {
						m.moveSessionList(-1)
					} else if value == 'j' {
						m.moveSessionList(1)
					}
					continue
				}
				if m.isHelpVisible() {
					flush()
					if direction, consumed := assetTUIMultiArrow(buffer[index:n]); consumed > 0 {
						index += consumed - 1
						switch direction {
						case 'u':
							m.scrollHelp(-1)
						case 'd':
							m.scrollHelp(1)
						}
					} else if value == 0x1b {
						m.closeHelp()
					} else if value == 'k' {
						m.scrollHelp(-1)
					} else if value == 'j' {
						m.scrollHelp(1)
					}
					continue
				}
				if m.isCommandMode() {
					flush()
					if direction, consumed := assetTUIMultiArrow(buffer[index:n]); consumed > 0 {
						index += consumed - 1
						switch direction {
						case 'l':
							m.switchSession(-1)
						case 'r':
							m.switchSession(1)
						case 'u':
							m.scrollCurrent(1)
						case 'd':
							m.scrollCurrent(-1)
						}
						continue
					}
					leave, err := m.handleCommand(value)
					if err != nil {
						m.showCommandError(err)
					}
					if leave {
						return nil
					}
					continue
				}
				if value == assetTUIMultiPrefix {
					flush()
					m.setCommandMode(true)
					continue
				}
				pending = append(pending, value)
			}
			flush()
		}
		if err != nil {
			return err
		}
	}
}

func (m *assetTUIMultiSessionManager) handleMouse(event assetTUIMultiMouseEvent) {
	if !event.press {
		return
	}
	if m.isSessionListVisible() {
		m.handleSessionListMouse(event)
		return
	}
	if m.isHelpVisible() {
		return
	}
	m.mu.RLock()
	height := m.height
	m.mu.RUnlock()
	if event.button&64 != 0 {
		if event.y >= assetTUIMultiContentHeight(height) {
			return
		}
		if event.button&3 == 0 {
			m.scrollCurrent(3)
		} else if event.button&3 == 1 {
			m.scrollCurrent(-3)
		}
		return
	}
	if event.button&3 == 0 && event.y == height-3 {
		m.activateTabAt(event.x)
	}
}

func (m *assetTUIMultiSessionManager) handleSessionListMouse(event assetTUIMultiMouseEvent) {
	if event.button&64 != 0 {
		if event.button&3 == 0 {
			m.moveSessionList(-1)
		} else if event.button&3 == 1 {
			m.moveSessionList(1)
		}
		return
	}
	if event.button&3 != 0 {
		return
	}
	m.outputMu.Lock()
	m.mu.Lock()
	geometry, ok := assetTUIMultiSessionListLayout(m.width, m.height, len(m.sessions))
	terminalRow := event.y + 1
	terminalColumn := event.x + 1
	row := terminalRow - geometry.top - 3
	if ok && terminalColumn >= geometry.left && terminalColumn < geometry.left+geometry.width &&
		row >= 0 && row < geometry.rows {
		index := m.sessionListScroll + row
		if index < len(m.sessions) {
			m.sessionListIndex = index
		}
	}
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func assetTUIMultiArrow(data []byte) (direction byte, consumed int) {
	if len(data) < 3 || data[0] != 0x1b || data[1] != '[' && data[1] != 'O' {
		return 0, 0
	}
	switch data[2] {
	case 'A':
		return 'u', 3
	case 'B':
		return 'd', 3
	case 'C':
		return 'r', 3
	case 'D':
		return 'l', 3
	default:
		return 0, 0
	}
}

func assetTUIMultiMouse(data []byte) (assetTUIMultiMouseEvent, int) {
	if len(data) < 3 || data[0] != 0x1b || data[1] != '[' {
		return assetTUIMultiMouseEvent{}, 0
	}
	if data[2] == 'M' {
		if len(data) < 6 {
			return assetTUIMultiMouseEvent{}, 0
		}
		if data[3] < 32 || data[4] < 33 || data[5] < 33 {
			return assetTUIMultiMouseEvent{}, 6
		}
		button := int(data[3]) - 32
		return assetTUIMultiMouseEvent{
			button: button,
			x:      int(data[4]) - 33,
			y:      int(data[5]) - 33,
			press:  button&3 != 3,
		}, 6
	}
	if len(data) < 6 || data[2] != '<' {
		return assetTUIMultiMouseEvent{}, 0
	}
	end := -1
	for index := 3; index < min(len(data), 32); index++ {
		if data[index] == 'M' || data[index] == 'm' {
			end = index
			break
		}
	}
	if end < 0 {
		return assetTUIMultiMouseEvent{}, 0
	}
	parts := strings.Split(string(data[3:end]), ";")
	if len(parts) != 3 {
		return assetTUIMultiMouseEvent{}, end + 1
	}
	button, errButton := strconv.Atoi(parts[0])
	x, errX := strconv.Atoi(parts[1])
	y, errY := strconv.Atoi(parts[2])
	if errButton != nil || errX != nil || errY != nil || x <= 0 || y <= 0 {
		return assetTUIMultiMouseEvent{}, end + 1
	}
	return assetTUIMultiMouseEvent{
		button: button, x: x - 1, y: y - 1, press: data[end] == 'M',
	}, end + 1
}

func (m *assetTUIMultiSessionManager) isCommandMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.commandMode
}

func (m *assetTUIMultiSessionManager) isHelpVisible() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.helpVisible
}

func (m *assetTUIMultiSessionManager) isSessionListVisible() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionListVisible
}

func (m *assetTUIMultiSessionManager) setCommandMode(active bool) {
	m.mu.Lock()
	m.commandMode = active
	m.mu.Unlock()
	m.redrawChrome()
}

func (m *assetTUIMultiSessionManager) handleCommand(value byte) (bool, error) {
	switch {
	case value == 'a':
		return true, nil
	case value == 'q':
		m.closeAll()
		return true, nil
	case value == 's':
		m.openSessionList()
	case value == 0x1b || value == 'i':
		m.setCommandMode(false)
	case value == '\t':
		m.switchSession(1)
	case value == 'h':
		m.switchSession(-1)
	case value == 'l':
		m.switchSession(1)
	case value == 'k':
		m.scrollCurrent(1)
	case value == 'j':
		m.scrollCurrent(-1)
	case value >= '1' && value <= '9':
		m.switchSessionTo(int(value - '1'))
	case value == 'x':
		if m.closeCurrent() == 0 {
			return true, nil
		}
	case value == 'd':
		if err := m.duplicateCurrent(); err != nil {
			m.redrawChrome()
			return false, err
		}
	case value == 'r':
		if err := m.reconnectCurrent(); err != nil {
			m.redrawChrome()
			return false, err
		}
	case value == '?':
		m.openHelp()
	case value == assetTUIMultiPrefix:
		m.forward([]byte{assetTUIMultiPrefix})
		m.redrawChrome()
	default:
		m.redrawChrome()
	}
	return false, nil
}

func (m *assetTUIMultiSessionManager) openHelp() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.helpVisible = true
	m.helpScroll = 0
	m.sessionListVisible = false
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) openSessionList() {
	m.outputMu.Lock()
	m.mu.Lock()
	if len(m.sessions) == 0 {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return
	}
	m.sessionListVisible = true
	m.sessionListIndex = max(0, min(m.active, len(m.sessions)-1))
	m.sessionListScroll = 0
	m.helpVisible = false
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) closeSessionList() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.sessionListVisible = false
	m.commandMode = false
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) moveSessionList(delta int) {
	m.outputMu.Lock()
	m.mu.Lock()
	if len(m.sessions) > 0 {
		m.sessionListIndex = (m.sessionListIndex + delta%len(m.sessions) + len(m.sessions)) % len(m.sessions)
	}
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) activateSessionListSelection() {
	m.outputMu.Lock()
	m.mu.Lock()
	if m.sessionListIndex >= 0 && m.sessionListIndex < len(m.sessions) {
		m.active = m.sessionListIndex
	}
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) closeHelp() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.helpVisible = false
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) scrollHelp(delta int) {
	m.outputMu.Lock()
	rows := helpDialogRows(assetTUIMultiHelpRows(m.handler.tr))
	m.mu.Lock()
	visible := assetTUIMultiHelpVisibleRows(m.height, len(rows))
	m.helpScroll = max(0, min(max(0, len(rows)-visible), m.helpScroll+delta))
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) switchSessionTo(index int) {
	m.outputMu.Lock()
	m.mu.Lock()
	if index >= 0 && index < len(m.sessions) {
		m.active = index
	}
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) activateTabAt(column int) {
	m.outputMu.Lock()
	m.mu.Lock()
	_, hits := assetTUIMultiTabsLayout(m.width, m.sessions, m.active, m.commandMode)
	for _, hit := range hits {
		if column >= hit.start && column < hit.end {
			if hit.sessionList {
				m.sessionListVisible = true
				m.sessionListIndex = max(0, min(m.active, len(m.sessions)-1))
				m.sessionListScroll = 0
				m.helpVisible = false
			} else {
				m.active = hit.index
			}
			break
		}
	}
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) duplicateCurrent() error {
	m.mu.RLock()
	if len(m.sessions) == 0 {
		m.mu.RUnlock()
		return nil
	}
	connection := m.sessions[max(0, min(m.active, len(m.sessions)-1))].connection
	m.mu.RUnlock()
	if err := m.Start(connection); err != nil {
		return err
	}
	m.outputMu.Lock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
	return nil
}

func (m *assetTUIMultiSessionManager) reconnectCurrent() error {
	m.mu.RLock()
	if len(m.sessions) == 0 {
		m.mu.RUnlock()
		return nil
	}
	index := max(0, min(m.active, len(m.sessions)-1))
	oldSession := m.sessions[index]
	connection := oldSession.connection
	width, height := m.width, assetTUIMultiContentHeight(m.height)
	m.mu.RUnlock()

	screen, err := newAssetTUIMultiScreen(width, height)
	if err != nil {
		return err
	}
	session := &assetTUIMultiSession{
		id: common.UUID(), title: connection.asset.Name,
		connection: connection, screen: screen,
	}
	session.conn = newAssetTUIMultiUserConnection(m, session, m.physical, ssh.Window{
		Width: width, Height: height,
	})
	i18nLang := m.handler.i18nLang
	client := m.handler.assetClient(connection.asset.OrgID)

	m.outputMu.Lock()
	m.mu.Lock()
	if index >= len(m.sessions) || m.sessions[index] != oldSession {
		m.mu.Unlock()
		m.outputMu.Unlock()
		_ = session.conn.Close()
		_ = session.screen.Close()
		return nil
	}
	m.sessions[index] = session
	m.active = index
	m.mu.Unlock()
	_ = oldSession.conn.Close()
	_ = oldSession.screen.Close()
	m.renderCurrentLocked()
	m.outputMu.Unlock()

	go m.connect(session, client, i18nLang)
	return nil
}

func (m *assetTUIMultiSessionManager) scrollCurrent(delta int) {
	m.outputMu.Lock()
	m.mu.RLock()
	if len(m.sessions) == 0 {
		m.mu.RUnlock()
		m.outputMu.Unlock()
		return
	}
	session := m.sessions[max(0, min(m.active, len(m.sessions)-1))]
	height := assetTUIMultiContentHeight(m.height)
	m.mu.RUnlock()
	rows, _ := session.screen.ScreenRows()
	maxOffset := max(0, len(rows)-height)
	m.mu.Lock()
	session.scrollOffset = max(0, min(maxOffset, session.scrollOffset+delta))
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func newAssetTUIMultiScreen(width, height int) (*terminalparser.TerminalVT, error) {
	screen, err := terminalparser.New(
		terminalparser.WithSize(uint16(width), uint16(height)),
		terminalparser.WithMaxScrollback(200),
	)
	if err != nil {
		return nil, err
	}
	_, _ = screen.Write([]byte(utils.CharClear))
	return screen, nil
}

func assetTUIMultiViewport(rows []string, height, offset int) ([]string, int) {
	if height <= 0 {
		return nil, 0
	}
	maxOffset := max(0, len(rows)-height)
	offset = max(0, min(offset, maxOffset))
	start := max(0, maxOffset-offset)
	end := min(len(rows), start+height)
	return rows[start:end], offset
}

func (m *assetTUIMultiSessionManager) showCommandError(err error) {
	m.mu.RLock()
	if len(m.sessions) == 0 {
		m.mu.RUnlock()
		return
	}
	session := m.sessions[max(0, min(m.active, len(m.sessions)-1))]
	m.mu.RUnlock()
	m.appendOutput(session, []byte(utils.CharNewLine+utils.WrapperWarn(err.Error())))
}

func (m *assetTUIMultiSessionManager) Resize(window ssh.Window) {
	width := max(1, window.Width)
	height := max(1, window.Height)
	contentHeight := assetTUIMultiContentHeight(height)

	m.outputMu.Lock()
	m.mu.Lock()
	m.width, m.height = width, height
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	active := m.viewActive
	m.mu.Unlock()
	for _, session := range sessions {
		_ = session.screen.Resize(uint16(width), uint16(contentHeight), 0, 0)
		session.conn.setWindow(ssh.Window{Width: width, Height: contentHeight})
	}
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) Close() {
	m.outputMu.Lock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return
	}
	m.closed = true
	m.viewActive = false
	m.commandMode = false
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	sessions := m.sessions
	m.sessions = nil
	m.mu.Unlock()
	for _, session := range sessions {
		_ = session.conn.Close()
		_ = session.screen.Close()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) activate() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.viewActive = true
	m.commandMode = false
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) deactivate() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.viewActive = false
	m.commandMode = false
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	m.mu.Unlock()
	_, _ = io.WriteString(m.physical, assetTUIMouseDisable+tuiShowCursor+"\x1b[r"+tuiExitAltScreen)
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) switchSession(delta int) {
	m.outputMu.Lock()
	m.mu.Lock()
	if len(m.sessions) > 0 {
		m.active = (m.active + delta%len(m.sessions) + len(m.sessions)) % len(m.sessions)
	}
	m.mu.Unlock()
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) closeCurrent() int {
	m.outputMu.Lock()
	m.mu.Lock()
	if len(m.sessions) == 0 {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return 0
	}
	index := max(0, min(m.active, len(m.sessions)-1))
	session := m.sessions[index]
	m.sessions = append(m.sessions[:index], m.sessions[index+1:]...)
	if len(m.sessions) == 0 {
		m.active = 0
	} else if index >= len(m.sessions) {
		m.active = len(m.sessions) - 1
	}
	count := len(m.sessions)
	m.mu.Unlock()
	_ = session.conn.Close()
	_ = session.screen.Close()
	if count > 0 {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
	return count
}

func (m *assetTUIMultiSessionManager) closeAll() {
	m.outputMu.Lock()
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = nil
	m.active = 0
	m.commandMode = false
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	m.mu.Unlock()
	for _, session := range sessions {
		_ = session.conn.Close()
		_ = session.screen.Close()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) forward(data []byte) {
	m.mu.RLock()
	if len(m.sessions) == 0 {
		m.mu.RUnlock()
		return
	}
	session := m.sessions[max(0, min(m.active, len(m.sessions)-1))]
	m.mu.RUnlock()
	_ = session.conn.sendInput(data)
}

func (m *assetTUIMultiSessionManager) appendOutput(session *assetTUIMultiSession, data []byte) {
	m.mu.RLock()
	wasScrolled := session.scrollOffset > 0
	contentHeight := assetTUIMultiContentHeight(m.height)
	m.mu.RUnlock()
	beforeRows := 0
	if wasScrolled {
		rows, _ := session.screen.ScreenRows()
		beforeRows = len(rows)
	}
	_, _ = session.screen.Write(data)
	if wasScrolled {
		rows, _ := session.screen.ScreenRows()
		m.mu.Lock()
		delta := max(0, len(rows)-beforeRows)
		session.scrollOffset = min(max(0, len(rows)-contentHeight), session.scrollOffset+delta)
		m.mu.Unlock()
	}
	m.outputMu.Lock()
	m.mu.RLock()
	active := m.viewActive && !m.helpVisible && !m.sessionListVisible && session.scrollOffset == 0 &&
		len(m.sessions) > 0 && m.sessions[m.active] == session
	m.mu.RUnlock()
	if active {
		_, _ = m.physical.Write(data)
		m.renderChromeLocked(true)
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) redrawChrome() {
	m.outputMu.Lock()
	m.mu.RLock()
	active := m.viewActive
	m.mu.RUnlock()
	if active {
		m.renderChromeLocked(true)
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) renderCurrentLocked() {
	m.mu.RLock()
	if len(m.sessions) == 0 {
		m.mu.RUnlock()
		return
	}
	session := m.sessions[max(0, min(m.active, len(m.sessions)-1))]
	width, height := m.width, m.height
	scrollOffset := session.scrollOffset
	m.mu.RUnlock()

	rows, _ := session.screen.ScreenRows()
	cursorX, _ := session.screen.CursorX()
	cursorY, _ := session.screen.CursorY()
	contentHeight := assetTUIMultiContentHeight(height)
	visibleRows, scrollOffset := assetTUIMultiViewport(rows, contentHeight, scrollOffset)
	m.mu.Lock()
	session.scrollOffset = scrollOffset
	m.mu.Unlock()
	_, _ = io.WriteString(m.physical, tuiExitAltScreen+"\x1b[r"+utils.CharClear+
		assetTUIMultiScrollRegion(contentHeight))
	for index := range visibleRows {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H%s", index+1, tuiFit(visibleRows[index], width))
	}
	m.renderChromeLocked(false)
	if m.isSessionListVisible() {
		m.renderSessionListLocked()
		_, _ = io.WriteString(m.physical, tuiHideCursor)
		return
	}
	if m.isHelpVisible() {
		m.renderHelpLocked()
		_, _ = io.WriteString(m.physical, tuiHideCursor)
		return
	}
	if scrollOffset > 0 {
		_, _ = io.WriteString(m.physical, tuiHideCursor)
		return
	}
	_, _ = io.WriteString(m.physical, assetTUIMultiCursorPosition(width, contentHeight, cursorX, cursorY))
}

func assetTUIMultiScrollRegion(height int) string {
	if height < 2 {
		return ""
	}
	return fmt.Sprintf("\x1b[1;%dr", height)
}

func assetTUIMultiCursorPosition(width, height int, x, y uint16) string {
	return fmt.Sprintf("\x1b[%d;%dH%s",
		max(1, min(height, int(y)+1)), max(1, min(width, int(x)+1)), tuiShowCursor)
}

func (m *assetTUIMultiSessionManager) renderChromeLocked(preserveCursor bool) {
	m.mu.RLock()
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	active, width, height := m.active, m.width, m.height
	commandMode := m.commandMode
	helpVisible, sessionListVisible := m.helpVisible, m.sessionListVisible
	tabsLine := assetTUIMultiTabsLine(width, sessions, active, commandMode)
	scrollOffset := 0
	if active >= 0 && active < len(sessions) {
		scrollOffset = sessions[active].scrollOffset
	}
	m.mu.RUnlock()
	if len(sessions) == 0 {
		return
	}
	restoreCursor := ""
	if preserveCursor {
		restoreCursor = tuiHideCursor
		if !helpVisible && !sessionListVisible && scrollOffset == 0 && active >= 0 && active < len(sessions) {
			cursorX, errX := sessions[active].screen.CursorX()
			cursorY, errY := sessions[active].screen.CursorY()
			if errX == nil && errY == nil {
				restoreCursor = assetTUIMultiCursorPosition(
					width, assetTUIMultiContentHeight(height), cursorX, cursorY,
				)
			}
		}
		_, _ = io.WriteString(m.physical, "\x1b[?6l")
	}
	_, _ = io.WriteString(m.physical, assetTUIMultiScrollRegion(assetTUIMultiContentHeight(height)))
	_, _ = io.WriteString(m.physical, assetTUIMouseEnable)
	if height >= 4 {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K%s", height-3,
			strings.Repeat("─", width))
	}
	if height >= 3 {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K%s", height-2,
			tabsLine)
	}
	if height >= 2 {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K%s", height-1,
			strings.Repeat("─", width))
	}
	lang := m.handler.tr
	footer := assetTUIMultiFooterLine(width, commandMode, lang)
	_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K%s", height, tuiFit(footer, width))
	if preserveCursor {
		_, _ = io.WriteString(m.physical, restoreCursor)
	}
}

func assetTUIMultiFooterLine(width int, commandMode bool, tr func(string, string) string) string {
	if !commandMode {
		return tuiFit("ctrl+b:"+tr("激活快捷键", "Activate shortcuts"), width)
	}
	return tuiShortcutLine(width, []string{
		"esc:" + tr("进入会话", "Enter session"),
		"tab:" + tr("下一个会话", "Next session"),
		"d:" + tr("复制会话", "Duplicate session"),
		"r:" + tr("重连会话", "Reconnect session"),
		"x:" + tr("关闭会话", "Close session"),
		"q:" + tr("关闭所有会话", "Close all sessions"),
		"a:" + tr("资产列表", "Asset list"),
	}, "?:"+tr("查看帮助", "View help"))
}

func assetTUIMultiSessionListLayout(width, height, total int) (assetTUIMultiSessionListGeometry, bool) {
	if width < 16 || total <= 0 {
		return assetTUIMultiSessionListGeometry{}, false
	}
	bottom := assetTUIMultiContentHeight(height) - 1
	if bottom < 7 {
		return assetTUIMultiSessionListGeometry{}, false
	}
	popupWidth := min(width-1, 48)
	rows := min(total, bottom-6)
	popupHeight := rows + 6
	return assetTUIMultiSessionListGeometry{
		left:  max(1, width-popupWidth),
		top:   bottom - popupHeight + 1,
		width: popupWidth,
		rows:  rows,
	}, true
}

func (m *assetTUIMultiSessionManager) renderSessionListLocked() {
	m.mu.RLock()
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	width, height := m.width, m.height
	selected, scroll := m.sessionListIndex, m.sessionListScroll
	m.mu.RUnlock()
	geometry, ok := assetTUIMultiSessionListLayout(width, height, len(sessions))
	if !ok {
		return
	}
	selected = max(0, min(selected, len(sessions)-1))
	if selected < scroll {
		scroll = selected
	} else if selected >= scroll+geometry.rows {
		scroll = selected - geometry.rows + 1
	}
	scroll = max(0, min(scroll, len(sessions)-geometry.rows))
	m.mu.Lock()
	m.sessionListIndex = selected
	m.sessionListScroll = scroll
	m.mu.Unlock()

	lang := m.handler.tr
	lines := make([]string, 0, geometry.rows+6)
	lines = append(lines, "┌"+strings.Repeat("─", geometry.width-2)+"┐")
	lines = append(lines, "│"+tuiCenter(lang("会话列表", "Session list"), geometry.width-2)+"│")
	lines = append(lines, "├"+strings.Repeat("─", geometry.width-2)+"┤")
	contentWidth := geometry.width - 5
	showScrollbar := len(sessions) > geometry.rows
	for row := 0; row < geometry.rows; row++ {
		index := scroll + row
		session := sessions[index]
		state := ""
		if session.failed {
			state = " !"
		} else if session.done {
			state = " ×"
		}
		label := tuiFit(fmt.Sprintf("%d:%s%s", index+1, session.title, state), contentWidth)
		if index == selected {
			label = tuiSelectedStyle + label + tuiStyleReset
		}
		scrollbar := " "
		if showScrollbar {
			scrollbar = tuiScrollbarCell(row, scroll, len(sessions), geometry.rows)
		}
		lines = append(lines, "│  "+label+scrollbar+"│")
	}
	lines = append(lines, "├"+strings.Repeat("─", geometry.width-2)+"┤")
	shortcuts := "↑, ↓:" + lang("选择", "Select") + " · enter:" +
		lang("切换会话", "Switch session") + " · esc:" + lang("进入会话", "Enter session")
	lines = append(lines, "│  "+tuiFit(shortcuts, geometry.width-4)+"│")
	lines = append(lines, "└"+strings.Repeat("─", geometry.width-2)+"┘")
	for index, line := range lines {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;%dH%s", geometry.top+index, geometry.left, line)
	}
}

func (m *assetTUIMultiSessionManager) renderHelpLocked() {
	m.mu.RLock()
	width, height, helpScroll := m.width, m.height, m.helpScroll
	m.mu.RUnlock()
	if width < 20 || height <= assetTUIMultiChromeRows+4 {
		return
	}
	lang := m.handler.tr
	rows := assetTUIMultiHelpRows(lang)
	displayRows := helpDialogRows(rows)
	popupWidth := min(width-1, 64)
	keyWidth := 0
	for _, row := range rows {
		keyWidth = max(keyWidth, runewidth.StringWidth(row.key))
	}
	keyWidth = min(keyWidth, max(1, popupWidth/3))
	descriptionWidth := max(0, popupWidth-keyWidth-8)
	bottom := assetTUIMultiContentHeight(height) - 1
	visibleRows := assetTUIMultiHelpVisibleRows(height, len(displayRows))
	if visibleRows == 0 {
		return
	}
	helpScroll = max(0, min(max(0, len(displayRows)-visibleRows), helpScroll))
	popupHeight := visibleRows + 6
	top := max(1, bottom-popupHeight+1)
	left := max(1, width-popupWidth)
	lines := make([]string, 0, popupHeight)
	lines = append(lines, "┌"+strings.Repeat("─", popupWidth-2)+"┐")
	lines = append(lines, "│"+tuiCenter(lang("查看帮助", "View help"), popupWidth-2)+"│")
	lines = append(lines, "├"+strings.Repeat("─", popupWidth-2)+"┤")
	showScrollbar := len(displayRows) > visibleRows
	for index := 0; index < visibleRows; index++ {
		row := assetTUIHelpRow{}
		if position := helpScroll + index; position < len(displayRows) {
			row = displayRows[position]
		}
		scrollbar := " "
		if showScrollbar {
			scrollbar = tuiScrollbarCell(index, helpScroll, len(displayRows), visibleRows)
		}
		if row.separator {
			lines = append(lines, "│  "+strings.Repeat(" ", keyWidth)+" │ "+
				strings.Repeat(" ", descriptionWidth)+scrollbar+"│")
			continue
		}
		lines = append(lines, "│  "+tuiFit(row.key, keyWidth)+" │ "+
			tuiFit(row.description, descriptionWidth)+scrollbar+"│")
	}
	lines = append(lines, "├"+strings.Repeat("─", popupWidth-2)+"┤")
	lines = append(lines, "│  "+tuiFit("esc:"+lang("取消", "Cancel"), popupWidth-4)+"│")
	lines = append(lines, "└"+strings.Repeat("─", popupWidth-2)+"┘")
	for index, line := range lines {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;%dH%s", top+index, left, line)
	}
}

func assetTUIMultiHelpRows(tr func(string, string) string) []assetTUIHelpRow {
	row := func(key, zh, en string) assetTUIHelpRow {
		return assetTUIHelpRow{key: key, description: tr(zh, en)}
	}
	return []assetTUIHelpRow{
		row("tab", "切换到下一个会话", "Move to the next session"),
		row("s", "打开会话列表", "Open the session list"),
		row("←, →, h, l", "切换到左侧或右侧会话", "Move to the previous or next session"),
		row("1-9", "按编号切换到对应会话", "Switch directly to session 1-9"),
		row("↑, ↓, j, k", "向下或向上滚动当前会话内容", "Scroll the current session down or up"),
		row("d", "复制当前会话", "Duplicate the current session"),
		row("r", "重新连接当前会话", "Reconnect the current session"),
		row("a", "返回资产列表", "Return to the asset list"),
		row("x", "关闭当前会话", "Close the current session"),
		row("q", "关闭所有会话并返回资产列表", "Close all sessions and return to the asset list"),
		row("ctrl+b, ctrl+b", "向当前会话发送 ctrl+b", "Send ctrl+b to the current session"),
		row("?", "打开快捷键帮助", "Open shortcut help"),
		row("esc, i", "进入当前会话", "Enter the current session"),
	}
}

func assetTUIMultiHelpVisibleRows(height, total int) int {
	bottom := assetTUIMultiContentHeight(height) - 1
	return min(total, max(0, bottom-6))
}

func assetTUIMultiTabsLine(width int, sessions []*assetTUIMultiSession, active int, showNumbers bool) string {
	line, _ := assetTUIMultiTabsLayout(width, sessions, active, showNumbers)
	return line
}

func assetTUIMultiTabsLayout(width int, sessions []*assetTUIMultiSession, active int,
	showNumbers bool) (string, []assetTUIMultiTabHit) {
	if width <= 0 || len(sessions) == 0 {
		return "", nil
	}
	active = max(0, min(active, len(sessions)-1))
	labels := make([]string, len(sessions))
	for index, session := range sessions {
		state := ""
		if session.failed {
			state = " !"
		} else if session.done {
			state = " ×"
		}
		labels[index] = session.title + state
		if showNumbers {
			labels[index] = fmt.Sprintf("%d:%s", index+1, labels[index])
		}
		labels[index] = strings.TrimRight(tuiFit(labels[index], 24), " ")
	}

	start, end := active, active+1
	bestCount, bestBalance, bestWidth := 0, len(sessions)+1, 0
	for candidateStart := 0; candidateStart <= active; candidateStart++ {
		for candidateEnd := active + 1; candidateEnd <= len(sessions); candidateEnd++ {
			candidateWidth := assetTUIMultiTabsRangeWidth(labels, candidateStart, candidateEnd)
			if candidateWidth > width {
				continue
			}
			count := candidateEnd - candidateStart
			balance := (active - candidateStart) - (candidateEnd - active - 1)
			if balance < 0 {
				balance = -balance
			}
			if count > bestCount || count == bestCount &&
				(balance < bestBalance || balance == bestBalance && candidateWidth > bestWidth) {
				start, end = candidateStart, candidateEnd
				bestCount, bestBalance, bestWidth = count, balance, candidateWidth
			}
		}
	}

	leftMarker, rightMarker := "", ""
	if start > 0 {
		leftMarker = fmt.Sprintf("‹%d", start)
	}
	if end < len(sessions) {
		rightMarker = fmt.Sprintf("%d›", len(sessions)-end)
	}
	requiredWidth := assetTUIMultiTabsRangeWidth(labels, start, end)
	if requiredWidth > width {
		activeWidth := runewidth.StringWidth(labels[active])
		available := width - (requiredWidth - activeWidth)
		if available <= 0 {
			start, end = active, active+1
			leftMarker, rightMarker = "", ""
			labels[active] = strings.TrimRight(tuiFit(labels[active], width), " ")
		} else {
			labels[active] = strings.TrimRight(tuiFit(labels[active], available), " ")
		}
	}

	type tabCell struct {
		label       string
		index       int
		sessionList bool
	}
	cells := make([]tabCell, 0, end-start+2)
	if leftMarker != "" {
		cells = append(cells, tabCell{label: leftMarker, index: -1, sessionList: true})
	}
	for index := start; index < end; index++ {
		cells = append(cells, tabCell{label: labels[index], index: index})
	}
	if rightMarker != "" {
		cells = append(cells, tabCell{label: rightMarker, index: -1, sessionList: true})
	}

	const separator = " ｜ "
	var result strings.Builder
	hits := make([]assetTUIMultiTabHit, 0, len(cells))
	visibleWidth := 0
	for cellIndex, cell := range cells {
		if cellIndex > 0 {
			result.WriteString(separator)
			visibleWidth += runewidth.StringWidth(separator)
		}
		labelWidth := runewidth.StringWidth(cell.label)
		hits = append(hits, assetTUIMultiTabHit{
			index: cell.index, start: visibleWidth, end: visibleWidth + labelWidth,
			sessionList: cell.sessionList,
		})
		if cell.index == active {
			result.WriteString(tuiSelectedStyle)
			result.WriteString(cell.label)
			result.WriteString(tuiStyleReset)
		} else {
			result.WriteString(cell.label)
		}
		visibleWidth += labelWidth
	}
	if visibleWidth < width {
		result.WriteString(strings.Repeat(" ", width-visibleWidth))
	}
	return result.String(), hits
}

func assetTUIMultiTabsRangeWidth(labels []string, start, end int) int {
	separatorWidth := runewidth.StringWidth(" ｜ ")
	cellCount := end - start
	width := 0
	if start > 0 {
		width += runewidth.StringWidth(fmt.Sprintf("‹%d", start))
		cellCount++
	}
	for index := start; index < end; index++ {
		width += runewidth.StringWidth(labels[index])
	}
	if end < len(labels) {
		width += runewidth.StringWidth(fmt.Sprintf("%d›", len(labels)-end))
		cellCount++
	}
	if cellCount > 1 {
		width += separatorWidth * (cellCount - 1)
	}
	return width
}

func assetTUIMultiContentHeight(height int) int {
	return max(1, height-assetTUIMultiChromeRows)
}

func newAssetTUIMultiUserConnection(manager *assetTUIMultiSessionManager, session *assetTUIMultiSession,
	parent *WrapperSession, window ssh.Window) *assetTUIMultiUserConnection {
	ctx, cancel := context.WithCancel(parent.Context())
	return &assetTUIMultiUserConnection{
		manager: manager, session: session, parent: parent,
		input: make(chan []byte, 64), ctx: ctx, cancel: cancel,
		window: window, winch: make(chan ssh.Window, 1),
	}
}

func (c *assetTUIMultiUserConnection) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	for len(c.pending) == 0 {
		if c.ctx.Err() != nil {
			return 0, io.EOF
		}
		select {
		case <-c.ctx.Done():
			return 0, io.EOF
		case c.pending = <-c.input:
		}
	}
	n := copy(data, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *assetTUIMultiUserConnection) Write(data []byte) (int, error) {
	if c.ctx.Err() != nil {
		return 0, io.ErrClosedPipe
	}
	c.manager.appendOutput(c.session, data)
	return len(data), nil
}

func (c *assetTUIMultiUserConnection) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
	})
	return nil
}

func (c *assetTUIMultiUserConnection) sendInput(data []byte) error {
	if c.ctx.Err() != nil {
		return io.ErrClosedPipe
	}
	input := append([]byte(nil), data...)
	select {
	case <-c.ctx.Done():
		return io.ErrClosedPipe
	case c.input <- input:
		return nil
	}
}

func (c *assetTUIMultiUserConnection) ID() string {
	return c.session.id
}

func (c *assetTUIMultiUserConnection) WinCh() <-chan ssh.Window {
	return c.winch
}

func (c *assetTUIMultiUserConnection) LoginFrom() string {
	return c.parent.LoginFrom()
}

func (c *assetTUIMultiUserConnection) RemoteAddr() string {
	return c.parent.RemoteAddr()
}

func (c *assetTUIMultiUserConnection) Pty() ssh.Pty {
	pty := c.parent.Pty()
	c.winMu.RLock()
	pty.Window = c.window
	c.winMu.RUnlock()
	return pty
}

func (c *assetTUIMultiUserConnection) Context() context.Context {
	return c.ctx
}

func (c *assetTUIMultiUserConnection) HandleRoomEvent(event string, message *exchange.RoomMessage) {
	c.parent.HandleRoomEvent(event, message)
}

func (c *assetTUIMultiUserConnection) setWindow(window ssh.Window) {
	c.winMu.Lock()
	c.window = window
	c.winMu.Unlock()
	select {
	case c.winch <- window:
	default:
		select {
		case <-c.winch:
		default:
		}
		select {
		case c.winch <- window:
		default:
		}
	}
}
