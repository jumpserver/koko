package handler

import (
	"io"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"

	"github.com/jumpserver/koko/internal/tui"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

type terminalMode uint8

const (
	terminalModeExit terminalMode = iota
	terminalModeTUI
	terminalModeText
)

// routedSSHSession keeps WrapperSession as the only reader of the SSH channel.
// TUI readers can then be detached without losing the login session, allowing
// the same connection to return to the line-oriented interface.
type routedSSHSession struct {
	ssh.Session
	input *WrapperSession
}

func (s *routedSSHSession) Read(p []byte) (int, error) { return s.input.Read(p) }

func (s *routedSSHSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	pty, _, ok := s.Session.Pty()
	pty.Window = s.input.Pty().Window
	return pty, s.input.WinCh(), ok
}

func (s *Server) runTerminalModes(sess ssh.Session, user *model.User, termConf model.TerminalConfig,
	winChan <-chan ssh.Window) {
	select {
	case <-s.tuiShutdown:
		return
	default:
	}
	input := NewWrapperSession(sess)
	routingDone := make(chan struct{})
	defer close(routingDone)
	go func() {
		for {
			select {
			case win, ok := <-winChan:
				if !ok {
					return
				}
				input.SetWin(win)
			case <-sess.Context().Done():
				return
			case <-routingDone:
				return
			}
		}
	}()

	mode := terminalModeTUI
	if s.tuiPreferences != nil {
		mode = s.tuiPreferences.terminalMode(user.ID)
	}
	for mode != terminalModeExit {
		select {
		case <-s.tuiShutdown:
			return
		default:
		}
		switch mode {
		case terminalModeTUI:
			mode = s.runTerminalUI(sess, input, user, termConf)
		case terminalModeText:
			mode = newInteractiveHandler(input, user, s.jmsService, termConf, s.tuiPreferences, s.tuiShutdown).Dispatch()
		default:
			return
		}
	}
}

func (s *Server) runTerminalUI(sess ssh.Session, input *WrapperSession, user *model.User,
	termConf model.TerminalConfig) terminalMode {
	language := getUserDefaultLangCode(user)
	if s.tuiPreferences != nil {
		language, _, _, _, _ = s.tuiPreferences.display(user.ID, language)
	}
	routed := &routedSSHSession{Session: sess, input: input}
	screen, err := tui.NewSSHScreen(routed, input.WinCh())
	if err != nil {
		logger.Warnf("TUI terminal unavailable: %s", err)
		writeTUIFallbackNotice(sess, language)
		return terminalModeText
	}
	if err = screen.Init(); err != nil {
		logger.Errorf("Initialize TUI: %s", err)
		screen.Fini()
		_ = input.Close()
		writeTUIFallbackNotice(sess, language)
		return terminalModeText
	}

	var bearerToken string
	if client, ok := sess.Context().Value(auth.ContextKeyClient).(*auth.UserAuthClient); ok {
		bearerToken = client.BearerToken()
	}
	userAPI, userAPIErr := newUserAPIClient(bearerToken, language)
	if userAPIErr != nil {
		logger.Warnf("Initialize TUI user API: %s", userAPIErr)
	}
	uiHandler := newTerminalUI(routed, user, s.jmsService, userAPI, termConf, screen, s.tuiPreferences)
	uiHandler.shutdown = s.tuiShutdown
	if err = uiHandler.run(); err != nil {
		logger.Errorf("TUI session %s: %s", sess.User(), err)
	}
	screen.Fini()
	// Release the TUI reader and prepare a fresh input pipe before another mode
	// starts. WrapperSession itself continues owning the underlying SSH reader.
	_ = input.Close()
	return uiHandler.nextMode
}

func writeTUIFallbackNotice(sess io.Writer, language string) {
	message := "TUI is unavailable; switched to text mode."
	if strings.HasPrefix(strings.ToLower(language), "zh") {
		message = "当前终端无法运行 TUI，已切换到纯文本模式。"
	}
	utils.IgnoreErrWriteString(sess, message+"\r\n")
}
