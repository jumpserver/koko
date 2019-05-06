package handler

import (
	"fmt"
	"strings"
)

const (
	ColorStart = "\033["
	Green      = "32m"
	Red        = "41m"
	White      = "47m"
	ColorEnd   = "0m"
	Tab        = "\t"
	EndLine    = "\r\n"
	Bold       = "1"
)

func WrapperString(text string, color string, bold bool) string {
	wrapWith := make([]string, 1)
	if bold {
		wrapWith = append(wrapWith, Bold)
	}
	wrapWith = append(wrapWith, color)
	return fmt.Sprintf("%s%s%s", strings.Join(wrapWith, ";"), text, ColorEnd)
}

func STitle(text string) string {
	return WrapperString(text, Green, true)
}

func SWarn(text string) string {
	return WrapperString(text, White, false)
}
