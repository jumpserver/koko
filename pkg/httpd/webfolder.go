package httpd

import (
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/terminalai"
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
	if volume, _, err := SftpCheckValidation(h.ws); err != nil {
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

type sftpTerminalConfig struct {
	model.TerminalConfig
	ChatAIEnabled  *bool  `json:"CHAT_AI_ENABLED"`
	ChatAIMethod   string `json:"CHAT_AI_METHOD"`
	ChatAIProvider string `json:"CHAT_AI_PROVIDER"`
	ChatAIBaseURL  string `json:"CHAT_AI_BASE_URL"`
	ChatAIAPIKey   string `json:"CHAT_AI_API_KEY"`
	ChatAIProxy    string `json:"CHAT_AI_PROXY"`
	ChatAIModel    string `json:"CHAT_AI_MODEL"`
}

func (c sftpTerminalConfig) fileAISettings() terminalai.Settings {
	return terminalai.Settings{
		Enabled: c.ChatAIEnabled, Method: c.ChatAIMethod,
		Provider: c.ChatAIProvider, BaseURL: c.ChatAIBaseURL,
		APIKey: c.ChatAIAPIKey, Proxy: c.ChatAIProxy, Model: c.ChatAIModel,
		ChatAIType: c.TerminalConfig.ChatAIType,
		GptBaseUrl: c.TerminalConfig.GptBaseUrl,
		GptApiKey:  c.TerminalConfig.GptApiKey,
		GptProxy:   c.TerminalConfig.GptProxy,
		GptModel:   c.TerminalConfig.GptModel,
	}
}

func SftpCheckValidation(
	ws *UserWebsocket,
) (*UserVolume, terminalai.Settings, error) {
	apiClient := ws.apiClient
	user := ws.CurrentUser()
	var combinedConfig sftpTerminalConfig
	_, err := ws.apiClient.Call(
		"GET", service.TerminalConfigURL, nil, &combinedConfig,
	)

	uv := &UserVolume{}
	if err != nil {
		logger.Errorf("Get terminal config failed: %s", err)
		return uv, terminalai.Settings{}, err
	}
	terminalCfg := combinedConfig.TerminalConfig
	fileAISettings := combinedConfig.fileAISettings()
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
				logger.Errorf("Get user asset %s error: %s", assetId, err1)
				return uv, fileAISettings, ErrAssetIdInvalid
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

	return NewUserVolume(apiClient, volOpts...), fileAISettings, nil
}
