package httpd

import (
	"github.com/jumpserver-dev/sdk-go/model"
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
	if volume, err := SftpCheckValidation(h.ws); err != nil {
		return err
	} else {
		h.volume = volume
	}
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

func SftpCheckValidation(ws *UserWebsocket) (*UserVolume, error) {
	apiClient := ws.apiClient
	user := ws.CurrentUser()
	terminalCfg, err := ws.apiClient.GetTerminalConfig()

	uv := &UserVolume{}
	if err != nil {
		logger.Errorf("Get terminal config failed: %s", err)
		return uv, err
	}
	volOpts := make([]VolumeOption, 0, 5)
	volOpts = append(volOpts, WithUser(user))
	volOpts = append(volOpts, WithAddr(ws.ClientIP()))
	volOpts = append(volOpts, WithTerminalCfg(&terminalCfg))
	params := ws.wsParams
	targetId := params.TargetId
	assetId := params.AssetId
	if assetId == "" {
		assetId = targetId
	}
	if ws.ConnectToken != nil {
		connectToken := ws.ConnectToken
		volOpts = append(volOpts, WithConnectToken(connectToken))
	} else {
		if common.ValidUUIDString(assetId) {
			detailAsset, err1 := apiClient.GetUserPermAssetDetailById(user.ID, assetId)
			if err1 != nil {
				logger.Errorf("Get user asset %s error: %s", assetId, err)
				return uv, ErrAssetIdInvalid
			}
			permAsset := &model.PermAsset{
				ID:       detailAsset.ID,
				Name:     detailAsset.Name,
				Address:  detailAsset.Address,
				Comment:  detailAsset.Comment,
				Platform: detailAsset.Platform,
				OrgID:    detailAsset.OrgID,
				OrgName:  detailAsset.OrgName,
				IsActive: detailAsset.IsActive,
				Type:     detailAsset.Type,
				Category: detailAsset.Category,
			}
			volOpts = append(volOpts, WithAsset(permAsset))
		}
	}

	return NewUserVolume(apiClient, volOpts...), nil
}
