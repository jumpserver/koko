package utils

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

func ParseTerminalData(p []byte) (lines []string) {
	c := bytes.NewReader(p)
	pasteActive := false
	var line []rune
	var pos int
	var remainder []byte
	var inBuf [256]byte
	for {
		rest := remainder
		lineOk := false
		for !lineOk {
			var key rune
			key, rest = bytesToKey(rest, pasteActive)
			if key == utf8.RuneError {
				break
			}
			if !pasteActive {
				if key == keyPasteStart {
					pasteActive = true
					if len(line) == 0 {
					}
					continue
				}
			} else if key == keyPasteEnd {
				pasteActive = false
				continue
			}

			switch key {
			case keyBackspace:
				if pos == 0 {
					continue
				}
				line, pos = EraseNPreviousChars(1, pos, line)
			case keyAltLeft:
				// move left by a word.
				pos -= CountToLeftWord(pos, line)
			case keyAltRight:
				// move right by a word.
				pos += CountToRightWord(pos, line)
			case keyLeft:
				if pos == 0 {
					continue
				}
				pos--
			case keyRight:
				if pos == len(line) {
					continue
				}
				pos++
			case keyHome:
				// clear screen ===> 不需要做任何事情
				fmt.Println("keyHome==>")
				if pos == 0 {
					continue
				}
				pos = 0
			case keyEnd:
				fmt.Println("keyEnd==>")
				if pos == len(line) {
					continue
				}
				pos = len(line)
			case keyUp:
				fmt.Println("keyUp==>")
				line = []rune{}
				pos = 0
			case keyDown:
				fmt.Println("keyDown==>")
				line = []rune{}
				pos = 0
			case keyEnter:
				fmt.Println("keyEnter==>")
				lines = append(lines, string(line))
				line = line[:0]
				pos = 0
				lineOk = true
			case keyDeleteWord:
				fmt.Println("keyDeleteWord==>")
				// Delete zero or more spaces and then one or more characters.
				line, pos = EraseNPreviousChars(CountToLeftWord(pos, line), pos, line)
			case keyDeleteLine:
				fmt.Println("keyDeleteLine==>")
				line = line[:pos]
			case keyCtrlD:
				fmt.Println("keyCtrlD==>")
				// Erase the character under the current position.
				// The EOF case when the line is empty is handled in
				// readLine().
				if pos < len(line) {
					pos++
					line, pos = EraseNPreviousChars(1, pos, line)
				}
			case keyCtrlU:
				fmt.Println("keyCtrlU==>")
				line = line[:0]
			case keyClearScreen:
				fmt.Println("keyClearScreen==>")
			default:
				if !isPrintable(key) {
					fmt.Println("could not printable: ", []byte(string(key)), " ", key)
					continue
				}
				fmt.Println("no key==>" ,string(key))
				line, pos = AddKeyToLine(key, pos, line)
			}

		}
		if len(rest) > 0 {
			n := copy(inBuf[:], rest)
			remainder = inBuf[:n]
		} else {
			remainder = nil
		}

		// remainder is a slice at the beginning of t.inBuf
		// containing a partial key sequence
		readBuf := inBuf[len(remainder):]

		var n int
		n, err := c.Read(readBuf)
		if err != nil {
			if len(line) > 0 {
				fmt.Println("read line ", line)
				lines = append(lines, string(line))
			} else if len(rest) > 0 {
				fmt.Println("read rest ")
				lines = append(lines, string(rest))
			}

			return
		}
		remainder = inBuf[:n+len(remainder)]
	}
}

func EraseNPreviousChars(n, cPos int, line []rune) ([]rune, int) {
	if n == 0 {
		return line, cPos
	}
	if cPos < n {
		n = cPos
	}
	cPos -= n
	copy(line[cPos:], line[n+cPos:])
	return line[:len(line)-n], cPos
}

func CountToLeftWord(currentPos int, line []rune) int {
	if currentPos == 0 {
		return 0
	}

	pos := currentPos - 1
	for pos > 0 {
		if line[pos] != ' ' {
			break
		}
		pos--
	}
	for pos > 0 {
		if line[pos] == ' ' {
			pos++
			break
		}
		pos--
	}

	return currentPos - pos
}

func CountToRightWord(currentPos int, line []rune) int {
	pos := currentPos
	for pos < len(line) {
		if line[pos] == ' ' {
			break
		}
		pos++
	}
	for pos < len(line) {
		if line[pos] != ' ' {
			break
		}
		pos++
	}
	return pos - currentPos
}

func AddKeyToLine(key rune, pos int, line []rune) ([]rune, int) {
	if len(line) == cap(line) {
		newLine := make([]rune, len(line), 2*(1+len(line)))
		copy(newLine, line)
		line = newLine
	}
	line = line[:len(line)+1]
	copy(line[pos+1:], line[pos:])
	line[pos] = key
	pos++
	return line, pos
}