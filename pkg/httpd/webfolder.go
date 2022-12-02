package httpd

import (
	"encoding/json"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

var _ Handler = (*webFolder)(nil)

type webFolder struct {
	ws *UserWebsocket

	done chan struct{}

	targetId string

	tokenId string
	assetId string

	connectToken *model.ConnectToken

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
	user := h.ws.CurrentUser()
	volOpts := make([]VolumeOption, 0, 5)
	volOpts = append(volOpts, WithUser(user))
	volOpts = append(volOpts, WithAddr(h.ws.ClientIP()))
	assetId := h.assetId
	if assetId == "" {
		assetId = h.targetId
	}
	if common.ValidUUIDString(assetId) {
		assets, err := jmsServiceCopy.GetUserAssetByID(user.ID, assetId)
		if err != nil {
			logger.Errorf("Get user asset %s error: %s", assetId, err)
			data, _ := json.Marshal(&Message{
				Id:   h.ws.Uuid,
				Type: TERMINALERROR,
				Err:  "Core API err",
			})
			h.ws.conn.WriteText(data, maxWriteTimeOut)
			return false
		}
		if len(assets) != 1 {
			logger.Errorf("Get user more than one asset %s: choose first", h.targetId)
		}
		volOpts = append(volOpts, WithAsset(&assets[0]))
	}
	if h.tokenId != "" {
		connectToken, err := jmsServiceCopy.GetConnectTokenInfo(h.tokenId)
		if err != nil {
			logger.Errorf("Get connect token info %s error: %s", h.tokenId, err)
			data, _ := json.Marshal(&Message{
				Id:   h.ws.Uuid,
				Type: TERMINALERROR,
				Err:  "Core API err",
			})
			h.ws.conn.WriteText(data, maxWriteTimeOut)
			return false
		}
		volOpts = append(volOpts, WithConnectToken(&connectToken))
	}
	h.volume = NewUserVolume(jmsServiceCopy, volOpts...)
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
