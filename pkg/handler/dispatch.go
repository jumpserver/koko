package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

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
	h.idleState = idleState
	logger.Infof("Request %s: User %s start classic text mode", h.sess.ID(), h.user.Name)
	defer logger.Infof("Request %s: User %s stop classic text mode", h.sess.ID(), h.user.Name)

	for {
		h.resizeTerminal()
		line, err := h.readClassicLine()
		if err != nil {
			logger.Debugf("User %s close connect %s", h.user.Name, err)
			return terminalModeExit
		}
		if mode := h.dispatchClassicInput(line, idleState); mode != terminalModeText {
			return mode
		}
		if h.exitRequested {
			return terminalModeExit
		}
	}
}

func (h *InteractiveHandler) dispatchClassicInput(line string, idleState chan bool) terminalMode {
	if line == "" && h.classicView != classicViewList {
		return terminalModeText
	}
	if line == "?" {
		if h.classicView == classicViewList {
			h.helpReturnView = classicViewList
		}
		h.displayHelp()
		return terminalModeText
	}
	if h.classicView == classicViewHelp {
		return h.dispatchClassicHelp(line, idleState)
	}
	return h.dispatchClassicList(line, idleState)
}

func (h *InteractiveHandler) dispatchClassicHelp(line string, idleState chan bool) terminalMode {
	switch line {
	case "b":
		if h.helpReturnView == classicViewList {
			h.selectHandler.DisplayCurrentResult()
			return terminalModeText
		}
	case "p":
		h.showAllClassicAssets()
		return terminalModeText
	case "g", "c", "f":
		kind := TypeNodeAsset
		switch line {
		case "c":
			kind = TypeTypeAsset
		case "f":
			kind = TypeFavoriteAsset
		}
		if !h.openClassicTree(kind, idleState) {
			return terminalModeExit
		}
		return terminalModeText
	case "s":
		h.ChangeLang()
		if h.exitRequested {
			return terminalModeExit
		}
		h.displayHelp()
		return terminalModeText
	case "t":
		if h.preferences != nil {
			h.preferences.storeTerminalMode(h.user.ID, terminalModeTUI)
		}
		return terminalModeTUI
	case "q":
		logger.Infof("user %s enter %s to exit", h.user.Name, line)
		return terminalModeExit
	}
	if strings.HasPrefix(line, "/") && !strings.HasPrefix(line, "//") {
		h.searchClassicAssets(line[1:])
		return terminalModeText
	}
	h.warnClassicInvalidCommand()
	return terminalModeText
}

func (h *InteractiveHandler) dispatchClassicList(line string, idleState chan bool) terminalMode {
	if line == "" {
		if h.selectHandler.HasNext() {
			h.selectHandler.MoveNextPage()
		}
		return terminalModeText
	}
	switch line {
	case "b":
		if !h.backClassicView(idleState) {
			return terminalModeExit
		}
		return terminalModeText
	case "p":
		if h.selectHandler.HasPrev() {
			h.selectHandler.MovePrePage()
			return terminalModeText
		}
	case "n":
		if h.selectHandler.HasNext() {
			h.selectHandler.MoveNextPage()
			return terminalModeText
		}
	}
	if strings.HasPrefix(line, "//") {
		if term := strings.TrimSpace(line[2:]); term != "" {
			h.selectHandler.SearchAgain(term)
		} else {
			h.warnClassicEmptySearch()
		}
		return terminalModeText
	}
	if strings.HasPrefix(line, "/") {
		h.searchClassicAssets(line[1:])
		return terminalModeText
	}
	if !h.selectHandler.SelectResult(line) {
		if _, err := strconv.Atoi(line); err == nil {
			utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(i18n.NewLang(h.i18nLang).T("Choose a number on this page.")))
		} else {
			h.warnClassicInvalidCommand()
		}
	}
	return terminalModeText
}

func (h *InteractiveHandler) searchClassicAssets(term string) {
	term = strings.TrimSpace(term)
	if term == "" {
		h.warnClassicEmptySearch()
		return
	}
	h.treeOrigin = 0
	h.selectHandler.SetSelectType(TypeAsset)
	h.selectHandler.SearchAndConnectSingle(term)
}

func (h *InteractiveHandler) warnClassicEmptySearch() {
	utils.IgnoreErrWriteString(h.term, i18n.NewLang(h.i18nLang).T("Enter a search term")+utils.CharNewLine)
}

func (h *InteractiveHandler) warnClassicInvalidCommand() {
	message := i18n.NewLang(h.i18nLang).T("Invalid input.")
	utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(message))
}

func (h *InteractiveHandler) showAllClassicAssets() {
	h.treeOrigin = 0
	h.selectHandler.SetSelectType(TypeAsset)
	h.selectHandler.Search("")
}

func (h *InteractiveHandler) redrawClassicView(view classicView) {
	if view == classicViewList {
		h.selectHandler.DisplayCurrentResult()
		return
	}
	h.displayHelp()
}

func (h *InteractiveHandler) backClassicView(idleState chan bool) bool {
	if h.treeOrigin != 0 {
		h.helpReturnView = classicViewHelp
		h.classicView = classicViewHelp
		return h.openClassicTree(h.treeOrigin, idleState)
	}
	h.helpReturnView = classicViewHelp
	h.displayHelp()
	return true
}

func (h *InteractiveHandler) openClassicTree(kind selectType, idleState chan bool) bool {
	source := h.classicView
	h.treeSelected = false
	var ok bool
	switch kind {
	case TypeNodeAsset:
		h.wg.Wait()
		ok = h.displayNodeTree(h.nodes, idleState)
	case TypeTypeAsset:
		if err := h.loadUserTypeNodes(); err != nil {
			h.redrawClassicView(source)
			message := i18n.NewLang(h.i18nLang).T("Failed to load type tree. Try again.")
			utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(userFacingErrorMessage(message, err)))
			return true
		}
		ok = h.displayTypeTree(h.typeNodes, idleState)
	case TypeFavoriteAsset:
		if err := h.loadUserFavoriteNodes(); err != nil {
			h.redrawClassicView(source)
			message := i18n.NewLang(h.i18nLang).T("Failed to load favorites tree. Try again.")
			utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(userFacingErrorMessage(message, err)))
			return true
		}
		ok = h.displayFavoriteTree(h.favoriteNodes, idleState)
	default:
		return true
	}
	if !h.treeSelected && !h.exitRequested {
		h.redrawClassicView(source)
	}
	return ok
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
