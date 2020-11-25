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
