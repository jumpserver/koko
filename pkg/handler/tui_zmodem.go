package handler

import (
	"bytes"

	"github.com/jumpserver/koko/pkg/zmodem"
)

const assetTUIZmodemHeaderLimit = 64

// Observe the bytes actually written to the client. Room end events can arrive
// before the final output chunks, so they cannot safely re-enable TUI rendering.
// All protocol parsing here is protected by outputMu.
func (m *assetTUIMultiSessionManager) appendZmodemOutputLocked(session *assetTUIMultiSession, data []byte) {
	if session.zmodemParser != nil && session.zmodemParser.IsStartSession() {
		var tail []byte
		if session.zmodemParser.Status() == zmodem.ZParserStatusSend {
			tail = session.zmodemParser.Parse(data)
		}
		m.writeZmodemOutputLocked(session, data, tail)
		return
	}

	if len(session.zmodemStartBuf) > 0 {
		data = append(session.zmodemStartBuf, data...)
		session.zmodemStartBuf = nil
	}
	prefix := zmodem.HexHeaderPrefix
	start := bytes.Index(data, prefix)
	if start < 0 {
		for size := min(len(data), len(prefix)-1); size > 0; size-- {
			if bytes.Equal(data[len(data)-size:], prefix[:size]) {
				session.zmodemStartBuf = bytes.Clone(data[len(data)-size:])
				data = data[:len(data)-size]
				break
			}
		}
		m.appendTerminalOutputLocked(session, data)
		return
	}

	header := data[start:]
	end := bytes.IndexByte(header, '\n')
	if highLF := bytes.IndexByte(header, 0x8a); highLF >= 0 && (end < 0 || highLF < end) {
		end = highLF
	}
	if end < 0 && len(header) <= assetTUIZmodemHeaderLimit {
		session.zmodemStartBuf = bytes.Clone(header)
		m.appendTerminalOutputLocked(session, data[:start])
		return
	}
	if end < 0 {
		m.appendTerminalOutputLocked(session, data)
		return
	}
	hd, _, ok := zmodem.DecodeHexFrameHeader(header[:end+1])
	if !ok || hd.Type != zmodem.ZRQINIT && hd.Type != zmodem.ZRINIT {
		m.appendTerminalOutputLocked(session, data)
		return
	}

	m.appendTerminalOutputLocked(session, data[:start])
	session.zmodemParser = zmodem.New()
	session.zmodemAborting = false
	session.zmodemParser.Parse(header[:end+1])
	m.mu.Lock()
	if m.zmodemSession == nil && m.viewActive && len(m.sessions) > 0 && m.sessions[m.active] == session {
		m.zmodemSession = session
	}
	m.mu.Unlock()
	tail := session.zmodemParser.Parse(header[end+1:])
	m.writeZmodemOutputLocked(session, header, tail)
}

func (m *assetTUIMultiSessionManager) writeZmodemOutputLocked(session *assetTUIMultiSession, data, tail []byte) {
	m.mu.RLock()
	active := m.zmodemSession == session
	m.mu.RUnlock()
	if active {
		// File bytes must bypass both virtual screens and every chrome/cursor write.
		_, _ = m.physical.Write(data[:len(data)-len(tail)])
	}
	if session.zmodemAborting || !session.zmodemParser.IsStartSession() {
		m.finishZmodemLocked(session)
	}
	m.appendTerminalOutputLocked(session, tail)
}

// Client replies and upload data may contain ctrl+b, mouse sequences and ctrl+l.
// Forward each read intact, without interpreting any of those bytes as TUI keys.
func (m *assetTUIMultiSessionManager) forwardZmodemInput(data []byte) bool {
	m.outputMu.Lock()
	m.mu.RLock()
	session := m.zmodemSession
	m.mu.RUnlock()
	if session == nil {
		m.outputMu.Unlock()
		return false
	}
	if session.zmodemAborting {
		m.finishZmodemLocked(session)
		m.outputMu.Unlock()
		return false
	}
	if session.zmodemParser.Status() == zmodem.ZParserStatusReceive {
		session.zmodemParser.Parse(data)
		if !session.zmodemParser.IsStartSession() {
			m.finishZmodemLocked(session)
		}
	}
	m.outputMu.Unlock()
	_ = session.conn.sendInput(data)
	return true
}

func (m *assetTUIMultiSessionManager) abortZmodem(session *assetTUIMultiSession) {
	m.outputMu.Lock()
	defer m.outputMu.Unlock()
	if session.zmodemParser != nil {
		session.zmodemAborting = true
	}
}

func (m *assetTUIMultiSessionManager) finishZmodemLocked(session *assetTUIMultiSession) {
	if session.zmodemParser != nil {
		session.zmodemParser.Cleanup()
		session.zmodemParser = nil
	}
	session.zmodemAborting = false
	session.zmodemStartBuf = nil
	m.mu.Lock()
	if m.zmodemSession == session {
		m.zmodemSession = nil
		// The next shell output restores the viewport without replaying file data.
		session.initialOutputRendered = false
	}
	m.mu.Unlock()
}
