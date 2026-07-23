package terminalai

import "github.com/jumpserver-dev/sdk-go/model"

func NewSessionContext(token *model.ConnectToken) SessionContext {
	if token == nil {
		return SessionContext{}
	}
	return normalizeSessionContext(SessionContext{
		Protocol:         token.Protocol,
		AssetName:        token.Asset.Name,
		PlatformCategory: labelValue(token.Platform.Category),
		PlatformType:     labelValue(token.Platform.Type),
		PlatformName:     token.Platform.Name,
		BaseOS:           token.Platform.BaseOs,
		Charset:          labelValue(token.Platform.Charset),
		Database:         token.Asset.SpecInfo.DBName,
	})
}

func labelValue(value model.LabelValue) string {
	if value.Value != "" {
		return value.Value
	}
	return value.Label
}
