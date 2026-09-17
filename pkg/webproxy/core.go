package webproxy

import (
	"encoding/json"
	"net/http"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

type webConnectToken struct {
	model.ConnectToken
	config webLoginConfig
}

type coreService struct{ *service.JMService }

func NewCoreService(core *service.JMService) *coreService { return &coreService{core} }

func (s *coreService) GetConnectTokenInfo(id string, expireNow bool) (webConnectToken, error) {
	// ponytail: the pinned SDK drops Web script fields. Decode the asset
	// with the existing authenticated client until the SDK exposes these fields.
	var response struct {
		model.ConnectToken
		Asset json.RawMessage `json:"asset"`
	}
	// CloneClient drops signing and organization headers; Call retains both.
	_, err := s.Call(http.MethodPost, service.SuperConnectTokenSecretURL, map[string]interface{}{
		"id": id, "expire_now": expireNow,
	}, &response)
	result := webConnectToken{ConnectToken: response.ConnectToken}
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(response.Asset, &result.Asset); err != nil {
		return result, err
	}
	var asset struct {
		SpecInfo webLoginConfig `json:"spec_info"`
	}
	err = json.Unmarshal(response.Asset, &asset)
	result.config = asset.SpecInfo
	return result, err
}
