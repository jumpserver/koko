package handler

import (
	"fmt"
	"io"
	"strings"

	"go.mitchellh.com/libghostty"
)

const assetTUIInputModesReset = "\x1b[?1;9;66;67;1000;1002;1003;1004;1005;1006;2004l"

// Remote control sequences belong to the session terminal, whose dimensions
// exclude Koko's chrome. Never interleave them with physical UI writes: doing
// so changes scrolling margins and can split a CSI or a UTF-8 character.
func (m *assetTUIMultiSessionManager) renderSessionOutputLocked(session *assetTUIMultiSession) {
	m.mu.RLock()
	width, height := m.width, m.height
	immersive, commandMode, hint := m.immersive, m.commandMode, m.immersiveHint
	m.mu.RUnlock()
	contentHeight := assetTUIMultiSessionHeight(height, immersive, false)
	rows, _ := assetTUIMultiViewport(session.screen.RenderRows(contentHeight), contentHeight, 0)
	if m.renderedSession != session || m.renderedWidth != width || len(m.renderedRows) != len(rows) {
		m.renderedRows = nil
	}
	var output strings.Builder
	output.WriteString(tuiHideCursor + "\x1b[?6l\x1b[r\x1b[0m")
	for row, value := range rows {
		if row < len(m.renderedRows) && m.renderedRows[row] == value {
			continue
		}
		fmt.Fprintf(&output, "\x1b[%d;1H\x1b[0m\x1b[2K%s", row+1, tuiANSIFit(value, width))
	}
	output.WriteString("\x1b[0m")
	_, _ = io.WriteString(m.physical, output.String())
	m.renderedSession, m.renderedWidth, m.renderedRows = session, width, rows
	if immersive {
		if hint {
			m.renderImmersiveHintLocked(false)
		}
	} else {
		m.renderChromeLocked(false)
	}
	m.restoreSessionInputLocked(session, commandMode, 0, width, contentHeight)
}

// Only modes that affect client input are mirrored. Display modes, margins,
// saved cursors and alternate buffers stay inside the virtual terminal.
func (s *assetTUIMultiScreen) inputModes(enabled bool) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var output strings.Builder
	for _, mode := range []libghostty.Mode{
		libghostty.ModeDECCKM, libghostty.ModeKeypadKeys, libghostty.ModeBackarrowKeyMode,
		libghostty.ModeBracketedPaste, libghostty.ModeX10Mouse,
		libghostty.ModeNormalMouse, libghostty.ModeButtonMouse, libghostty.ModeAnyMouse,
		libghostty.ModeFocusEvent, libghostty.ModeUTF8Mouse, libghostty.ModeSGRMouse,
	} {
		value, _ := s.styledTerminal.ModeGet(mode)
		action := 'l'
		if enabled && value {
			action = 'h'
		}
		fmt.Fprintf(&output, "\x1b[?%d%c", mode.Value(), action)
	}
	visible, _ := s.styledTerminal.CursorVisible()
	return output.String(), visible
}

func (m *assetTUIMultiSessionManager) restoreSessionInputLocked(session *assetTUIMultiSession,
	commandMode bool, scrollOffset, width, height int) {
	inputEnabled := !commandMode && scrollOffset == 0
	modes, visible := session.screen.inputModes(inputEnabled)
	if !inputEnabled {
		modes += m.mouseTracking(commandMode)
	}
	x, y, _ := session.screen.Cursor()
	_, _ = io.WriteString(m.physical, "\x1b[?6l"+modes+
		assetTUIMultiCursorRestore(width, height, x, y, inputEnabled && visible))
}

func (s *assetTUIMultiScreen) takeReplies() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	replies := s.replies
	s.replies = nil
	return replies
}
