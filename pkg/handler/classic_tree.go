package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/mattn/go-runewidth"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *InteractiveHandler) displayNodeTree(nodes model.NodeList, idleState chan bool) bool {
	for {
		prefixes, orderedNodes := constructNodeTreeRows(nodes)
		h.nodes = orderedNodes
		header := func() string {
			lang := i18n.NewLang(h.i18nLang)
			text := "\n\r" + lang.T("Node: [ Number.Name(Asset amount) ]") + utils.CharNewLine
			if h.nodeLoadErr != nil {
				message := lang.T("Authorization tree unavailable. Type r to retry.")
				text += utils.WrapperWarn(userFacingErrorMessage(message, h.nodeLoadErr))
			}
			return text
		}
		rows := make([]string, len(h.nodes))
		width, _ := h.GetPtySize()
		for i := range h.nodes {
			rows[i] = classicTreeRow(prefixes[i], i+1, h.nodes[i].Name, h.nodes[i].AssetsAmount, width)
		}
		result := h.displayClassicTreeRows(rows, header, "Back to authorization tree", true, idleState, func(number int) bool {
			if number <= 0 || number > len(h.nodes) {
				return false
			}
			h.selectHandler.SetNode(h.nodes[number-1])
			h.treeOrigin = TypeNodeAsset
			h.treeSelected = true
			h.selectHandler.Search("")
			return true
		})
		switch result {
		case classicTreeDisplayExit:
			return false
		case classicTreeDisplayRefresh:
			if h.refreshAuthorizationTreeCache() {
				nodes = h.nodes
				utils.IgnoreErrWriteString(h.term, utils.CharClear)
			}
			continue
		default:
			return true
		}
	}
}

func (h *InteractiveHandler) displayTypeTree(nodes []classicTypeNode, idleState chan bool) bool {
	prefixes, orderedNodes := constructTypeTreeRows(nodes)
	h.typeNodes = orderedNodes
	header := func() string {
		return "\n\r" + i18n.NewLang(h.i18nLang).T("Type: [ Number.Name(Asset amount) ]") + utils.CharNewLine
	}
	rows := make([]string, len(h.typeNodes))
	width, _ := h.GetPtySize()
	for i := range h.typeNodes {
		rows[i] = classicTreeRow(prefixes[i], i+1, h.typeNodes[i].Name, h.typeNodes[i].AssetsAmount, width)
	}
	return h.displayClassicTreeRows(rows, header, "Back to type tree", false, idleState, func(number int) bool {
		if number <= 0 || number > len(h.typeNodes) {
			return false
		}
		h.selectHandler.SetType(h.typeNodes[number-1])
		h.treeOrigin = TypeTypeAsset
		h.treeSelected = true
		h.selectHandler.Search("")
		return true
	}) != classicTreeDisplayExit
}

func (h *InteractiveHandler) displayFavoriteTree(nodes []classicFavoriteNode, idleState chan bool) bool {
	lang := i18n.NewLang(h.i18nLang)
	for i := range nodes {
		if nodes[i].ID == "favorite-root" {
			nodes[i].Name = lang.T("All favorites")
		}
	}
	prefixes, orderedNodes := constructFavoriteTreeRows(nodes)
	h.favoriteNodes = orderedNodes
	header := func() string {
		return "\n\r" + i18n.NewLang(h.i18nLang).T("Favorite: [ Number.Name(Asset amount) ]") + utils.CharNewLine
	}
	rows := make([]string, len(h.favoriteNodes))
	width, _ := h.GetPtySize()
	for i := range h.favoriteNodes {
		rows[i] = classicTreeRow(prefixes[i], i+1, h.favoriteNodes[i].Name, h.favoriteNodes[i].AssetsAmount, width)
	}
	return h.displayClassicTreeRows(rows, header, "Back to favorites tree", false, idleState, func(number int) bool {
		if number <= 0 || number > len(h.favoriteNodes) {
			return false
		}
		h.selectHandler.SetFavorite(h.favoriteNodes[number-1])
		h.treeOrigin = TypeFavoriteAsset
		h.treeSelected = true
		h.selectHandler.Search("")
		return true
	}) != classicTreeDisplayExit
}

func classicTreeRow(prefix string, number int, name string, count, width int) string {
	if width < 60 {
		depth := max(0, (runewidth.StringWidth(prefix)-4)/4)
		return fmt.Sprintf("%s%d. %s (%d)%s", strings.Repeat("  ", min(depth, 3)), number, name, count, utils.CharNewLine)
	}
	return fmt.Sprintf("%s%d.%s(%d)%s", prefix, number, name, count, utils.CharNewLine)
}

func (h *InteractiveHandler) displayClassicTreeRows(rows []string, header func() string, helpBackTarget string,
	allowRefresh bool, idleState chan bool, selectItem func(int) bool) classicTreeDisplayResult {
	width, height := h.GetPtySize()
	lang := i18n.NewLang(h.i18nLang)
	footer := classicHintPanel(classicTreeHints(lang, width, 0, len(rows), allowRefresh), width)
	pageRows := max(1, height-strings.Count(footer, utils.CharNewLine)-3)
	end := classicTreePageEnd(rows, 0, pageRows, width)
	errorTip := ""
	render := func(clear bool) {
		if clear {
			utils.IgnoreErrWriteString(h.term, utils.CharClear)
		}
		utils.IgnoreErrWriteString(h.term, header())
		writeClassicTreeRows(h.term, rows[:end], width)
		if len(rows) == 0 {
			utils.IgnoreErrWriteString(h.term, i18n.NewLang(h.i18nLang).T("No nodes")+utils.CharNewLine)
		}
	}
	render(false)
	for {
		lang := i18n.NewLang(h.i18nLang)
		pager := classicHintPanel(classicTreeHints(lang, width, end, len(rows), allowRefresh), width)
		if errorTip != "" {
			for _, line := range classicHintLines(errorTip, width) {
				pager += utils.WrapperString(line, utils.Red) + utils.CharNewLine
			}
		}
		utils.IgnoreErrWriteString(h.term, pager)
		utils.IgnoreErrWriteString(h.term, "Tree> ")
		if !h.setIdle(idleState, true) {
			return classicTreeDisplayExit
		}
		action, err := readClassicTreePagerAction(h.sess, h.term, end < len(rows), allowRefresh)
		if !h.setIdle(idleState, false) {
			return classicTreeDisplayExit
		}
		clearClassicHintPanel(h.term, pager)
		if err != nil {
			logger.Debugf("User %s close tree pager: %s", h.user.Name, err)
			return classicTreeDisplayExit
		}
		switch action.kind {
		case classicTreePagerHelp:
			if !h.showClassicHelpOverlay(helpBackTarget) {
				return classicTreeDisplayDone
			}
			render(true)
			errorTip = ""
		case classicTreePagerExit:
			h.requestClassicExit()
			return classicTreeDisplayExit
		case classicTreePagerQuit:
			utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
			return classicTreeDisplayDone
		case classicTreePagerRefresh:
			return classicTreeDisplayRefresh
		case classicTreePagerSelect:
			if !selectItem(action.itemNumber) {
				errorTip = lang.T("Invalid number")
				continue
			}
			return classicTreeDisplayDone
		case classicTreePagerInvalid:
			errorTip = lang.T("Invalid input.")
		case classicTreePagerLine:
			errorTip = ""
			if end < len(rows) {
				writeClassicTreeRows(h.term, rows[end:min(end+1, len(rows))], width)
				end++
			}
		case classicTreePagerPage:
			errorTip = ""
			if end < len(rows) {
				next := classicTreePageEnd(rows, end, pageRows, width)
				writeClassicTreeRows(h.term, rows[end:next], width)
				end = next
			}
		}
	}
}

func classicTreeHints(lang i18n.LanguageCode, width, end, total int, allowRefresh bool) []classicHintRow {
	var actions []string
	if total > 0 {
		actions = append(actions, lang.T("[number] View assets in this node"))
	}
	if allowRefresh {
		actions = append(actions, fmt.Sprintf("[r] %s", lang.T("Refresh authorization tree")))
	}
	actions = append(actions, fmt.Sprintf("[b] %s", lang.T("Back to help")))
	actions = append(actions, fmt.Sprintf("[?] %s", lang.T("View help")), classicExitHint(lang))
	actionLine := strings.Join(actions, " · ")
	progress := fmt.Sprintf("%d / %d", end, total)
	if end < total {
		browse := lang.T("Browse tree: [Enter] next line · [Space] next page")
		hints := classicRightAlignedHintLines(browse, progress, width)
		return classicHintRows(classicHintShortcut, append(hints, classicHintLines(actionLine, width)...))
	}
	return classicHintRows(classicHintShortcut, classicRightAlignedHintLines(actionLine, progress, width))
}

func classicTreePageEnd(rows []string, start, budget, width int) int {
	used, end := 0, start
	for end < len(rows) {
		rowHeight := len(classicTreeLines(rows[end], width))
		if used+rowHeight > budget && end > start {
			break
		}
		used += rowHeight
		end++
		if used >= budget {
			break
		}
	}
	return end
}

func classicTreeLines(row string, width int) []string {
	row = strings.TrimRight(row, "\r\n")
	if width < 60 {
		if dot := strings.Index(row, ". "); dot >= 0 {
			return classicIndentedLines(row[:dot+2], row[dot+2:], width)
		}
	}
	return classicHintLines(row, width)
}

func clearClassicHintPanel(writer io.Writer, panel string) {
	_, _ = io.WriteString(writer, "\r\x1b[2K")
	for range strings.Count(panel, utils.CharNewLine) {
		_, _ = io.WriteString(writer, "\x1b[1A\r\x1b[2K")
	}
}

type classicTreeDisplayResult uint8

const (
	classicTreeDisplayDone classicTreeDisplayResult = iota + 1
	classicTreeDisplayExit
	classicTreeDisplayRefresh
)

type classicTreePagerActionKind uint8

const (
	classicTreePagerLine classicTreePagerActionKind = iota + 1
	classicTreePagerPage
	classicTreePagerQuit
	classicTreePagerExit
	classicTreePagerSelect
	classicTreePagerRefresh
	classicTreePagerHelp
	classicTreePagerInvalid
)

type classicTreePagerAction struct {
	kind       classicTreePagerActionKind
	itemNumber int
}

func writeClassicTreeRows(writer io.Writer, rows []string, width int) {
	for _, row := range rows {
		for _, line := range classicTreeLines(row, width) {
			_, _ = io.WriteString(writer, line+utils.CharNewLine)
		}
	}
}

func readClassicTreePagerAction(reader io.Reader, writer io.Writer, canAdvance, allowRefresh bool) (classicTreePagerAction, error) {
	var input [1]byte
	command := classicTreeCommand{writer: writer}
	var escape []byte
	var multibyte []byte
	for {
		n, readErr := reader.Read(input[:])
		for _, key := range input[:n] {
			if len(escape) > 0 {
				if len(escape) != 1 || key == '[' || key == 'O' {
					escape = append(escape, key)
					if key >= 'A' && key <= 'Z' || key >= 'a' && key <= 'z' || key == '~' {
						switch key {
						case 'C':
							command.moveTo(command.cursor + 1)
						case 'D':
							command.moveTo(command.cursor - 1)
						case 'H':
							command.moveTo(0)
						case 'F':
							command.moveTo(len(command.value))
						case '~':
							if string(escape) == "\x1b[3~" {
								command.deleteAt()
							}
						}
						escape = nil
					} else if len(escape) >= 12 || key == '\r' || key == '\n' {
						escape = nil
					} else {
						continue
					}
					if key != '\r' && key != '\n' {
						continue
					}
				} else {
					escape = nil
				}
			}
			if len(multibyte) > 0 || key >= utf8.RuneSelf {
				multibyte = append(multibyte, key)
				if !utf8.FullRune(multibyte) {
					continue
				}
				r, _ := utf8.DecodeRune(multibyte)
				multibyte = nil
				switch {
				case r == '？':
					command.insert('?')
				case unicode.IsSpace(r):
					command.insert(' ')
				}
				continue
			}
			switch key {
			case '\x1b':
				escape = []byte{key}
			case '\r', '\n':
				value := classicShortcut(string(command.value))
				switch value {
				case "":
					if canAdvance {
						return classicTreePagerAction{kind: classicTreePagerLine}, nil
					}
					continue
				case "b":
					return classicTreePagerAction{kind: classicTreePagerQuit}, nil
				case "exit":
					return classicTreePagerAction{kind: classicTreePagerExit}, nil
				case "?":
					return classicTreePagerAction{kind: classicTreePagerHelp}, nil
				case "r":
					if allowRefresh {
						return classicTreePagerAction{kind: classicTreePagerRefresh}, nil
					}
					return classicTreePagerAction{kind: classicTreePagerInvalid}, nil
				}
				for _, digit := range value {
					if digit < '0' || digit > '9' {
						return classicTreePagerAction{kind: classicTreePagerInvalid}, nil
					}
				}
				number, err := strconv.Atoi(value)
				if err != nil {
					return classicTreePagerAction{kind: classicTreePagerInvalid}, nil
				}
				return classicTreePagerAction{kind: classicTreePagerSelect, itemNumber: number}, nil
			case ' ':
				if len(command.value) == 0 {
					if canAdvance {
						return classicTreePagerAction{kind: classicTreePagerPage}, nil
					}
					continue
				}
				command.insert(key)
			case '\b', 127:
				command.backspace()
			case 1:
				command.moveTo(0)
			case 5:
				command.moveTo(len(command.value))
			default:
				if key >= 32 && key < 127 {
					command.insert(key)
				}
			}
		}
		if readErr != nil {
			return classicTreePagerAction{}, readErr
		}
	}
}

type classicTreeCommand struct {
	value  []byte
	cursor int
	writer io.Writer
}

func (c *classicTreeCommand) moveTo(position int) {
	position = max(0, min(position, len(c.value)))
	if position < c.cursor {
		_, _ = fmt.Fprintf(c.writer, "\x1b[%dD", c.cursor-position)
	} else if position > c.cursor {
		_, _ = fmt.Fprintf(c.writer, "\x1b[%dC", position-c.cursor)
	}
	c.cursor = position
}

func (c *classicTreeCommand) insert(key byte) {
	if len(c.value) >= 32 {
		return
	}
	c.value = append(c.value, 0)
	copy(c.value[c.cursor+1:], c.value[c.cursor:])
	c.value[c.cursor] = key
	_, _ = c.writer.Write(c.value[c.cursor:])
	if trailing := len(c.value) - c.cursor - 1; trailing > 0 {
		_, _ = fmt.Fprintf(c.writer, "\x1b[%dD", trailing)
	}
	c.cursor++
}

func (c *classicTreeCommand) deleteAt() {
	if c.cursor >= len(c.value) {
		return
	}
	copy(c.value[c.cursor:], c.value[c.cursor+1:])
	c.value = c.value[:len(c.value)-1]
	_, _ = c.writer.Write(c.value[c.cursor:])
	_, _ = io.WriteString(c.writer, " ")
	_, _ = fmt.Fprintf(c.writer, "\x1b[%dD", len(c.value)-c.cursor+1)
}

func (c *classicTreeCommand) backspace() {
	if c.cursor == 0 {
		return
	}
	c.moveTo(c.cursor - 1)
	c.deleteAt()
}
