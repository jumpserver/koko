package handler

import (
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewInteractiveHandler(sess ssh.Session, user *model.User, jmsService *service.JMService,
	termConfig model.TerminalConfig) *InteractiveHandler {
	return newInteractiveHandler(NewWrapperSession(sess), user, jmsService, termConfig, nil, nil)
}

func newInteractiveHandler(sess *WrapperSession, user *model.User, jmsService *service.JMService,
	termConfig model.TerminalConfig, preferences *tuiPreferences, shutdown <-chan struct{}) *InteractiveHandler {
	language := getUserDefaultLangCode(user)
	if preferences != nil {
		language, _, _, _, _ = preferences.display(user.ID, language)
	}
	api := newLangAPIClient(jmsService, language)
	publicSetting, err := api.GetPublicSetting()
	if err != nil {
		logger.Errorf("Get public setting error: %s", err)
	}
	handler := &InteractiveHandler{
		sess: sess, user: user, term: term.NewTerminal(sess, "Opt> "), jmsService: api,
		terminalConf: &termConfig, i18nLang: language, publicSetting: &publicSetting,
		preferences: preferences, shutdown: shutdown,
	}
	handler.Initial()
	return handler
}

type InteractiveHandler struct {
	sess *WrapperSession
	user *model.User
	term *term.Terminal

	selectHandler   *UserSelectHandler
	nodes           model.NodeList
	nodeLoadErr     error
	typeNodes       []classicTypeNode
	favoriteNodes   []classicFavoriteNode
	assetLoadPolicy string
	wg              sync.WaitGroup
	jmsService      *service.JMService
	terminalConf    *model.TerminalConfig
	publicSetting   *model.PublicSetting
	i18nLang        string
	preferences     *tuiPreferences
	shutdown        <-chan struct{}
	manualPasswords tuiManualPasswordAttempts
	classicView     classicView
	helpReturnView  classicView
	treeOrigin      selectType
	treeSelected    bool
	idleState       chan bool
	exitRequested   bool
}

func (h *InteractiveHandler) Initial() {
	conf := config.GetConf()
	h.assetLoadPolicy = strings.ToLower(conf.AssetLoadPolicy)
	h.displayHelp()
	hiddenFields := make(map[string]struct{}, len(conf.HiddenFields))
	for _, field := range conf.HiddenFields {
		hiddenFields[strings.ToLower(strings.TrimSpace(field))] = struct{}{}
	}
	h.selectHandler = &UserSelectHandler{
		user: h.user, h: h, pageInfo: &pageInfo{}, hiddenFields: hiddenFields,
	}
	if h.assetLoadPolicy == "all" {
		allAssets, err := h.jmsService.GetAllUserPermsAssets(h.user.ID)
		if err != nil {
			logger.Errorf("Get all user perms assets failed: %s", err)
			h.assetLoadPolicy = "remote"
		} else {
			h.selectHandler.SetAllLocalData(allAssets)
		}
	}
	h.firstLoadData()
}

func (h *InteractiveHandler) GetPtySize() (int, int) {
	window := h.sess.Pty().Window
	width, height := window.Width, window.Height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return min(500, width), min(200, height)
}

func (h *InteractiveHandler) resizeTerminal() {
	width, height := h.GetPtySize()
	_ = h.term.SetSize(width, height)
}

func (h *InteractiveHandler) firstLoadData() {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.loadUserNodes()
	}()
}

func (h *InteractiveHandler) displayHelp() {
	h.classicView = classicViewHelp
	h.term.SetPrompt("Opt> ")
	h.displayBanner(h.sess, h.user.Name, h.terminalConf)
	h.displayAnnouncement(h.sess, h.publicSetting)
	if h.helpReturnView == classicViewList {
		utils.IgnoreErrWriteString(h.term, utils.WrapperTitle("[b]")+" "+i18n.NewLang(h.i18nLang).T("Back to assets")+utils.CharNewLine)
	}
}

type classicView uint8

const (
	classicViewHelp classicView = iota
	classicViewList
)

func (h *InteractiveHandler) readClassicLine() (string, error) {
	if !h.setIdle(h.idleState, true) {
		return "", io.EOF
	}
	line, err := h.term.ReadLine()
	if !h.setIdle(h.idleState, false) {
		return "", io.EOF
	}
	line = strings.TrimSpace(line)
	if err == nil && line == "exit" {
		h.requestClassicExit()
		return "", io.EOF
	}
	return line, err
}

func (h *InteractiveHandler) requestClassicExit() {
	if h.exitRequested {
		return
	}
	h.exitRequested = true
	message := i18n.NewLang(h.i18nLang).T("Koko session ended.")
	utils.IgnoreErrWriteString(h.term, message+utils.CharNewLine)
}

func classicChoiceNumber(input string, count int) (int, bool) {
	if input == "" {
		return 0, false
	}
	for _, digit := range input {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	number, err := strconv.Atoi(input)
	return number, err == nil && number > 0 && number <= count
}

func (h *InteractiveHandler) readClassicChoice(table string, hints []string, width int,
	prompt string, count int) (number int, back bool, err error) {
	h.resizeTerminal()
	h.term.SetPrompt(prompt)
	utils.IgnoreErrWriteString(h.term, table)
	utils.IgnoreErrWriteString(h.term, classicHintPanel(hints, width))
	for {
		line, readErr := h.readClassicLine()
		if readErr != nil {
			return 0, false, readErr
		}
		if line == "b" {
			return 0, true, nil
		}
		if line == "" {
			continue
		}
		if number, ok := classicChoiceNumber(line, count); ok {
			return number, false, nil
		}
		message := i18n.NewLang(h.i18nLang).T("Invalid input.")
		utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(message))
	}
}

func (h *InteractiveHandler) WatchWinSizeChange(winChan <-chan ssh.Window) {
	defer logger.Infof("Request %s: Windows change watch close", h.sess.Uuid)
	for {
		select {
		case <-h.sess.Sess.Context().Done():
			return
		case win, ok := <-winChan:
			if !ok {
				return
			}
			h.sess.SetWin(win)
			logger.Debugf("Term window size change: %d*%d", win.Height, win.Width)
			_ = h.term.SetSize(min(500, max(1, win.Width)), min(200, max(1, win.Height)))
		}
	}
}

func (h *InteractiveHandler) watchSession(done <-chan struct{}) {
	interval := config.GetConf().ClientAliveInterval
	var keepAlive <-chan time.Time
	var ticker *time.Ticker
	if interval > 0 {
		ticker = time.NewTicker(time.Duration(interval) * time.Second)
		keepAlive = ticker.C
		defer ticker.Stop()
	}
	for {
		select {
		case <-h.shutdown:
			_ = h.sess.Sess.Close()
			return
		case <-done:
			return
		case <-h.sess.Context().Done():
			return
		case <-keepAlive:
			if _, err := h.sess.Sess.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				logger.Errorf("Request %s: Send user %s keepalive packet failed: %s", h.sess.Uuid, h.user.Name, err)
			}
		}
	}
}

func (h *InteractiveHandler) tr(zh, en string) string {
	lang := i18n.NewLang(h.i18nLang)
	if lang == i18n.ZH {
		return zh
	}
	return lang.T(en)
}

func (h *InteractiveHandler) refreshAuthorizationTreeCache() bool {
	h.wg.Wait()
	_, err := h.jmsService.GetUserPermsAssets(h.user.ID, model.PaginationParam{PageSize: 1, Refresh: true})
	if err != nil {
		logger.Errorf("Rebuild user authorization tree error: %s", err)
		h.nodeLoadErr = err
		utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(userFacingErrorMessage(i18n.NewLang(h.i18nLang).T("Core API failed"), err)))
		return false
	}
	nodes, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).authorizationNodes()
	if err != nil {
		logger.Errorf("Refresh user authorization tree error: %s", err)
		h.nodeLoadErr = err
		utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(userFacingErrorMessage(i18n.NewLang(h.i18nLang).T("Core API failed"), err)))
		return false
	}
	h.nodes = nodes
	h.nodeLoadErr = nil
	if _, err := io.WriteString(h.term, i18n.NewLang(h.i18nLang).T("Refresh done")+"\n\r"); err != nil {
		logger.Error("refresh authorization tree err:", err)
	}
	return true
}

func (h *InteractiveHandler) loadUserNodes() {
	nodes, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).authorizationNodes()
	if err != nil {
		logger.Errorf("Get user nodes error: %s", err)
		h.nodeLoadErr = err
		return
	}
	h.nodes = nodes
	h.nodeLoadErr = nil
}

func (h *InteractiveHandler) loadUserTypeNodes() error {
	types, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).typeNodes()
	if err != nil {
		logger.Errorf("Get user type tree error: %s", err)
		return err
	}
	h.typeNodes = types
	return nil
}

func (h *InteractiveHandler) loadUserFavoriteNodes() error {
	favorites, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).favoriteNodes()
	if err != nil {
		logger.Errorf("Get user favorite tree error: %s", err)
		return err
	}
	h.favoriteNodes = favorites
	return nil
}

func getPageSize(h *InteractiveHandler, termConf *model.TerminalConfig) int {
	_, height := h.GetPtySize()
	pageSize := height - 8
	switch termConf.AssetListPageSize {
	case "auto":
	case "all":
		return PAGESIZEALL
	default:
		if value, err := strconv.Atoi(termConf.AssetListPageSize); err == nil {
			pageSize = value
		}
	}
	return max(1, pageSize)
}
