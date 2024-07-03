package httpd

import (
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
)

var _ Handler = (*webFolder)(nil)

type webFolder struct {
	ws *UserWebsocket

	done chan struct{}

	volume *UserVolume
}

func (h *webFolder) Name() string {
	return WebFolderName
}

func (h *webFolder) CheckValidation() error {
	apiClient := h.ws.apiClient
	user := h.ws.CurrentUser()
	volOpts := make([]VolumeOption, 0, 5)
	volOpts = append(volOpts, WithUser(user))
	volOpts = append(volOpts, WithAddr(h.ws.ClientIP()))
	params := h.ws.wsParams
	targetId := params.TargetId
	assetId := params.AssetId
	if assetId == "" {
		assetId = targetId
	}
	if h.ws.ConnectToken != nil {
		connectToken := h.ws.ConnectToken
		volOpts = append(volOpts, WithConnectToken(connectToken))
	} else {
		if common.ValidUUIDString(assetId) {
			assets, err := apiClient.GetUserAssetByID(user.ID, assetId)
			if err != nil {
				logger.Errorf("Get user asset %s error: %s", assetId, err)
				return ErrAssetIdInvalid
			}
			if len(assets) != 1 {
				logger.Errorf("Get user more than one asset %s: choose first", targetId)
			}
			volOpts = append(volOpts, WithAsset(&assets[0]))
		}
	}
	h.volume = NewUserVolume(apiClient, volOpts...)
	return nil
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
