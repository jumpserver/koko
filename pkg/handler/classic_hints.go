package handler

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/utils"
)

type classicHintStyle uint8

const (
	classicHintPlain classicHintStyle = iota
	classicHintShortcut
	classicHintSearchGuide
	classicHintSearchTip
)

type classicHintRow struct {
	text  string
	style classicHintStyle
}

func classicHintRows(style classicHintStyle, texts []string) []classicHintRow {
	rows := make([]classicHintRow, 0, len(texts))
	for _, text := range texts {
		rows = append(rows, classicHintRow{text: text, style: style})
	}
	return rows
}

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
		combined := current + " · " + text
		if width > 0 && runewidth.StringWidth(combined) > width {
			rows = append(rows, current)
			current = text
			continue
		}
		current = combined
	}
	return append(rows, current)
}

func classicRightAlignedHintLines(hint, count string, width int) []string {
	if count == "" {
		return classicHintLines(hint, width)
	}
	countWidth := runewidth.StringWidth(count)
	if countWidth > width {
		if hint == "" {
			return classicHintLines(count, width)
		}
		return append(classicHintLines(hint, width), classicHintLines(count, width)...)
	}
	if hint == "" {
		return []string{strings.Repeat(" ", width-countWidth) + count}
	}
	if hintWidth := runewidth.StringWidth(hint); hintWidth <= width && hintWidth+2+countWidth > width {
		return []string{hint, strings.Repeat(" ", width-countWidth) + count}
	}
	if width-countWidth-2 >= 16 {
		lines := classicHintLines(hint, width-countWidth-2)
		if gap := width - runewidth.StringWidth(lines[0]) - countWidth; gap >= 2 {
			lines[0] += strings.Repeat(" ", gap) + count
			return lines
		}
	}
	lines := classicHintLines(hint, width)
	last := len(lines) - 1
	if runewidth.StringWidth(lines[last])+2+countWidth <= width {
		lines[last] += strings.Repeat(" ", width-runewidth.StringWidth(lines[last])-countWidth) + count
	} else {
		lines = append(lines, strings.Repeat(" ", width-countWidth)+count)
	}
	return lines
}

func classicHintPanel(rows []classicHintRow, width int) string {
	return classicHintPanelWithRule(rows, width, strings.Repeat("─", max(1, width)))
}

func classicAssetHintPanel(rows []classicHintRow, width int) string {
	width = max(1, width)
	rule := strings.Repeat("-", width)
	return classicHintPanelWithRule(rows, width, rule)
}

func classicHintPanelWithRule(rows []classicHintRow, width int, rule string) string {
	if len(rows) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString(rule)
	result.WriteString(utils.CharNewLine)
	for _, row := range rows {
		var searchTokens []string
		if row.style == classicHintSearchGuide {
			searchTokens = classicSearchGuideTokens(row.text)
		}
		for _, line := range classicHintLines(row.text, width) {
			switch row.style {
			case classicHintShortcut:
				line = highlightClassicShortcuts(line)
			case classicHintSearchGuide:
				line = highlightClassicSearchGuide(line, searchTokens)
			case classicHintSearchTip:
				line = highlightClassicSearchTip(line)
			}
			result.WriteString(line)
			result.WriteString(utils.CharNewLine)
		}
	}
	return result.String()
}

func highlightClassicShortcuts(value string) string {
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
		_, size := utf8.DecodeRuneInString(value[i:])
		result.WriteString(value[i : i+size])
		i += size
	}
	return result.String()
}

func classicSearchGuideTokens(value string) []string {
	parts := strings.SplitN(value, " · ", 3)
	tokens := make([]string, 0, 2)
	for _, part := range parts[:min(2, len(parts))] {
		start := strings.IndexByte(part, '/')
		if start < 0 {
			continue
		}
		command := part[start:]
		if end := strings.IndexAny(command, "(（"); end >= 0 {
			command = command[:end]
		}
		if command = strings.TrimSpace(command); command != "" {
			tokens = append(tokens, command)
		}
	}
	return tokens
}

func highlightClassicSearchGuide(line string, tokens []string) string {
	var result strings.Builder
	for i := 0; i < len(line); {
		matched := false
		for _, token := range tokens {
			if strings.HasPrefix(line[i:], token) {
				result.WriteString(utils.WrapperString(token, utils.Green, true))
				i += len(token)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		result.WriteString(line[i : i+size])
		i += size
	}
	return result.String()
}

func highlightClassicSearchTip(line string) string {
	if index := strings.Index(line, "/ +"); index >= 0 {
		return line[:index] + utils.WrapperString("/", utils.Green, true) + line[index+1:]
	}
	return line
}
