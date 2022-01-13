package httpd

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"strings"
)

var _ Handler = (*webFolder)(nil)

type webFolder struct {
	ws *UserWebsocket

	done chan struct{}

	targetId string

	volume *UserVolume

	jmsService *service.JMService
}

func (h *webFolder) Name() string {
	return WebFolderName
}

func (h *webFolder) CheckValidation() bool {
	jmsServiceCopy := h.jmsService.Copy()
	if langCode, err := h.ws.ctx.Cookie("django_language"); err == nil {
		jmsServiceCopy.SetCookie("django_language", langCode)
	}
	switch strings.TrimSpace(h.targetId) {
	case "_":
		h.volume = NewUserVolume(jmsServiceCopy, h.ws.CurrentUser(), h.ws.ClientIP(), "")
	default:
		h.volume = NewUserVolume(jmsServiceCopy, h.ws.CurrentUser(), h.ws.ClientIP(), strings.TrimSpace(h.targetId))
	}
	return true
}

func (h *webFolder) HandleMessage(*Message) {
}

func (h *webFolder) CleanUp() {
	close(h.done)
	h.volume.Close()
}

func (h *webFolder) GetVolume() *UserVolume {
	select {
	case <-h.done:
		return nil
	default:
		return h.volume
	}
}
