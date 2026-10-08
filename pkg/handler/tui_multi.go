package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LeeEirc/terminalparser"
	"github.com/charmbracelet/x/ansi"
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/mattn/go-runewidth"
	"go.mitchellh.com/libghostty"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

const (
	assetTUIMaxMultiSessions    = 9
	assetTUIMultiChromeRows     = 3
	assetTUIMultiContentTop     = 1
	assetTUIMultiPrefix         = byte(0x02) // ctrl+b
	assetTUIMouseEnable         = "\x1b[?1000h\x1b[?1006h"
	assetTUIMouseDisable        = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l"
	assetTUIMultiClearHistory   = "\x1b[3J"
	assetTUIMultiHintDuration   = 5 * time.Second
	assetTUIMultiNoticeDuration = 3 * time.Second
	assetTUIMultiMaxScrollback  = 200
	assetTUIMultiClearScanLimit = 8192
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

type assetTUIMultiConfirmAction uint8

const (
	assetTUIMultiConfirmNone assetTUIMultiConfirmAction = iota
	assetTUIMultiConfirmCloseCurrent
	assetTUIMultiConfirmCloseAll
	assetTUIMultiConfirmCloseEnded
)

type assetTUIMultiSessionManager struct {
	handler  *InteractiveHandler
	physical *WrapperSession

	mu                 sync.RWMutex
	outputMu           sync.Mutex
	sessions           []*assetTUIMultiSession
	active             int
	viewActive         bool
	commandMode        bool
	immersive          bool
	immersiveHint      bool
	immersiveHintID    uint64
	helpVisible        bool
	helpScroll         int
	sessionListVisible bool
	sessionListIndex   int
	sessionListScroll  int
	confirmAction      assetTUIMultiConfirmAction
	notice             string
	noticeID           uint64
	closed             bool
	width              int
	height             int
}

type assetTUIMultiSession struct {
	id                    string
	title                 string
	connection            assetTUIConnection
	conn                  *assetTUIMultiUserConnection
	screen                *assetTUIMultiScreen
	scrollOffset          int
	outputStarted         bool
	initialOutputRendered bool
	done                  bool
	failed                bool
}

type assetTUIMultiScreen struct {
	mu              sync.Mutex
	terminal        *terminalparser.TerminalVT
	styledTerminal  *libghostty.Terminal
	styledFormatter *libghostty.Formatter
	clearedRows     []string
	clearedStyled   []string
	preserveClear   bool
	clearTail       []byte
	clearRemaining  int
	clearGeneration uint64
	closed          bool
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
	window := physical.Pty().Window
	width, height := assetTUITerminalSize(window.Width, window.Height)
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

func (m *assetTUIMultiSessionManager) UnfinishedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, session := range m.sessions {
		if !session.done {
			count++
		}
	}
	return count
}

func (m *assetTUIMultiSessionManager) Start(connection assetTUIConnection) error {
	m.outputMu.Lock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return io.ErrClosedPipe
	}
	if len(m.sessions) >= assetTUIMaxMultiSessions {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return fmt.Errorf(m.handler.tr(
			"多会话模式最多支持同时打开 %d 个会话窗口",
			"Multi-session mode supports at most %d session windows",
		), assetTUIMaxMultiSessions)
	}
	width, height := m.width, assetTUIMultiSessionHeight(m.height, m.immersive, m.commandMode)
	screen, err := newAssetTUIMultiScreen(width, height)
	if err != nil {
		m.mu.Unlock()
		m.outputMu.Unlock()
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
	viewActive := m.viewActive
	i18nLang := m.handler.i18nLang
	client := m.handler.assetClient(connection.asset.OrgID)
	m.mu.Unlock()
	if viewActive {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()

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
	showConfirmation := m.markFinished(session, failed)
	if showConfirmation && m.isImmersive() {
		m.exitImmersiveMode()
		return
	}
	if showConfirmation {
		m.resizeAndRenderCurrent()
		return
	}
	m.redrawChrome()
}

func (m *assetTUIMultiSessionManager) markFinished(session *assetTUIMultiSession, failed bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index, current := range m.sessions {
		if current == session {
			current.done = true
			current.failed = failed
			if !failed && index == m.active && m.viewActive &&
				m.confirmAction == assetTUIMultiConfirmNone {
				m.confirmAction = assetTUIMultiConfirmCloseEnded
				m.commandMode = true
				m.helpVisible = false
				m.sessionListVisible = false
				return true
			}
			return false
		}
	}
	return false
}

func (m *assetTUIMultiSessionManager) Run(startInCommandMode bool) error {
	if m.Count() == 0 {
		return nil
	}
	m.activate(startInCommandMode)
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
					if m.isCommandMode() {
						flush()
						m.handleMouse(event)
					} else {
						pending = append(pending, buffer[index:index+consumed]...)
					}
					index += consumed - 1
					continue
				}
				if m.isImmersive() {
					if value == assetTUIMultiPrefix {
						flush()
						m.exitImmersiveMode()
					} else {
						pending = append(pending, value)
					}
					continue
				}
				if m.isConfirmationVisible() {
					flush()
					if m.handleConfirmationKey(value) {
						return nil
					}
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
						m.closeSessionList(false)
					} else if value == '\r' || value == '\n' {
						m.closeSessionList(true)
					} else if value >= '1' && value <= '9' {
						m.selectSessionList(int(value - '1'))
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
					if consumed := assetTUIMultiShiftTab(buffer[index:n]); consumed > 0 {
						index += consumed - 1
						m.switchSession(-1)
						continue
					}
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
	if !event.press || !m.isCommandMode() {
		return
	}
	if m.isConfirmationVisible() {
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
	immersive := m.immersive
	m.mu.RUnlock()
	if event.button&64 != 0 {
		if !assetTUIMultiSessionContains(height, event.y, immersive) {
			return
		}
		if event.button&3 == 0 {
			m.scrollCurrent(3)
		} else if event.button&3 == 1 {
			m.scrollCurrent(-3)
		}
		return
	}
	if event.button&3 != 0 {
		return
	}
	if !immersive && event.y == height-2 {
		m.activateTabAt(event.x)
		return
	}
	if !immersive && assetTUIMultiContentContains(height, event.y) && m.isCommandMode() {
		m.setCommandMode(false)
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
	insidePopup := ok && terminalColumn >= geometry.left &&
		terminalColumn < geometry.left+geometry.width && terminalRow >= geometry.top &&
		terminalRow < geometry.top+geometry.rows+6
	if !insidePopup {
		m.sessionListVisible = false
		active := m.viewActive
		m.mu.Unlock()
		if active {
			m.renderCurrentLocked()
		}
		m.outputMu.Unlock()
		return
	}
	row := terminalRow - geometry.top - 3
	if row >= 0 && row < geometry.rows {
		index := m.sessionListScroll + row
		if index < len(m.sessions) {
			m.sessionListIndex = index
			m.active = index
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

func assetTUIMultiShiftTab(data []byte) int {
	for _, sequence := range [][]byte{
		{0x1b, '[', 'Z'},
		{0x1b, '[', '1', ';', '2', 'Z'},
		{0x9b, 'Z'},
	} {
		if len(data) >= len(sequence) && bytes.Equal(data[:len(sequence)], sequence) {
			return len(sequence)
		}
	}
	return 0
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

func (m *assetTUIMultiSessionManager) isImmersive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.immersive
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

func (m *assetTUIMultiSessionManager) isConfirmationVisible() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.confirmAction != assetTUIMultiConfirmNone
}

func (m *assetTUIMultiSessionManager) setCommandMode(active bool) {
	m.outputMu.Lock()
	m.mu.Lock()
	m.commandMode = active
	if !active && len(m.sessions) > 0 {
		m.sessions[max(0, min(m.active, len(m.sessions)-1))].scrollOffset = 0
	}
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	width := m.width
	height := assetTUIMultiSessionHeight(m.height, m.immersive, m.commandMode)
	viewActive := m.viewActive
	m.mu.Unlock()
	resizeAssetTUIMultiSessions(sessions, width, height)
	if viewActive {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) enterImmersiveMode() {
	m.outputMu.Lock()
	m.mu.Lock()
	if m.immersive || len(m.sessions) == 0 {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return
	}
	m.immersive = true
	m.immersiveHint = true
	m.immersiveHintID++
	hintID := m.immersiveHintID
	m.commandMode = false
	m.sessions[max(0, min(m.active, len(m.sessions)-1))].scrollOffset = 0
	m.helpVisible = false
	m.sessionListVisible = false
	m.confirmAction = assetTUIMultiConfirmNone
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	width, height := m.width, m.height
	active := m.viewActive
	m.mu.Unlock()
	resizeAssetTUIMultiSessions(sessions, width, height)
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()

	time.AfterFunc(assetTUIMultiHintDuration, func() {
		m.hideImmersiveHint(hintID)
	})
}

func (m *assetTUIMultiSessionManager) exitImmersiveMode() {
	m.outputMu.Lock()
	m.mu.Lock()
	if !m.immersive {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return
	}
	m.immersive = false
	m.immersiveHint = false
	m.immersiveHintID++
	m.commandMode = true
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	width, height := m.width, assetTUIMultiSessionHeight(m.height, false, m.commandMode)
	active := m.viewActive
	m.mu.Unlock()
	resizeAssetTUIMultiSessions(sessions, width, height)
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) hideImmersiveHint(hintID uint64) {
	m.outputMu.Lock()
	m.mu.Lock()
	if !m.immersive || !m.immersiveHint || m.immersiveHintID != hintID {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return
	}
	m.immersiveHint = false
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.restoreImmersiveHintLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) handleCommand(value byte) (bool, error) {
	switch {
	case value == 'a':
		return true, nil
	case value == 'q':
		m.openConfirmation(assetTUIMultiConfirmCloseAll)
	case value == 's':
		m.openSessionList()
	case value == 0x1b || value == 'i' || value == '\r' || value == '\n':
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
		m.openConfirmation(assetTUIMultiConfirmCloseCurrent)
	case value == 'd':
		if m.Count() >= assetTUIMaxMultiSessions {
			m.showNotice(fmt.Sprintf(m.handler.tr(
				"多会话模式最多支持同时打开 %d 个会话窗口",
				"Multi-session mode supports at most %d session windows",
			), assetTUIMaxMultiSessions))
			break
		}
		if err := m.duplicateCurrent(); err != nil {
			m.redrawChrome()
			return false, err
		}
	case value == 'r':
		if err := m.reconnectCurrent(); err != nil {
			m.redrawChrome()
			return false, err
		}
	case value == 'z':
		m.enterImmersiveMode()
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

func (m *assetTUIMultiSessionManager) openConfirmation(action assetTUIMultiConfirmAction) {
	m.outputMu.Lock()
	m.mu.Lock()
	m.confirmAction = action
	m.helpVisible = false
	m.sessionListVisible = false
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) handleConfirmationKey(value byte) bool {
	if value == 0x1b {
		m.closeConfirmation()
		return false
	}
	if value != '\r' && value != '\n' {
		return false
	}

	m.mu.Lock()
	action := m.confirmAction
	m.confirmAction = assetTUIMultiConfirmNone
	m.mu.Unlock()
	switch action {
	case assetTUIMultiConfirmCloseCurrent, assetTUIMultiConfirmCloseEnded:
		return m.closeCurrent() == 0
	case assetTUIMultiConfirmCloseAll:
		m.closeAll()
		return true
	default:
		return false
	}
}

func (m *assetTUIMultiSessionManager) closeConfirmation() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.confirmAction = assetTUIMultiConfirmNone
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) openHelp() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.helpVisible = true
	m.helpScroll = 0
	m.sessionListVisible = false
	m.confirmAction = assetTUIMultiConfirmNone
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
	m.confirmAction = assetTUIMultiConfirmNone
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) closeSessionList(enterSession bool) {
	m.outputMu.Lock()
	m.mu.Lock()
	m.sessionListVisible = false
	if enterSession {
		m.commandMode = false
		if len(m.sessions) > 0 {
			m.sessions[max(0, min(m.active, len(m.sessions)-1))].scrollOffset = 0
		}
	}
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	width := m.width
	height := assetTUIMultiSessionHeight(m.height, m.immersive, m.commandMode)
	active := m.viewActive
	m.mu.Unlock()
	if enterSession {
		resizeAssetTUIMultiSessions(sessions, width, height)
	}
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) moveSessionList(delta int) {
	m.outputMu.Lock()
	m.mu.Lock()
	if len(m.sessions) > 0 {
		m.sessionListIndex = tuiBoundedSelection(m.sessionListIndex, delta, len(m.sessions))
		m.active = m.sessionListIndex
	}
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) selectSessionList(index int) {
	m.outputMu.Lock()
	m.mu.Lock()
	if index >= 0 && index < len(m.sessions) {
		m.sessionListIndex = index
		m.active = index
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
	_, hits := assetTUIMultiTabsLayout(m.width, m.sessions, m.active)
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
	width, height := m.width, assetTUIMultiSessionHeight(m.height, m.immersive, m.commandMode)
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
	width := m.width
	height := m.height
	immersive := m.immersive
	commandMode := m.commandMode
	contentHeight := assetTUIMultiSessionHeight(height, immersive, commandMode)
	hint := m.immersiveHint
	active := m.viewActive && !m.helpVisible && !m.sessionListVisible
	oldOffset := session.scrollOffset
	m.mu.RUnlock()
	rows := session.screen.RenderRows(contentHeight)
	maxOffset := max(0, len(rows)-contentHeight)
	newOffset := max(0, min(maxOffset, oldOffset+delta))
	m.mu.Lock()
	session.scrollOffset = newOffset
	m.mu.Unlock()
	if active && newOffset != oldOffset {
		if immersive && hint {
			m.renderCurrentLocked()
			m.outputMu.Unlock()
			return
		}
		_, _ = io.WriteString(m.physical, assetTUIMultiScrollViewport(
			rows, width, height, oldOffset, newOffset, immersive, commandMode,
		))
		if newOffset > 0 {
			_, _ = io.WriteString(m.physical, tuiHideCursor)
		} else {
			cursorX, cursorY, _ := session.screen.Cursor()
			_, _ = io.WriteString(m.physical,
				assetTUIMultiCursorRestore(width, contentHeight, cursorX, cursorY,
					assetTUIMultiSessionCursorVisible(commandMode, newOffset)))
		}
	}
	m.outputMu.Unlock()
}

func newAssetTUIMultiScreen(width, height int) (*assetTUIMultiScreen, error) {
	terminal, err := terminalparser.New(
		terminalparser.WithSize(uint16(width), uint16(height)),
		terminalparser.WithMaxScrollback(assetTUIMultiMaxScrollback),
	)
	if err != nil {
		return nil, err
	}
	styledTerminal, err := libghostty.NewTerminal(
		libghostty.WithSize(uint16(width), uint16(height)),
		libghostty.WithMaxScrollback(assetTUIMultiMaxScrollback),
	)
	if err != nil {
		_ = terminal.Close()
		return nil, err
	}
	styledFormatter, err := libghostty.NewFormatter(styledTerminal,
		libghostty.WithFormatterFormat(libghostty.FormatterFormatVT),
		libghostty.WithFormatterTrim(true),
	)
	if err != nil {
		_ = terminal.Close()
		styledTerminal.Close()
		return nil, err
	}
	screen := &assetTUIMultiScreen{
		terminal: terminal, styledTerminal: styledTerminal, styledFormatter: styledFormatter,
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

func assetTUIMultiScrollViewport(rows []string, width, terminalHeight, oldOffset, newOffset int,
	immersive, commandMode bool,
) string {
	height := assetTUIMultiSessionHeight(terminalHeight, immersive, commandMode)
	if width <= 0 || terminalHeight <= 0 || oldOffset == newOffset {
		return ""
	}
	visibleRows, newOffset := assetTUIMultiViewport(rows, height, newOffset)
	_, oldOffset = assetTUIMultiViewport(rows, height, oldOffset)
	shift := newOffset - oldOffset
	if shift == 0 {
		return ""
	}

	var output strings.Builder
	output.WriteString(tuiHideCursor)
	if immersive {
		output.WriteString("\x1b[?6l\x1b[r")
	} else {
		output.WriteString("\x1b[?6l")
		output.WriteString(assetTUIMultiScrollRegion(terminalHeight, commandMode))
		output.WriteString("\x1b[?6h")
	}
	writeRow := func(row int, value string) {
		_, _ = fmt.Fprintf(&output, "\x1b[%d;1H\x1b[2K%s", row+1, tuiANSIFit(value, width))
	}
	if height < 2 || shift >= height || shift <= -height {
		for row := 0; row < height; row++ {
			value := ""
			if row < len(visibleRows) {
				value = visibleRows[row]
			}
			writeRow(row, value)
		}
		return output.String()
	}

	if shift > 0 {
		_, _ = fmt.Fprintf(&output, "\x1b[1;1H\x1b[%dT", shift)
		for row := 0; row < shift; row++ {
			writeRow(row, visibleRows[row])
		}
		return output.String()
	}

	shift = -shift
	_, _ = fmt.Fprintf(&output, "\x1b[1;1H\x1b[%dS", shift)
	for row := height - shift; row < height; row++ {
		writeRow(row, visibleRows[row])
	}
	return output.String()
}

func tuiANSIFit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	valueWidth := ansi.StringWidth(value)
	if valueWidth > width {
		if width == 1 {
			return "…"
		}
		return ansi.Truncate(value, width-1, "") + "…"
	}
	return value + strings.Repeat(" ", width-valueWidth)
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

func (m *assetTUIMultiSessionManager) showNotice(message string) {
	m.outputMu.Lock()
	m.mu.Lock()
	m.notice = message
	m.noticeID++
	noticeID := m.noticeID
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()

	time.AfterFunc(assetTUIMultiNoticeDuration, func() {
		m.hideNotice(noticeID)
	})
}

func (m *assetTUIMultiSessionManager) hideNotice(noticeID uint64) {
	m.outputMu.Lock()
	m.mu.Lock()
	if m.notice == "" || m.noticeID != noticeID {
		m.mu.Unlock()
		m.outputMu.Unlock()
		return
	}
	m.notice = ""
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) Resize(window ssh.Window) {
	width, height := assetTUITerminalSize(window.Width, window.Height)

	m.outputMu.Lock()
	m.mu.Lock()
	m.width, m.height = width, height
	contentHeight := assetTUIMultiSessionHeight(height, m.immersive, m.commandMode)
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	active := m.viewActive
	m.mu.Unlock()
	resizeAssetTUIMultiSessions(sessions, width, contentHeight)
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
	m.immersive = false
	m.immersiveHint = false
	m.immersiveHintID++
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	m.confirmAction = assetTUIMultiConfirmNone
	m.notice = ""
	m.noticeID++
	sessions := m.sessions
	m.sessions = nil
	m.mu.Unlock()
	for _, session := range sessions {
		_ = session.conn.Close()
		_ = session.screen.Close()
	}
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) activate(commandMode bool) {
	m.outputMu.Lock()
	m.mu.Lock()
	m.viewActive = true
	m.commandMode = commandMode
	m.immersive = false
	m.immersiveHint = false
	m.immersiveHintID++
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	m.confirmAction = assetTUIMultiConfirmNone
	m.notice = ""
	m.noticeID++
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	width := m.width
	height := assetTUIMultiSessionHeight(m.height, m.immersive, m.commandMode)
	m.mu.Unlock()
	resizeAssetTUIMultiSessions(sessions, width, height)
	_, _ = io.WriteString(m.physical, assetTUIMultiClearHistory)
	m.renderCurrentLocked()
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) resizeAndRenderCurrent() {
	m.outputMu.Lock()
	m.mu.RLock()
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	width := m.width
	height := assetTUIMultiSessionHeight(m.height, m.immersive, m.commandMode)
	active := m.viewActive
	m.mu.RUnlock()
	resizeAssetTUIMultiSessions(sessions, width, height)
	if active {
		m.renderCurrentLocked()
	}
	m.outputMu.Unlock()
}

func resizeAssetTUIMultiSessions(sessions []*assetTUIMultiSession, width, height int) {
	for _, session := range sessions {
		if session.screen != nil {
			_ = session.screen.Resize(uint16(width), uint16(height), 0, 0)
		}
		if session.conn != nil {
			session.conn.setWindow(ssh.Window{Width: width, Height: height})
		}
	}
}

func (m *assetTUIMultiSessionManager) deactivate() {
	m.outputMu.Lock()
	m.mu.Lock()
	m.viewActive = false
	m.commandMode = false
	m.immersive = false
	m.immersiveHint = false
	m.immersiveHintID++
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	m.confirmAction = assetTUIMultiConfirmNone
	m.notice = ""
	m.noticeID++
	m.mu.Unlock()
	_, _ = io.WriteString(m.physical,
		assetTUIMouseDisable+tuiCursorBlinkRestore+tuiShowCursor+"\x1b[?6l\x1b[r"+tuiExitAltScreen)
	m.outputMu.Unlock()
}

func (m *assetTUIMultiSessionManager) switchSession(delta int) {
	m.outputMu.Lock()
	m.mu.Lock()
	if len(m.sessions) > 0 {
		m.active = tuiBoundedSelection(m.active, delta, len(m.sessions))
	}
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
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
	active := m.viewActive
	m.mu.Unlock()
	_ = session.conn.Close()
	_ = session.screen.Close()
	if count > 0 && active {
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
	m.immersive = false
	m.immersiveHint = false
	m.immersiveHintID++
	m.helpVisible = false
	m.helpScroll = 0
	m.sessionListVisible = false
	m.sessionListIndex = 0
	m.sessionListScroll = 0
	m.confirmAction = assetTUIMultiConfirmNone
	m.notice = ""
	m.noticeID++
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
	if bytes.IndexByte(data, 0x0c) >= 0 {
		session.screen.PreserveNextClear()
	}
	_ = session.conn.sendInput(data)
}

func (s *assetTUIMultiScreen) preserveCurrentHistoryLocked() error {
	current, err := s.terminal.ScreenRows()
	if err != nil {
		return err
	}
	styled, err := s.styledRowsLocked()
	if err != nil {
		return err
	}
	s.clearedRows = assetTUIMultiAppendRows(s.clearedRows, current)
	s.clearedStyled = assetTUIMultiAppendRows(s.clearedStyled, styled)
	return nil
}

func (s *assetTUIMultiScreen) Rows() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _ := s.terminal.ScreenRows()
	return assetTUIMultiSessionRows(s.clearedRows, current)
}

func (s *assetTUIMultiScreen) ViewRows(height int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _ := s.terminal.ScreenRows()
	return assetTUIMultiSessionViewRows(s.clearedRows, current, height)
}

func (s *assetTUIMultiScreen) RenderRows(height int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.styledRowsLocked()
	if err != nil {
		current, _ = s.terminal.ScreenRows()
		return assetTUIMultiSessionViewRows(s.clearedRows, current, height)
	}
	return assetTUIMultiSessionViewRows(s.clearedStyled, current, height)
}

func (s *assetTUIMultiScreen) styledRowsLocked() ([]string, error) {
	screen, err := s.styledFormatter.FormatString()
	if err != nil {
		return nil, err
	}
	if screen == "" {
		return []string{}, nil
	}
	rows := strings.Split(screen, "\n")
	for index := range rows {
		rows[index] = strings.TrimSuffix(rows[index], "\r")
	}
	return rows, nil
}

func assetTUIMultiAppendRows(previous, current []string) []string {
	rows := make([]string, 0, len(previous)+len(current))
	rows = append(rows, previous...)
	rows = append(rows, current...)
	if len(rows) > assetTUIMultiMaxScrollback {
		rows = rows[len(rows)-assetTUIMultiMaxScrollback:]
	}
	return rows
}

func assetTUIMultiSessionRows(clearedRows, current []string) []string {
	rows := make([]string, 0, len(clearedRows)+len(current))
	rows = append(rows, clearedRows...)
	rows = append(rows, current...)
	return rows
}

func assetTUIMultiSessionViewRows(clearedRows, current []string, height int) []string {
	padding := max(0, height-len(current))
	rows := make([]string, 0, len(clearedRows)+len(current)+padding)
	rows = append(rows, clearedRows...)
	rows = append(rows, current...)
	rows = append(rows, make([]string, padding)...)
	return rows
}

func (s *assetTUIMultiScreen) PreserveNextClear() {
	s.mu.Lock()
	s.preserveClear = true
	s.clearTail = s.clearTail[:0]
	s.clearRemaining = assetTUIMultiClearScanLimit
	s.mu.Unlock()
}

func (s *assetTUIMultiScreen) ClearGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearGeneration
}

func (s *assetTUIMultiScreen) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.preserveClear {
		n, err := s.terminal.Write(data)
		if err == nil {
			s.styledTerminal.VTWrite(data)
		}
		return n, err
	}

	combined := make([]byte, 0, len(s.clearTail)+len(data))
	combined = append(combined, s.clearTail...)
	combined = append(combined, data...)
	sequenceStart, found := assetTUIMultiClearSequence(combined)
	if !found {
		n, err := s.terminal.Write(data)
		if err == nil {
			s.styledTerminal.VTWrite(data)
		}
		s.clearRemaining -= len(data)
		if s.clearRemaining <= 0 {
			s.preserveClear = false
			s.clearTail = s.clearTail[:0]
		} else {
			s.clearTail = append(s.clearTail[:0], assetTUIMultiClearTail(combined)...)
		}
		return n, err
	}

	prefixEnd := max(0, sequenceStart-len(s.clearTail))
	if prefixEnd > 0 {
		if _, err := s.terminal.Write(data[:prefixEnd]); err != nil {
			return 0, err
		}
		s.styledTerminal.VTWrite(data[:prefixEnd])
	}
	if err := s.preserveCurrentHistoryLocked(); err != nil {
		return 0, err
	}
	if err := s.terminal.Reset(); err != nil {
		return 0, err
	}
	s.styledTerminal.Reset()
	s.preserveClear = false
	s.clearTail = s.clearTail[:0]
	s.clearRemaining = 0
	s.clearGeneration++
	replay := combined[sequenceStart:]
	if _, err := s.terminal.Write(replay); err != nil {
		return 0, err
	}
	s.styledTerminal.VTWrite(replay)
	return len(data), nil
}

func (s *assetTUIMultiScreen) Resize(columns, rows uint16, cellWidthPx, cellHeightPx uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.terminal.Resize(columns, rows, cellWidthPx, cellHeightPx); err != nil {
		return err
	}
	return s.styledTerminal.Resize(columns, rows, cellWidthPx, cellHeightPx)
}

func (s *assetTUIMultiScreen) Cursor() (x, y uint16, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, err = s.terminal.CursorX()
	if err != nil {
		return 0, 0, err
	}
	y, err = s.terminal.CursorY()
	return x, y, err
}

func (s *assetTUIMultiScreen) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.terminal.Close()
	s.styledFormatter.Close()
	s.styledTerminal.Close()
	return err
}

func assetTUIMultiClearSequence(data []byte) (int, bool) {
	start := -1
	for _, sequence := range [][]byte{{0x1b, '[', '2', 'J'}, {0x1b, '[', 'J'}, {0x9b, '2', 'J'}, {0x9b, 'J'}} {
		if index := bytes.Index(data, sequence); index >= 0 && (start < 0 || index < start) {
			start = index
		}
	}
	return start, start >= 0
}

func assetTUIMultiClearTail(data []byte) []byte {
	sequences := [][]byte{{0x1b, '[', '2', 'J'}, {0x1b, '[', 'J'}, {0x9b, '2', 'J'}, {0x9b, 'J'}}
	maxSize := 0
	for _, sequence := range sequences {
		for size := 1; size < len(sequence) && size <= len(data); size++ {
			if bytes.Equal(data[len(data)-size:], sequence[:size]) {
				maxSize = max(maxSize, size)
			}
		}
	}
	return append([]byte(nil), data[len(data)-maxSize:]...)
}

func (m *assetTUIMultiSessionManager) appendOutput(session *assetTUIMultiSession, data []byte) {
	m.outputMu.Lock()
	defer m.outputMu.Unlock()

	m.mu.RLock()
	wasScrolled := session.scrollOffset > 0
	immersive := m.immersive
	commandMode := m.commandMode
	hint := m.immersiveHint
	contentHeight := assetTUIMultiSessionHeight(m.height, immersive, commandMode)
	m.mu.RUnlock()
	beforeRows := 0
	if wasScrolled {
		rows := session.screen.ViewRows(contentHeight)
		beforeRows = len(rows)
	}
	clearGeneration := session.screen.ClearGeneration()
	_, _ = session.screen.Write(data)
	session.outputStarted = true
	cleared := session.screen.ClearGeneration() != clearGeneration
	if cleared {
		m.mu.Lock()
		session.scrollOffset = 0
		m.mu.Unlock()
	} else if wasScrolled {
		rows := session.screen.ViewRows(contentHeight)
		m.mu.Lock()
		delta := max(0, len(rows)-beforeRows)
		session.scrollOffset = min(max(0, len(rows)-contentHeight), session.scrollOffset+delta)
		m.mu.Unlock()
	}
	m.mu.RLock()
	active := m.viewActive && !m.helpVisible && !m.sessionListVisible &&
		m.confirmAction == assetTUIMultiConfirmNone && m.notice == "" && session.scrollOffset == 0 &&
		len(m.sessions) > 0 && m.sessions[m.active] == session
	m.mu.RUnlock()
	if active {
		// A newly activated session may still inherit the physical cursor position
		// of the previous session. Synchronize its first output from the virtual
		// screen so an initial reuse/login notice cannot be overwritten by the next
		// output chunk. Later output remains incremental.
		if !session.initialOutputRendered {
			session.initialOutputRendered = true
			m.renderCurrentLocked()
			return
		}
		_, _ = m.physical.Write(data)
		if immersive {
			_, _ = io.WriteString(m.physical, assetTUIMouseDisable)
			if hint {
				m.renderImmersiveHintLocked(true)
			}
		} else {
			m.renderChromeLocked(true)
		}
	}
}

func (m *assetTUIMultiSessionManager) redrawChrome() {
	m.outputMu.Lock()
	m.mu.RLock()
	active := m.viewActive
	immersive := m.immersive
	hint := m.immersiveHint
	confirmation := m.confirmAction != assetTUIMultiConfirmNone
	notice := m.notice != ""
	m.mu.RUnlock()
	if active {
		if immersive {
			if hint {
				m.renderImmersiveHintLocked(true)
			}
		} else if confirmation || notice {
			m.renderCurrentLocked()
		} else {
			m.renderChromeLocked(true)
		}
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
	immersive, hint := m.immersive, m.immersiveHint
	commandMode := m.commandMode
	m.mu.RUnlock()

	contentHeight := assetTUIMultiSessionHeight(height, immersive, commandMode)
	rows := session.screen.RenderRows(contentHeight)
	cursorX, cursorY, _ := session.screen.Cursor()
	visibleRows, scrollOffset := assetTUIMultiViewport(rows, contentHeight, scrollOffset)
	if session.outputStarted {
		session.initialOutputRendered = true
	}
	m.mu.Lock()
	session.scrollOffset = scrollOffset
	m.mu.Unlock()
	mode := ""
	if !immersive {
		mode += assetTUIMultiScrollRegion(height, commandMode) + "\x1b[?6h"
	}
	_, _ = io.WriteString(m.physical,
		m.mouseTracking(commandMode)+"\x1b[?6l\x1b[r"+utils.CharClear+mode)
	for index := range visibleRows {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H%s", index+1, tuiANSIFit(visibleRows[index], width))
	}
	if immersive {
		if hint {
			m.renderImmersiveHintLocked(true)
		} else if scrollOffset > 0 {
			_, _ = io.WriteString(m.physical, tuiHideCursor)
		} else {
			_, _ = io.WriteString(m.physical,
				assetTUIMultiCursorPosition(width, contentHeight, cursorX, cursorY))
		}
		return
	}
	m.renderChromeLocked(false)
	if m.isConfirmationVisible() {
		m.renderConfirmationLocked()
		_, _ = io.WriteString(m.physical, tuiHideCursor)
		return
	}
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
	if message := m.noticeMessage(); message != "" {
		m.renderNoticeLocked(message)
		_, _ = io.WriteString(m.physical, tuiHideCursor)
		return
	}
	cursorVisible := assetTUIMultiSessionCursorVisible(commandMode, scrollOffset)
	_, _ = io.WriteString(m.physical, assetTUIMultiScrollRegion(height, commandMode)+"\x1b[?6h"+
		assetTUIMultiCursorRestore(width, contentHeight, cursorX, cursorY, cursorVisible))
}

func (m *assetTUIMultiSessionManager) renderImmersiveHintLocked(preserveCursor bool) {
	m.mu.RLock()
	if !m.immersive || !m.immersiveHint || len(m.sessions) == 0 {
		m.mu.RUnlock()
		return
	}
	session := m.sessions[max(0, min(m.active, len(m.sessions)-1))]
	width, height := m.width, m.height
	scrollOffset := session.scrollOffset
	m.mu.RUnlock()

	message := "ctrl+b:" + m.handler.tr("退出沉浸模式", "Exit immersive mode")
	popupWidth := min(width, max(1, runewidth.StringWidth(message)+4))
	left := max(1, width-popupWidth+1)
	top := assetTUIMultiHintTop(height)
	lines := []string{tuiFit(message, popupWidth)}
	if popupWidth >= 4 && height >= 3 {
		lines = []string{
			"┌" + strings.Repeat("─", popupWidth-2) + "┐",
			"│" + tuiCenter(message, popupWidth-2) + "│",
			"└" + strings.Repeat("─", popupWidth-2) + "┘",
		}
	}
	_, _ = io.WriteString(m.physical, assetTUIMouseDisable+"\x1b[?6l\x1b[r")
	for index, line := range lines {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;%dH%s", top+index, left, line)
	}
	if preserveCursor {
		if scrollOffset > 0 {
			_, _ = io.WriteString(m.physical, tuiHideCursor)
		} else {
			cursorX, cursorY, _ := session.screen.Cursor()
			_, _ = io.WriteString(m.physical,
				assetTUIMultiCursorPosition(width, height, cursorX, cursorY))
		}
	}
}

func (m *assetTUIMultiSessionManager) restoreImmersiveHintLocked() {
	m.mu.RLock()
	if !m.immersive || len(m.sessions) == 0 {
		m.mu.RUnlock()
		return
	}
	session := m.sessions[max(0, min(m.active, len(m.sessions)-1))]
	width, height, scrollOffset := m.width, m.height, session.scrollOffset
	m.mu.RUnlock()

	rows := session.screen.RenderRows(height)
	visibleRows, _ := assetTUIMultiViewport(rows, height, scrollOffset)
	top := assetTUIMultiHintTop(height)
	_, _ = io.WriteString(m.physical, assetTUIMouseDisable+"\x1b[?6l\x1b[r")
	for row := top - 1; row < height; row++ {
		value := ""
		if row < len(visibleRows) {
			value = visibleRows[row]
		}
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K%s", row+1, tuiANSIFit(value, width))
	}
	if scrollOffset > 0 {
		_, _ = io.WriteString(m.physical, tuiHideCursor)
		return
	}
	cursorX, cursorY, _ := session.screen.Cursor()
	_, _ = io.WriteString(m.physical, assetTUIMultiCursorPosition(width, height, cursorX, cursorY))
}

func assetTUIMultiHintTop(height int) int {
	return max(1, height-2)
}

func assetTUIMultiScrollRegion(height int, commandMode bool) string {
	bottom := assetTUIMultiSessionHeight(height, false, commandMode)
	if bottom <= assetTUIMultiContentTop {
		return ""
	}
	return fmt.Sprintf("\x1b[%d;%dr", assetTUIMultiContentTop, bottom)
}

func assetTUIMultiCursorPosition(width, height int, x, y uint16) string {
	return assetTUIMultiCursorRestore(width, height, x, y, true)
}

func assetTUIMultiCursorRestore(width, height int, x, y uint16, visible bool) string {
	cursor := tuiHideCursor
	if visible {
		cursor = tuiCursorBlinkRestore + tuiShowCursor
	}
	return fmt.Sprintf("\x1b[%d;%dH%s",
		max(1, min(height, int(y)+1)), max(1, min(width, int(x)+1)), cursor)
}

func assetTUIMultiSessionCursorVisible(commandMode bool, scrollOffset int) bool {
	return !commandMode && scrollOffset == 0
}

func assetTUIMultiMouseTracking(commandMode, kokoControlsMouse bool) string {
	if commandMode && kokoControlsMouse {
		return assetTUIMouseEnable
	}
	return assetTUIMouseDisable
}

func (m *assetTUIMultiSessionManager) mouseTracking(commandMode bool) string {
	return assetTUIMultiMouseTracking(
		commandMode, m.handler.mouseMode == terminalMouseModeKoko,
	)
}

func (m *assetTUIMultiSessionManager) renderChromeLocked(preserveCursor bool) {
	m.mu.RLock()
	sessions := append([]*assetTUIMultiSession(nil), m.sessions...)
	active, width, height := m.active, m.width, m.height
	commandMode := m.commandMode
	helpVisible, sessionListVisible := m.helpVisible, m.sessionListVisible
	confirmationVisible := m.confirmAction != assetTUIMultiConfirmNone
	noticeVisible := m.notice != ""
	tabsLine := assetTUIMultiTabsLine(width, sessions, active)
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
		if !helpVisible && !sessionListVisible && !confirmationVisible && !noticeVisible &&
			active >= 0 && active < len(sessions) {
			cursorX, cursorY, err := sessions[active].screen.Cursor()
			if err == nil {
				restoreCursor = assetTUIMultiCursorRestore(
					width, assetTUIMultiSessionHeight(height, false, commandMode), cursorX, cursorY,
					assetTUIMultiSessionCursorVisible(commandMode, scrollOffset),
				)
			}
		}
	}
	_, _ = io.WriteString(m.physical, "\x1b[?6l")
	_, _ = io.WriteString(m.physical, assetTUIMultiScrollRegion(height, commandMode))
	_, _ = io.WriteString(m.physical, m.mouseTracking(commandMode))
	if commandMode && height >= 3 {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K", height-2)
	}
	if height >= 2 {
		middle := assetTUIMultiMiddleLine(width, commandMode, tabsLine)
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K%s", height-1, middle)
	}
	lang := m.handler.tr
	var activeSession *assetTUIMultiSession
	if active >= 0 && active < len(sessions) {
		activeSession = sessions[active]
	}
	footer := assetTUIMultiFooterLine(width, commandMode, len(sessions), activeSession, lang)
	_, _ = fmt.Fprintf(m.physical, "\x1b[%d;1H\x1b[2K%s", height, tuiFit(footer, width))
	if preserveCursor {
		_, _ = io.WriteString(m.physical,
			assetTUIMultiScrollRegion(height, commandMode)+"\x1b[?6h"+restoreCursor)
	}
}

func assetTUIMultiMiddleLine(width int, commandMode bool, tabsLine string) string {
	if commandMode {
		return tabsLine
	}
	return ""
}

func assetTUIMultiFooterLine(width int, commandMode bool, sessionCount int,
	session *assetTUIMultiSession, tr func(string, string) string) string {
	if !commandMode {
		left := fmt.Sprintf("ctrl+b:%s(%d/%d)",
			tr("会话窗口", "Sessions"), sessionCount, assetTUIMaxMultiSessions)
		return assetTUIMultiLeftRightLine(width, left, assetTUIMultiConnectionLabel(session))
	}
	return tuiShortcutLine(width, []string{
		"enter:" + tr("进入会话", "Enter session"),
		"tab:" + tr("下一个会话", "Next session"),
		"d:" + tr("复制会话", "Duplicate session"),
		"r:" + tr("重连会话", "Reconnect session"),
		"z:" + tr("沉浸模式", "Immersive mode"),
		"x:" + tr("关闭会话", "Close session"),
		"q:" + tr("关闭所有会话", "Close all sessions"),
		"a:" + tr("资产列表", "Asset list"),
	}, "?:"+tr("查看帮助", "View help"))
}

func assetTUIMultiLeftRightLine(width int, left, right string) string {
	if width <= 0 {
		return ""
	}
	left = strings.TrimRight(tuiFit(left, width), " ")
	leftWidth := runewidth.StringWidth(left)
	if right == "" || leftWidth >= width {
		return tuiFit(left, width)
	}
	right = strings.TrimRight(tuiFit(right, width-leftWidth-1), " ")
	rightWidth := runewidth.StringWidth(right)
	return left + strings.Repeat(" ", max(1, width-leftWidth-rightWidth)) + right
}

func assetTUIMultiSessionListLayout(width, height, total int) (assetTUIMultiSessionListGeometry, bool) {
	if width < 16 || total <= 0 {
		return assetTUIMultiSessionListGeometry{}, false
	}
	contentTop, contentBottom := assetTUIMultiContentBounds(height)
	bottom := contentBottom - 1
	available := bottom - contentTop + 1
	if available < 7 {
		return assetTUIMultiSessionListGeometry{}, false
	}
	popupWidth := min(width-1, 64)
	rows := min(total, available-6)
	popupHeight := rows + 6
	return assetTUIMultiSessionListGeometry{
		left:  max(1, width-popupWidth),
		top:   max(contentTop, bottom-popupHeight+1),
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
		label := tuiFit(assetTUIMultiSessionListLabel(index, session), contentWidth)
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
	shortcuts := assetTUIMultiSessionListShortcuts(geometry.width-4, lang)
	lines = append(lines, "│  "+tuiFit(shortcuts, geometry.width-4)+"│")
	lines = append(lines, "└"+strings.Repeat("─", geometry.width-2)+"┘")
	for index, line := range lines {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;%dH%s", geometry.top+index, geometry.left, line)
	}
}

func assetTUIMultiSessionListLabel(index int, session *assetTUIMultiSession) string {
	return fmt.Sprintf("%d. %s", index+1, assetTUIMultiConnectionLabel(session))
}

func assetTUIMultiConnectionLabel(session *assetTUIMultiSession) string {
	if session == nil {
		return ""
	}
	connection := session.connection
	return fmt.Sprintf("%s://%s@%s", connection.protocol,
		connection.account.Username, connection.asset.Name)
}

func assetTUIMultiSessionListShortcuts(width int, tr func(string, string) string) string {
	return tuiShortcutLine(width, []string{
		"↑, ↓:" + tr("选择", "Select"),
		"enter:" + tr("进入会话", "Enter session"),
	}, "esc:"+tr("关闭", "Close"))
}

func (m *assetTUIMultiSessionManager) noticeMessage() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.notice
}

func (m *assetTUIMultiSessionManager) renderNoticeLocked(message string) {
	m.mu.RLock()
	width, height := m.width, m.height
	m.mu.RUnlock()
	left, top, lines := assetTUIMultiNoticeLayout(width, height, message)
	for index, line := range lines {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;%dH%s", top+index, left, line)
	}
}

func assetTUIMultiNoticeLayout(width, height int, message string) (left, top int, lines []string) {
	if width <= 0 || height <= 0 || message == "" {
		return 0, 0, nil
	}
	contentTop, contentBottom := assetTUIMultiContentBounds(height)
	popupWidth := min(width, max(1, runewidth.StringWidth(message)+4))
	left = max(1, width-popupWidth+1)
	if popupWidth < 4 || contentBottom-contentTop+1 < 3 {
		return left, contentBottom, []string{tuiFit(message, popupWidth)}
	}
	lines = []string{
		"┌" + strings.Repeat("─", popupWidth-2) + "┐",
		"│" + tuiCenter(message, popupWidth-2) + "│",
		"└" + strings.Repeat("─", popupWidth-2) + "┘",
	}
	return left, max(contentTop, contentBottom-len(lines)+1), lines
}

func (m *assetTUIMultiSessionManager) renderConfirmationLocked() {
	m.mu.RLock()
	action := m.confirmAction
	width, height := m.width, m.height
	m.mu.RUnlock()
	if action == assetTUIMultiConfirmNone {
		return
	}

	title, message, shortcuts := assetTUIMultiConfirmationText(action, m.handler.tr)
	left, top, popupWidth, ok := assetTUIMultiConfirmationLayout(
		width, height, title, message, shortcuts,
	)
	if !ok {
		return
	}

	popup := tuiDialogFrame(title, popupWidth, 7)
	popup[2] = "├" + strings.Repeat("─", popupWidth-2) + "┤"
	popup[3] = "│  " + tuiFit(message, popupWidth-4) + "│"
	popup[4] = "├" + strings.Repeat("─", popupWidth-2) + "┤"
	popup[5] = "│  " + tuiFit(shortcuts, popupWidth-4) + "│"
	for index, line := range popup {
		_, _ = fmt.Fprintf(m.physical, "\x1b[%d;%dH%s", top+index, left, line)
	}
}

func assetTUIMultiConfirmationText(action assetTUIMultiConfirmAction,
	lang func(string, string) string) (title, message, shortcuts string) {
	title = lang("关闭会话", "Close session")
	message = lang("确定关闭当前会话？", "Close the current session?")
	shortcuts = "enter:" + lang("确认", "Confirm") + " · esc:" + lang("取消", "Cancel")
	switch action {
	case assetTUIMultiConfirmCloseAll:
		title = lang("关闭所有会话", "Close all sessions")
		message = lang("确定关闭所有会话并返回资产列表？",
			"Close all sessions and return to the asset list?")
	case assetTUIMultiConfirmCloseEnded:
		title = lang("会话已结束", "Session ended")
		message = lang("当前会话已结束，是否关闭此标签？",
			"The current session has ended. Close this tab?")
		shortcuts = "enter:" + lang("关闭", "Close") + " · esc:" + lang("保留", "Keep")
	}
	return title, message, shortcuts
}

func assetTUIMultiConfirmationLayout(width, height int, values ...string) (left, top, popupWidth int, ok bool) {
	contentTop, contentBottom := assetTUIMultiContentBounds(height)
	availableHeight := contentBottom - contentTop + 1
	if width < 16 || availableHeight < 7 {
		return 0, 0, 0, false
	}
	popupWidth = 34
	for _, value := range values {
		popupWidth = max(popupWidth, runewidth.StringWidth(value)+8)
	}
	popupWidth = min(width, min(62, popupWidth))
	left = max(1, (width-popupWidth)/2+1)
	top = contentTop + (availableHeight-7)/2
	return left, top, popupWidth, true
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
	contentTop, contentBottom := assetTUIMultiContentBounds(height)
	bottom := contentBottom - 1
	visibleRows := assetTUIMultiHelpVisibleRows(height, len(displayRows))
	if visibleRows == 0 {
		return
	}
	helpScroll = max(0, min(max(0, len(displayRows)-visibleRows), helpScroll))
	popupHeight := visibleRows + 6
	top := max(contentTop, bottom-popupHeight+1)
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
		row("shift+tab", "切换到上一个会话", "Move to the previous session"),
		row("s", "打开会话列表", "Open the session list"),
		row("←, →, h, l", "切换到左侧或右侧会话", "Move to the previous or next session"),
		row("1-9", "按编号切换到对应会话", "Switch directly to session 1-9"),
		row("↑, ↓, j, k", "向下或向上滚动当前会话内容", "Scroll the current session down or up"),
		row("d", "复制当前会话", "Duplicate the current session"),
		row("r", "重新连接当前会话", "Reconnect the current session"),
		row("z", "进入沉浸模式", "Enter immersive mode"),
		row("a", "返回资产列表", "Return to the asset list"),
		row("x", "关闭当前会话", "Close the current session"),
		row("q", "关闭所有会话并返回资产列表", "Close all sessions and return to the asset list"),
		row("ctrl+b, ctrl+b", "向当前会话发送 ctrl+b", "Send ctrl+b to the current session"),
		row("?", "打开快捷键帮助", "Open shortcut help"),
		row("enter, esc, i", "进入当前会话", "Enter the current session"),
	}
}

func assetTUIMultiHelpVisibleRows(height, total int) int {
	contentTop, contentBottom := assetTUIMultiContentBounds(height)
	available := contentBottom - contentTop
	return min(total, max(0, available-6))
}

func assetTUIMultiTabsLine(width int, sessions []*assetTUIMultiSession, active int) string {
	line, _ := assetTUIMultiTabsLayout(width, sessions, active)
	return line
}

func assetTUIMultiTabsLayout(width int, sessions []*assetTUIMultiSession,
	active int) (string, []assetTUIMultiTabHit) {
	if width <= 0 || len(sessions) == 0 {
		return "", nil
	}
	tabsWidth := width
	active = max(0, min(active, len(sessions)-1))
	bodies := make([]string, len(sessions))
	labels := make([]string, len(sessions))
	for index, session := range sessions {
		bodies[index] = assetTUIMultiTabBody(index, session, 24)
		labels[index] = assetTUIMultiTabLabel(bodies[index], index == active,
			runewidth.StringWidth(bodies[index])+6)
	}

	start, end := active, active+1
	bestCount, bestBalance, bestWidth := 0, len(sessions)+1, 0
	for candidateStart := 0; candidateStart <= active; candidateStart++ {
		for candidateEnd := active + 1; candidateEnd <= len(sessions); candidateEnd++ {
			candidateWidth := assetTUIMultiTabsRangeWidth(labels, candidateStart, candidateEnd)
			if candidateWidth > tabsWidth {
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
	if requiredWidth > tabsWidth {
		activeWidth := runewidth.StringWidth(labels[active])
		available := tabsWidth - (requiredWidth - activeWidth)
		if available < 7 {
			start, end = active, active+1
			leftMarker, rightMarker = "", ""
			bodies[active] = assetTUIMultiTabBody(active, sessions[active], max(1, tabsWidth-6))
			labels[active] = assetTUIMultiTabLabel(bodies[active], true, tabsWidth)
		} else {
			bodies[active] = assetTUIMultiTabBody(active, sessions[active], available-6)
			labels[active] = assetTUIMultiTabLabel(bodies[active], true, available)
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

	const separator = "|"
	var result strings.Builder
	result.WriteString(tuiSelectedStyle)
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
		result.WriteString(cell.label)
		visibleWidth += labelWidth
	}
	if visibleWidth < width {
		result.WriteString(strings.Repeat(" ", width-visibleWidth))
	}
	result.WriteString(tuiStyleReset)
	return result.String(), hits
}

func assetTUIMultiTabBody(index int, session *assetTUIMultiSession, width int) string {
	if width <= 0 {
		return ""
	}
	prefix := fmt.Sprintf("%d:", index+1)
	state := ""
	if session.failed {
		state = " !"
	} else if session.done {
		state = " ×"
	}
	stateWidth := runewidth.StringWidth(state)
	if stateWidth >= width {
		return strings.TrimSpace(state)
	}
	prefixWidth := runewidth.StringWidth(prefix)
	if prefixWidth+stateWidth >= width {
		prefix = strings.TrimRight(tuiFit(prefix, width-stateWidth), " ")
		return prefix + state
	}
	titleWidth := width - prefixWidth - stateWidth
	title := strings.TrimRight(tuiFit(session.title, titleWidth), " ")
	return prefix + title + state
}

func assetTUIMultiTabLabel(body string, active bool, width int) string {
	const padding = 3
	left := strings.Repeat(" ", padding)
	if active {
		left = " ▶ "
	}
	right := strings.Repeat(" ", padding)
	if width < padding*2 {
		return strings.TrimRight(tuiFit(left+body+right, width), " ")
	}
	body = strings.TrimRight(tuiFit(body, width-padding*2), " ")
	return left + body + right
}

func assetTUIMultiTabsRangeWidth(labels []string, start, end int) int {
	separatorWidth := runewidth.StringWidth("|")
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

func assetTUIMultiSessionHeight(height int, immersive, commandMode bool) int {
	if immersive {
		return max(1, height)
	}
	if !commandMode {
		return max(1, height-2)
	}
	return assetTUIMultiContentHeight(height)
}

func assetTUIMultiContentBounds(height int) (top, bottom int) {
	terminalHeight := max(1, height)
	top = min(assetTUIMultiContentTop, terminalHeight)
	bottom = min(terminalHeight, top+assetTUIMultiContentHeight(height)-1)
	return top, max(top, bottom)
}

func assetTUIMultiContentContains(height, row int) bool {
	top, bottom := assetTUIMultiContentBounds(height)
	row++
	return row >= top && row <= bottom
}

func assetTUIMultiSessionContains(height, row int, immersive bool) bool {
	if immersive {
		return row >= 0 && row < height
	}
	return assetTUIMultiContentContains(height, row)
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
