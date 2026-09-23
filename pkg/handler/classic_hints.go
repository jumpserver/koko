package handler

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/utils"
)

func classicExitHint(lang i18n.LanguageCode) string {
	return "[exit] " + lang.T("End session")
}

func classicIndentedLines(prefix, text string, width int) []string {
	indent := runewidth.StringWidth(prefix)
	if indent >= width {
		return classicHintLines(prefix+text, width)
	}
	lines := classicHintLines(text, width-indent)
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", indent) + lines[i]
		}
	}
	return lines
}

func classicHintLines(text string, width int) []string {
	width = max(1, width)
	lines := strings.Split(runewidth.Wrap(strings.TrimRight(text, " \t"), width), "\n")
	for i := 0; i+1 < len(lines); i++ {
		left, right := lines[i], lines[i+1]
		if right != "" {
			first, _ := utf8.DecodeRuneInString(right)
			if strings.ContainsRune("，。、；：！？）】」", first) {
				start := len(left)
				for start > 0 {
					r, size := utf8.DecodeLastRuneInString(left[:start])
					if r < utf8.RuneSelf || !unicode.IsLetter(r) {
						break
					}
					start -= size
				}
				if start < len(left) && strings.TrimSpace(left[:start]) != "" &&
					runewidth.StringWidth(left[start:]+string(first)) <= width {
					lines[i] = strings.TrimRight(left[:start], " ")
					rewrapped := strings.Split(runewidth.Wrap(left[start:]+right, width), "\n")
					lines = append(lines[:i+1], append(rewrapped, lines[i+2:]...)...)
					continue
				}
			}
		}
		start, end := len(left), 0
		for start > 0 && classicASCIIWordByte(left[start-1]) {
			start--
		}
		for end < len(right) && classicASCIIWordByte(right[end]) {
			end++
		}
		if start == len(left) || end == 0 {
			continue
		}
		word := left[start:] + right[:end]
		if runewidth.StringWidth(word) > width {
			continue
		}
		rewrapped := strings.Split(runewidth.Wrap(word+right[end:], width), "\n")
		if strings.TrimSpace(left[:start]) == "" {
			lines = append(lines[:i], append(rewrapped, lines[i+2:]...)...)
			i--
			continue
		}
		lines[i] = strings.TrimRight(left[:start], " ")
		lines = append(lines[:i+1], append(rewrapped, lines[i+2:]...)...)
	}
	return lines
}

func classicASCIIWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || strings.ContainsRune("_./-", rune(value))
}

func compactClassicHintRows(width int, texts ...string) []string {
	filtered := make([]string, 0, len(texts))
	for _, text := range texts {
		if text = strings.TrimSpace(text); text != "" {
			filtered = append(filtered, text)
		}
	}
	if len(filtered) <= 1 {
		return filtered
	}
	rows := make([]string, 0, len(filtered))
	current := filtered[0]
	for _, text := range filtered[1:] {
		combined := current + "  ·  " + text
		if width > 0 && runewidth.StringWidth(combined) > width {
			rows = append(rows, current)
			current = text
			continue
		}
		current = combined
	}
	return append(rows, current)
}

func classicHintPanel(texts []string, width int) string {
	return classicHintPanelWithRule(texts, width, strings.Repeat("─", max(1, width)))
}

func classicAssetHintPanel(texts []string, width int) string {
	width = max(1, width)
	rule := strings.Repeat("-", width)
	return classicHintPanelWithRule(texts, width, rule)
}

func classicHintPanelWithRule(texts []string, width int, rule string) string {
	if len(texts) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString(rule)
	result.WriteString(utils.CharNewLine)
	for _, text := range texts {
		for _, line := range classicHintLines(text, width) {
			result.WriteString(highlightClassicShortcuts(line))
			result.WriteString(utils.CharNewLine)
		}
	}
	return result.String()
}

func highlightClassicShortcuts(value string) string {
	searchSyntax := strings.Contains(value, "IP") && strings.Contains(value, "/")
	highlightNumber := strings.Contains(value, "Enter") || strings.Contains(value, "回车")
	numberTokens := []string{"number", "序号", "序號", "编号", "編號", "番号", "번호", "número", "номер", "số"}
	var result strings.Builder
	for i := 0; i < len(value); {
		if value[i] == '[' {
			if end := strings.IndexByte(value[i:], ']'); end >= 0 {
				end += i + 1
				result.WriteString(utils.WrapperString(value[i:end], utils.Green, true))
				i = end
				continue
			}
		}
		if searchSyntax && value[i] == '/' {
			end := i + 1
			if end < len(value) && value[end] == '/' {
				end++
			}
			result.WriteString(utils.WrapperString(value[i:end], utils.Green, true))
			i = end
			continue
		}
		if value[i] == '?' {
			result.WriteString(utils.WrapperString("?", utils.Green, true))
			i++
			continue
		}
		if highlightNumber {
			matched := false
			for _, token := range numberTokens {
				if strings.HasPrefix(value[i:], token) {
					result.WriteString(utils.WrapperString(token, utils.Green, true))
					i += len(token)
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		matchedShortcut := false
		for _, token := range []string{"Enter", "回车"} {
			if strings.HasPrefix(value[i:], token) {
				result.WriteString(utils.WrapperString(token, utils.Green, true))
				i += len(token)
				matchedShortcut = true
				break
			}
		}
		if matchedShortcut {
			continue
		}
		_, size := utf8.DecodeRuneInString(value[i:])
		result.WriteString(value[i : i+size])
		i += size
	}
	return result.String()
}
