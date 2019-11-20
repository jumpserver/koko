package utils

import (
	"bytes"
	"unicode/utf8"
)

type TerminalParser struct {
	// Escape contains a pointer to the escape codes for this terminal.
	// It's always a valid pointer, although the escape codes themselves
	// may be empty if the terminal doesn't support them.
	Escape *EscapeCodes

	// line is the current line being entered.
	line []rune
	// pos is the logical position of the cursor in line
	pos int
	// pasteActive is true iff there is a bracketed paste operation in
	// progress.
	pasteActive bool

	// maxLine is the greatest value of cursorY so far.
	maxLine int

	// remainder contains the remainder of any partial key sequences after
	// a read. It aliases into inBuf.
	remainder []byte
	inBuf     [256]byte
}

func NewTerminalParser() TerminalParser {
	return TerminalParser{
		Escape: &vt100EscapeCodes,
	}
}

func (t *TerminalParser) ParseLines(p []byte) (lines []string, isOk bool) {
	c := bytes.NewReader(p)
	var err error
	isOk = true
	lineIsPasted := t.pasteActive
	var line string
	lines = make([]string, 0, 10)
	for {
		rest := t.remainder
		lineOk := false
		for !lineOk {
			var key rune
			key, rest = bytesToKey(rest, t.pasteActive)
			if key == utf8.RuneError {
				break
			}
			if !t.pasteActive {
				if key == keyCtrlD {
					if len(t.line) == 0 {
						// as key has already handled, we need update remainder data,
						t.remainder = rest
						return
					}
				}
				if key == keyPasteStart {
					t.pasteActive = true
					if len(t.line) == 0 {
						lineIsPasted = true
					}
					continue
				}
			} else if key == keyPasteEnd {
				t.pasteActive = false
				continue
			}
			switch key {
			case keyUp:
				runes := []rune("")
				t.setLine(runes, len(runes))
				isOk = false
				continue
			case keyDown:
				runes := []rune("")
				t.setLine(runes, len(runes))
				isOk = false
				continue
			}

			if !t.pasteActive {
				lineIsPasted = false
			}
			line, lineOk = t.handleKey(key)
		}
		if len(rest) > 0 {
			n := copy(t.inBuf[:], rest)
			t.remainder = t.inBuf[:n]
		} else {
			t.remainder = nil
		}
		if lineOk {
			if lineIsPasted {
				err = ErrPasteIndicator
			}
			lines = append(lines, line)
		}

		// t.remainder is a slice at the beginning of t.inBuf
		// containing a partial key sequence
		readBuf := t.inBuf[len(t.remainder):]
		var n int

		n, err = c.Read(readBuf)

		t.remainder = t.inBuf[:n+len(t.remainder)]
		if n == 0 && err != nil && len(t.remainder) == 0 {
			lines = append(lines, string(t.line))
			return
		}
	}
}

func (t *TerminalParser) eraseNPreviousChars(n int) {
	if n == 0 {
		return
	}

	if t.pos < n {
		n = t.pos
	}
	t.pos -= n

	copy(t.line[t.pos:], t.line[n+t.pos:])
	t.line = t.line[:len(t.line)-n]
}

func (t *TerminalParser) countToLeftWord() int {
	if t.pos == 0 {
		return 0
	}

	pos := t.pos - 1
	for pos > 0 {
		if t.line[pos] != ' ' {
			break
		}
		pos--
	}
	for pos > 0 {
		if t.line[pos] == ' ' {
			pos++
			break
		}
		pos--
	}

	return t.pos - pos
}

func (t *TerminalParser) countToRightWord() int {
	pos := t.pos
	for pos < len(t.line) {
		if t.line[pos] == ' ' {
			break
		}
		pos++
	}
	for pos < len(t.line) {
		if t.line[pos] != ' ' {
			break
		}
		pos++
	}
	return pos - t.pos
}

func (t *TerminalParser) setLine(newLine []rune, newPos int) {
	t.line = newLine
	t.pos = newPos
}

func (t *TerminalParser) handleKey(key rune) (line string, ok bool) {
	if t.pasteActive && key != keyEnter {
		t.addKeyToLine(key)
		return
	}

	switch key {
	case keyBackspace:
		if t.pos == 0 {
			return
		}
		t.eraseNPreviousChars(1)
	case keyAltLeft:
		// move left by a word.
		t.pos -= t.countToLeftWord()
	case keyAltRight:
		// move right by a word.
		t.pos += t.countToRightWord()
	case keyLeft:
		if t.pos == 0 {
			return
		}
		t.pos--
	case keyRight:
		if t.pos == len(t.line) {
			return
		}
		t.pos++
	case keyHome:
		if t.pos == 0 {
			return
		}
		t.pos = 0
	case keyEnd:
		if t.pos == len(t.line) {
			return
		}
		t.pos = len(t.line)
	case keyUp:
		runes := []rune("")
		t.setLine(runes, len(runes))
	case keyDown:
		runes := []rune("")
		t.setLine(runes, len(runes))
	case keyEnter:
		line = string(t.line)
		ok = true
		t.line = t.line[:0]
		t.pos = 0
		t.maxLine = 0
	case keyDeleteWord:
		// Delete zero or more spaces and then one or more characters.
		t.eraseNPreviousChars(t.countToLeftWord())
	case keyDeleteLine:
		// Delete everything from the current cursor position to the
		// end of line.
		//for i := t.pos; i < len(t.line); i++ {
		//	t.advanceCursor(1)
		//}
		t.line = t.line[:t.pos]
	case keyCtrlD:
		// Erase the character under the current position.
		// The EOF case when the line is empty is handled in
		// readLine().
		if t.pos < len(t.line) {
			t.pos++
			t.eraseNPreviousChars(1)
		}
	case keyCtrlU:
		t.eraseNPreviousChars(t.pos)
	case keyClearScreen:
		// Erases the screen and moves the cursor to the home position.
		//t.cursorX, t.cursorY = 0, 0
		//t.advanceCursor(visualLength(t.prompt))
		t.setLine(t.line, t.pos)
	default:
		if !isPrintable(key) {
			return
		}
		if len(t.line) == maxLineLength {
			return
		}
		t.addKeyToLine(key)
	}
	return
}

func (t *TerminalParser) addKeyToLine(key rune) {
	if len(t.line) == cap(t.line) {
		newLine := make([]rune, len(t.line), 2*(1+len(t.line)))
		copy(newLine, t.line)
		t.line = newLine
	}
	t.line = t.line[:len(t.line)+1]
	copy(t.line[t.pos+1:], t.line[t.pos:])
	t.line[t.pos] = key
	t.pos++
}
