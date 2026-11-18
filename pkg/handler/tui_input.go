package handler

import (
	"bytes"
	"time"
)

const (
	assetTUIEscapeTimeout      = 100 * time.Millisecond
	assetTUIInputSequenceLimit = 64
)

// A terminal input sequence can span SSH reads. Only an isolated Escape key
// needs a timeout; once CSI/SS3 has begun, keep its bounded tail until complete.
func assetTUIInputSequenceLength(data []byte) int {
	if len(data) == 0 || data[0] != 0x1b {
		return 1
	}
	if len(data) == 1 {
		return 0
	}
	if data[1] == 'O' {
		if len(data) < 3 {
			return 0
		}
		return 3
	}
	if data[1] != '[' {
		return 1
	}
	if len(data) < 3 {
		return 0
	}
	if data[2] == 'M' {
		if len(data) < 6 {
			return 0
		}
		return 6
	}
	for i := 2; i < len(data); i++ {
		if data[i] >= 0x40 && data[i] <= 0x7e {
			return i + 1
		}
		if data[i] < 0x20 || data[i] > 0x3f {
			return i
		}
		if i >= assetTUIInputSequenceLimit-1 {
			return i + 1
		}
	}
	return 0
}

// Split before an incomplete escape sequence. Bytes before that point can be
// handled immediately, including ordinary text and complete mouse/key events.
func assetTUIInputBoundary(data []byte) int {
	for offset := 0; offset < len(data); {
		next := bytes.IndexByte(data[offset:], 0x1b)
		if next < 0 {
			break
		}
		offset += next
		size := assetTUIInputSequenceLength(data[offset:])
		if size == 0 {
			return offset
		}
		offset += size
	}
	return len(data)
}

type assetTUIInputRead struct {
	data []byte
	err  error
}

// Escape is ambiguous until another byte arrives. After treating it as a key,
// retain only the possibility of a mouse continuation; ordinary subsequent text
// (including a non-mouse CSI) must not be swallowed or receive a second Escape.
func assetTUILateMouse(data []byte) (size int, incomplete bool) {
	if len(data) < 2 {
		return 0, true
	}
	if data[1] != '[' {
		return 0, false
	}
	if len(data) < 3 {
		return 0, true
	}
	if data[2] != '<' && data[2] != 'M' {
		return 0, false
	}
	size = assetTUIInputSequenceLength(data)
	if size == 0 {
		return 0, true
	}
	_, consumed := assetTUIMultiMouse(data[:size])
	return consumed, false
}

type assetTUIEscapeState struct {
	commandMode  bool
	session      *assetTUIMultiSession
	scrollOffset int
}

func (m *assetTUIMultiSessionManager) escapeState() assetTUIEscapeState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := assetTUIEscapeState{commandMode: m.commandMode}
	if len(m.sessions) > 0 {
		state.session = m.sessions[max(0, min(m.active, len(m.sessions)-1))]
		state.scrollOffset = state.session.scrollOffset
	}
	return state
}

// Roll back only the speculative UI Escape, before handling any subsequent
// input. A session ending asynchronously must keep its new confirmation state.
func (m *assetTUIMultiSessionManager) restoreEscape(state assetTUIEscapeState) bool {
	m.outputMu.Lock()
	defer m.outputMu.Unlock()
	m.mu.Lock()
	if !state.commandMode || len(m.sessions) == 0 ||
		m.sessions[max(0, min(m.active, len(m.sessions)-1))] != state.session {
		m.mu.Unlock()
		return false
	}
	if m.commandMode {
		m.mu.Unlock()
		return true
	}
	m.commandMode = true
	state.session.scrollOffset = state.scrollOffset
	active := m.viewActive
	m.mu.Unlock()
	if active {
		m.renderCurrentLocked()
	}
	return true
}

// One interruptible reader lets Run distinguish Escape from a fragmented CSI
// without a goroutine per key. Join it before another terminal view reads input.
func (m *assetTUIMultiSessionManager) readInput() (<-chan assetTUIInputRead, func()) {
	reads := make(chan assetTUIInputRead)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 8192)
		for {
			n, err := m.physical.Read(buffer)
			select {
			case reads <- assetTUIInputRead{data: bytes.Clone(buffer[:n]), err: err}:
			case <-stop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return reads, func() {
		close(stop)
		_ = m.physical.InterruptRead()
		<-done
		_ = m.physical.Close()
	}
}
