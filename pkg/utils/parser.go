// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package utils

import (
	"bytes"
	"unicode/utf8"
)

type terminalParser struct {

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

	// history contains previously entered commands so that they can be
	// accessed with the up and down keys.
	history stRingBuffer
	// historyIndex stores the currently accessed history entry, where zero
	// means the immediately previous entry.
	historyIndex int
	// When navigating up and down the history it's possible to return to
	// the incomplete, initial line. That value is stored in
	// historyPending.
	historyPending string
}

func (t *terminalParser) setLine(newLine []rune, newPos int) {
	t.line = newLine
	t.pos = newPos
}

func (t *terminalParser) eraseNPreviousChars(n int) {
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

// countToLeftWord returns then number of characters from the cursor to the
// start of the previous word.
func (t *terminalParser) countToLeftWord() int {
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

// countToRightWord returns then number of characters from the cursor to the
// start of the next word.
func (t *terminalParser) countToRightWord() int {
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

// handleKey processes the given key and, optionally, returns a line of text
// that the user has entered.
func (t *terminalParser) handleKey(key rune) (line string, ok bool) {
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
		entry, ok := t.history.NthPreviousEntry(t.historyIndex + 1)
		if !ok {
			return "", false
		}
		if t.historyIndex == -1 {
			t.historyPending = string(t.line)
		}
		t.historyIndex++
		runes := []rune(entry)
		t.setLine(runes, len(runes))
	case keyDown:
		switch t.historyIndex {
		case -1:
			return
		case 0:
			runes := []rune(t.historyPending)
			t.setLine(runes, len(runes))
			t.historyIndex--
		default:
			entry, ok := t.history.NthPreviousEntry(t.historyIndex - 1)
			if ok {
				t.historyIndex--
				runes := []rune(entry)
				t.setLine(runes, len(runes))
			}
		}
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

// addKeyToLine inserts the given key at the current position in the current
// line.
func (t *terminalParser) addKeyToLine(key rune) {
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

func (t *terminalParser) parseLines(p []byte) (lines []string) {
	var err error

	lines = make([]string, 0, 3)
	lineIsPasted := t.pasteActive
	reader := bytes.NewBuffer(p)
	for {
		rest := t.remainder
		line := ""
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
						return lines
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

		n, err = reader.Read(readBuf)
		if err != nil && n == 0 {
			if len(t.line) > 0 && len(t.remainder) == 0 {
				lines = append(lines, string(t.line))
			}
			if len(t.remainder) > 0 {
				t.remainder = t.remainder[1:]
				continue
			}
			return
		} else if err == nil && n == 0 {
			if len(t.remainder) == len(t.inBuf) {
				t.remainder = t.remainder[1:]
				continue
			}
		}

		t.remainder = t.inBuf[:n+len(t.remainder)]
	}
}

func ParseTerminalData(p []byte) (lines []string) {
	t := terminalParser{
		historyIndex: -1,
	}
	return t.parseLines(p)
}
