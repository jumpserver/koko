package handler

import (
	"errors"
	"strings"

	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/logger"
)

const (
	assetTUIMinWidth  = 80
	assetTUIMinHeight = 24
)

type terminalInterfaceMode string

const (
	terminalInterfaceModeTUI  terminalInterfaceMode = "tui"
	terminalInterfaceModeText terminalInterfaceMode = "text"
)

type terminalMouseMode string

const (
	terminalMouseModeKoko   terminalMouseMode = "koko"
	terminalMouseModeClient terminalMouseMode = "client"
)

type terminalPreference struct {
	Basic terminalPreferenceBasic `json:"basic"`
}

type terminalPreferenceBasic struct {
	InterfaceMode terminalInterfaceMode `json:"interface_mode,omitempty"`
	MouseMode     terminalMouseMode     `json:"mouse_mode,omitempty"`
}

func normalizeTerminalInterfaceMode(value terminalInterfaceMode) terminalInterfaceMode {
	if value == terminalInterfaceModeText {
		return value
	}
	return terminalInterfaceModeTUI
}

func normalizeTerminalMouseMode(value terminalMouseMode) terminalMouseMode {
	if value == terminalMouseModeClient {
		return value
	}
	return terminalMouseModeKoko
}

func terminalWindowSupportsTUI(width, height int) bool {
	width, height = assetTUITerminalSize(width, height)
	return width >= assetTUIMinWidth && height >= assetTUIMinHeight
}

func (h *InteractiveHandler) loadTerminalPreference() {
	h.interfaceMode = terminalInterfaceModeTUI
	h.mouseMode = terminalMouseModeKoko
	client, err := h.terminalPreferenceClient()
	if err != nil {
		logger.Warnf("Load user terminal preference skipped: %s", err)
		return
	}
	var preference terminalPreference
	if _, err = client.Get(service.UserKoKoPreferenceURL, &preference); err != nil {
		logger.Warnf("Load user terminal preference failed: %s", err)
		return
	}
	h.interfaceMode = normalizeTerminalInterfaceMode(preference.Basic.InterfaceMode)
	h.mouseMode = normalizeTerminalMouseMode(preference.Basic.MouseMode)
}

func (h *InteractiveHandler) saveTerminalPreference(interfaceMode terminalInterfaceMode,
	mouseMode terminalMouseMode) error {
	client, err := h.terminalPreferenceClient()
	if err != nil {
		return err
	}
	preference := terminalPreference{Basic: terminalPreferenceBasic{
		InterfaceMode: interfaceMode,
		MouseMode:     mouseMode,
	}}
	_, err = client.Patch(service.UserKoKoPreferenceURL, preference, nil)
	return err
}

func (h *InteractiveHandler) terminalPreferenceClient() (*httplib.Client, error) {
	if strings.TrimSpace(h.preferenceToken) == "" {
		return nil, errors.New("user authentication token is unavailable")
	}
	client := h.jmsService.CloneClient()
	client.SetAuthSign(&httplib.BearerTokenAuth{Token: h.preferenceToken})
	return &client, nil
}
