package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *InteractiveHandler) Dispatch() terminalMode {
	done := make(chan struct{})
	defer close(done)
	go h.watchSession(done)

	var idleState chan bool
	if h.terminalConf.MaxIdleTime > 0 {
		idleState = make(chan bool)
		go h.watchIdle(done, idleState)
	}
	logger.Infof("Request %s: User %s start classic text mode", h.sess.ID(), h.user.Name)
	defer logger.Infof("Request %s: User %s stop classic text mode", h.sess.ID(), h.user.Name)

	var initialized bool
	for {
		h.resizeTerminal()
		if !h.setIdle(idleState, true) {
			return terminalModeExit
		}
		line, err := h.term.ReadLine()
		if !h.setIdle(idleState, false) {
			return terminalModeExit
		}
		if err != nil {
			logger.Debugf("User %s close connect %s", h.user.Name, err)
			return terminalModeExit
		}
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if isClassicTUISwitchCommand(lower) {
			if h.preferences != nil {
				h.preferences.storeTerminalMode(h.user.ID, terminalModeTUI)
			}
			return terminalModeTUI
		}
		if line == "" {
			if !initialized {
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search("")
			}
			initialized = true
			continue
		}
		if isClassicHelpCommand(line) {
			h.displayHelp()
			initialized = false
			continue
		}
		initialized = true
		if len(line) == 1 {
			switch lower {
			case "/":
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search("")
				continue
			case "p", "h", "d", "k":
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search("")
				continue
			case "b":
				h.selectHandler.MovePrePage()
				continue
			case "n":
				h.selectHandler.MoveNextPage()
				continue
			case "g":
				h.wg.Wait()
				if !h.displayNodeTree(h.nodes, idleState) {
					return terminalModeExit
				}
				continue
			case "c":
				if !h.loadUserTypeNodes() {
					continue
				}
				if !h.displayTypeTree(h.typeNodes, idleState) {
					return terminalModeExit
				}
				continue
			case "f":
				if !h.loadUserFavoriteNodes() {
					continue
				}
				if !h.displayFavoriteTree(h.favoriteNodes, idleState) {
					return terminalModeExit
				}
				continue
			case "s":
				h.ChangeLang()
				h.displayHelp()
				initialized = false
				continue
			case "q":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return terminalModeExit
			}
		} else {
			switch {
			case lower == "exit" || lower == "quit":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return terminalModeExit
			case strings.HasPrefix(line, "//"):
				h.selectHandler.SearchAgain(strings.TrimSpace(line[2:]))
				continue
			case strings.HasPrefix(line, "/"):
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search(strings.TrimSpace(line[1:]))
				continue
			case strings.HasPrefix(lower, "g"):
				if num, ok := classicTreeSelectionNumber(lower, 'g'); ok {
					h.wg.Wait()
					if num > 0 && num <= len(h.nodes) {
						h.selectHandler.SetNode(h.nodes[num-1])
						h.selectHandler.Search("")
						continue
					}
				}
			case strings.HasPrefix(lower, "c"):
				if num, ok := classicTreeSelectionNumber(lower, 'c'); ok {
					if len(h.typeNodes) == 0 && !h.loadUserTypeNodes() {
						continue
					}
					if num > 0 && num <= len(h.typeNodes) {
						h.selectHandler.SetType(h.typeNodes[num-1])
						h.selectHandler.Search("")
						continue
					}
				}
			case strings.HasPrefix(lower, "f"):
				if num, ok := classicTreeSelectionNumber(lower, 'f'); ok {
					if len(h.favoriteNodes) == 0 && !h.loadUserFavoriteNodes() {
						continue
					}
					if num > 0 && num <= len(h.favoriteNodes) {
						h.selectHandler.SetFavorite(h.favoriteNodes[num-1])
						h.selectHandler.Search("")
						continue
					}
				}
			}
		}
		if h.selectHandler.SelectResult(line) {
			continue
		}
		message := h.tr(
			"请使用 / + IP、主机名或备注搜索，或输入 p 查看授权的资产",
			"Use / + IP, hostname, or comment to search, or enter p to list all permitted assets",
		)
		utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(message))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	}
}

func isClassicHelpCommand(input string) bool {
	input = strings.TrimSpace(input)
	return input == "?" || input == "？"
}

func isClassicTUISwitchCommand(input string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "t" || input == "tui" || input == "mode tui"
}

func classicTreeSelectionNumber(input string, prefix byte) (int, bool) {
	input = strings.TrimSpace(input)
	if len(input) == 0 || input[0] != prefix && input[0] != prefix-('a'-'A') {
		return 0, false
	}
	value := strings.TrimSpace(input[1:])
	value = strings.TrimSpace(strings.TrimPrefix(value, "+"))
	number, err := strconv.Atoi(value)
	return number, err == nil
}

func (h *InteractiveHandler) setIdle(state chan bool, active bool) bool {
	if state == nil {
		return true
	}
	select {
	case state <- active:
		return true
	case <-h.shutdown:
		return false
	case <-h.sess.Context().Done():
		return false
	}
}

func (h *InteractiveHandler) watchIdle(done <-chan struct{}, state <-chan bool) {
	duration := time.Duration(h.terminalConf.MaxIdleTime) * time.Minute
	language := h.i18nLang
	timer := time.NewTimer(duration)
	defer timer.Stop()
	active := true
	for {
		var timeout <-chan time.Time
		if active {
			timeout = timer.C
		}
		select {
		case active = <-state:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if active {
				timer.Reset(duration)
			}
		case <-timeout:
			message := fmt.Sprintf(i18n.NewLang(language).T("Connect idle more than %d minutes, disconnect"),
				h.terminalConf.MaxIdleTime)
			_, _ = io.WriteString(h.sess, "\r\n"+message+"\r\n")
			_ = h.sess.Sess.Close()
			logger.Infof("User %s input idle more than %d minutes", h.user.Name, h.terminalConf.MaxIdleTime)
			return
		case <-done:
			return
		case <-h.sess.Context().Done():
			return
		}
	}
}

func (h *InteractiveHandler) ChangeLang() {
	lang := i18n.NewLang(h.i18nLang)
	language := h.i18nLang
	labels := []string{lang.T("ID"), lang.T("Name")}
	fields := []string{"ID", "Name"}
	data := make([]map[string]string, len(i18n.AllLangCodesStr))
	for i, name := range i18n.AllLangCodesStr {
		data[i] = map[string]string{"ID": strconv.Itoa(i + 1), "Name": name}
	}
	width, _ := h.GetPtySize()
	table := common.WrapperTable{
		Fields: fields, Labels: labels, FieldsSize: map[string][3]int{"ID": {0, 0, 5}, "Name": {0, 8, 0}},
		Data: data, TotalSize: width, TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	h.resizeTerminal()
	h.term.SetPrompt("ID> ")
	hints := compactClassicHintRows(width,
		fmt.Sprintf("[%s] %s", lang.T("Number"), lang.T("Select")),
		fmt.Sprintf("[b] %s", lang.T("Back")))
	for range 3 {
		utils.IgnoreErrWriteString(h.term, table.Display())
		utils.IgnoreErrWriteString(h.term, classicHintPanel(hints, width))
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Errorf("User %s switch language err %s", h.user.Name, err)
			break
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "b", "quit", "exit", "back":
			logger.Infof("User %s switch language exit", h.user.Name)
			return
		case "":
			continue
		}
		if num, parseErr := strconv.Atoi(line); parseErr == nil && num > 0 && num <= len(i18n.AllCodes) {
			lang = i18n.AllCodes[num-1]
			language = lang.String()
			break
		}
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(lang.T("Invalid ID"), utils.Red))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	}
	if language != h.i18nLang {
		setAPIClientLang(h.jmsService, language)
		if h.preferences != nil {
			h.preferences.storeLanguage(h.user.ID, language)
		}
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(lang.T("Switch language successfully"), utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	}
	h.i18nLang = language
}

func (h *InteractiveHandler) displayNodeTree(nodes model.NodeList, idleState chan bool) bool {
	for {
		lang := i18n.NewLang(h.i18nLang)
		prefixes, orderedNodes := constructNodeTreeRows(nodes)
		h.nodes = orderedNodes
		_, _ = io.WriteString(h.term, "\n\r"+lang.T("Node: [ Number.Name(Asset amount) ]")+utils.CharNewLine)
		rows := make([]string, len(h.nodes))
		for i := range h.nodes {
			rows[i] = fmt.Sprintf("%s%d.%s(%d)%s", prefixes[i], i+1,
				h.nodes[i].Name, h.nodes[i].AssetsAmount, utils.CharNewLine)
		}
		result := h.displayClassicTreeRows(rows, 'g', "节点", "node", true, idleState, func(number int) bool {
			if number <= 0 || number > len(h.nodes) {
				return false
			}
			h.selectHandler.SetNode(h.nodes[number-1])
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
	lang := i18n.NewLang(h.i18nLang)
	prefixes, orderedNodes := constructTypeTreeRows(nodes)
	h.typeNodes = orderedNodes
	_, _ = io.WriteString(h.term, "\n\r"+lang.T("Type: [ Number.Name(Asset amount) ]")+utils.CharNewLine)
	if len(h.typeNodes) == 0 {
		return true
	}
	rows := make([]string, len(h.typeNodes))
	for i := range h.typeNodes {
		rows[i] = fmt.Sprintf("%s%d.%s(%d)%s", prefixes[i], i+1,
			h.typeNodes[i].Name, h.typeNodes[i].AssetsAmount, utils.CharNewLine)
	}
	return h.displayClassicTreeRows(rows, 'c', "类型", "type", false, idleState, func(number int) bool {
		if number <= 0 || number > len(h.typeNodes) {
			return false
		}
		h.selectHandler.SetType(h.typeNodes[number-1])
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
	_, _ = io.WriteString(h.term, "\n\r"+lang.T("Favorite: [ Number.Name(Asset amount) ]")+utils.CharNewLine)
	if len(h.favoriteNodes) == 0 {
		return true
	}
	rows := make([]string, len(h.favoriteNodes))
	for i := range h.favoriteNodes {
		rows[i] = fmt.Sprintf("%s%d.%s(%d)%s", prefixes[i], i+1,
			h.favoriteNodes[i].Name, h.favoriteNodes[i].AssetsAmount, utils.CharNewLine)
	}
	return h.displayClassicTreeRows(rows, 'f', "收藏", "favorite", false, idleState, func(number int) bool {
		if number <= 0 || number > len(h.favoriteNodes) {
			return false
		}
		h.selectHandler.SetFavorite(h.favoriteNodes[number-1])
		h.selectHandler.Search("")
		return true
	}) != classicTreeDisplayExit
}

func (h *InteractiveHandler) displayClassicTreeRows(rows []string, selectKey byte, itemZH, itemEN string,
	allowRefresh bool, idleState chan bool, selectItem func(int) bool) classicTreeDisplayResult {
	width, height := h.GetPtySize()
	// Reserve the heading, compact hint panel, and final tree input prompt.
	pageSize := max(1, height-7)
	end := min(pageSize, len(rows))
	writeClassicTreeRows(h.term, rows[:end])
	for {
		lang := i18n.NewLang(h.i18nLang)
		hints := make([]string, 0, 3)
		if end < len(rows) {
			progressTip := fmt.Sprintf(lang.T("Tree progress: %d/%d"), end, len(rows))
			hints = compactClassicHintRows(width, progressTip,
				lang.T("Browse tree: [Enter] next line  [Space] next page"))
		}
		actionHints := make([]string, 0, 3)
		if len(rows) > 0 {
			actionHints = append(actionHints,
				fmt.Sprintf("[%c+%s] %s", selectKey, lang.T("Number"), lang.T("Assets")))
		}
		if allowRefresh {
			actionHints = append(actionHints, fmt.Sprintf("[r] %s", lang.T("Refresh")))
		}
		actionHints = append(actionHints, fmt.Sprintf("[b] %s", lang.T("Back")))
		hints = append(hints, compactClassicHintRows(width, actionHints...)...)
		pager := classicHintPanel(hints, width)
		utils.IgnoreErrWriteString(h.term, pager)
		utils.IgnoreErrWriteString(h.term, classicTreeInputPrompt(end, len(rows)))
		if !h.setIdle(idleState, true) {
			return classicTreeDisplayExit
		}
		action, err := readClassicTreePagerAction(h.sess, h.term, selectKey, allowRefresh)
		if !h.setIdle(idleState, false) {
			return classicTreeDisplayExit
		}
		clearClassicHintPanel(h.term, pager)
		if err != nil {
			logger.Debugf("User %s close tree pager: %s", h.user.Name, err)
			return classicTreeDisplayExit
		}
		switch action.kind {
		case classicTreePagerQuit:
			utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
			return classicTreeDisplayDone
		case classicTreePagerRefresh:
			return classicTreeDisplayRefresh
		case classicTreePagerSelect:
			if !selectItem(action.itemNumber) {
				message := fmt.Sprintf(h.tr("%s编号无效", "Invalid %s number"), h.tr(itemZH, itemEN))
				utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(message)+utils.CharNewLine)
				continue
			}
			return classicTreeDisplayDone
		case classicTreePagerInvalid:
			message := fmt.Sprintf(h.tr("请输入 %c+编号并回车", "Enter %c+number and press Enter"), selectKey)
			utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(message)+utils.CharNewLine)
		case classicTreePagerLine:
			if end < len(rows) {
				writeClassicTreeRows(h.term, rows[end:min(end+1, len(rows))])
				end++
			}
		case classicTreePagerPage:
			if end < len(rows) {
				next := min(end+pageSize, len(rows))
				writeClassicTreeRows(h.term, rows[end:next])
				end = next
			}
		}
	}
}

func classicTreeInputPrompt(displayed, total int) string {
	if displayed >= total {
		return "[tree]> "
	}
	return ": "
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
	classicTreePagerSelect
	classicTreePagerRefresh
	classicTreePagerInvalid
)

type classicTreePagerAction struct {
	kind       classicTreePagerActionKind
	itemNumber int
}

func writeClassicTreeRows(writer io.Writer, rows []string) {
	for _, row := range rows {
		_, _ = io.WriteString(writer, row)
	}
}

func readClassicTreePagerAction(reader io.Reader, writer io.Writer, selectKey byte, allowRefresh bool) (classicTreePagerAction, error) {
	var input [32]byte
	var command strings.Builder
	for {
		n, readErr := reader.Read(input[:])
		for _, key := range input[:n] {
			switch key {
			case '\r', '\n':
				value := strings.TrimSpace(command.String())
				switch strings.ToLower(value) {
				case "":
					return classicTreePagerAction{kind: classicTreePagerLine}, nil
				case "b":
					return classicTreePagerAction{kind: classicTreePagerQuit}, nil
				case "r":
					if allowRefresh {
						return classicTreePagerAction{kind: classicTreePagerRefresh}, nil
					}
					return classicTreePagerAction{kind: classicTreePagerInvalid}, nil
				}
				number, ok := classicTreeSelectionNumber(value, selectKey)
				if !ok {
					return classicTreePagerAction{kind: classicTreePagerInvalid}, nil
				}
				return classicTreePagerAction{kind: classicTreePagerSelect, itemNumber: number}, nil
			case ' ':
				if command.Len() == 0 {
					return classicTreePagerAction{kind: classicTreePagerPage}, nil
				}
				command.WriteByte(key)
				_, _ = writer.Write([]byte{key})
			case 'n', 'N':
				if command.Len() == 0 {
					return classicTreePagerAction{kind: classicTreePagerPage}, nil
				}
			case '\b', 127:
				value := command.String()
				if len(value) > 0 {
					command.Reset()
					command.WriteString(value[:len(value)-1])
					_, _ = io.WriteString(writer, "\b \b")
				}
			case 3, 4, 27:
				return classicTreePagerAction{kind: classicTreePagerQuit}, nil
			case 'r', 'R':
				if allowRefresh {
					command.WriteByte(key)
					_, _ = writer.Write([]byte{key})
				}
			case 'b', 'B', '+', '\t', selectKey, selectKey - ('a' - 'A'):
				command.WriteByte(key)
				_, _ = writer.Write([]byte{key})
			default:
				if key >= '0' && key <= '9' {
					command.WriteByte(key)
					_, _ = writer.Write([]byte{key})
				}
			}
		}
		if readErr != nil {
			return classicTreePagerAction{}, readErr
		}
	}
}
