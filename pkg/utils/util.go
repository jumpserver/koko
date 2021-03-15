package utils

import (
	"fmt"
	"io"
	"strings"
)

func IgnoreErrWriteString(writer io.Writer, s string) {
	_, _ = io.WriteString(writer, s)
}

const (
	ColorEscape = "\033["
	Green       = "32m"
	Red         = "31m"
	ColorEnd    = ColorEscape + "0m"
	Bold        = "1"
)

const (
	CharClear     = "\x1b[H\x1b[2J"
	CharTab       = "\t"
	CharNewLine   = "\r\n"
	CharCleanLine = '\x15'
)

func WrapperString(text string, color string, meta ...bool) string {
	wrapWith := make([]string, 0)
	metaLen := len(meta)
	switch metaLen {
	case 1:
		wrapWith = append(wrapWith, Bold)
	}
	wrapWith = append(wrapWith, color)
	return fmt.Sprintf("%s%s%s%s", ColorEscape, strings.Join(wrapWith, ";"), text, ColorEnd)
}

func WrapperTitle(text string) string {
	return WrapperString(text, Green, true)
}

func WrapperWarn(text string) string {
	text += "\n\r"
	return WrapperString(text, Red)
}

func IgnoreErrWriteWindowTitle(writer io.Writer, title string) {
	// OSC Ps ; Pt BEL
	// OSC Ps ; Pt ST
	// Ps = 2  ⇒  Change Window Title to Pt.
	_, _ = writer.Write([]byte(fmt.Sprintf("\x1b]2;%s\x07", title)))
}

func LongestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}

	isCommonPrefix := func(length int) bool {
		str0, count := strs[0][:length], len(strs)
		for i := 1; i < count; i++ {
			if strs[i][:length] != str0 {
				return false
			}
		}
		return true
	}

	minLength := len(strs[0])
	for _, s := range strs {
		if len(s) < minLength {
			minLength = len(s)
		}

	}

	low, high := 0, minLength
	for low < high {
		mid := (high-low+1)/2 + low
		if isCommonPrefix(mid) {
			low = mid
		} else {
			high = mid - 1
		}

	}
	return strs[0][:low]
}

func FilterPrefix(strs []string, s string) (r []string) {
	for _, v := range strs {
		if len(v) >= len(s) {
			if v[:len(s)] == s {
				r = append(r, v)
			}
		}
	}

	return r
}
