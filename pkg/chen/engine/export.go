package engine

import "strings"

// Spreadsheet-oriented CSV readers may evaluate strings as formulas.
// Prefix potentially executable cell values with an apostrophe.
func safeCSVValue(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) || strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") {
		return "'" + value
	}
	return value
}
