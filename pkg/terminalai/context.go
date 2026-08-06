package terminalai

import (
	"fmt"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
)

func NewSessionContext(token *model.ConnectToken) SessionContext {
	if token == nil {
		return SessionContext{}
	}
	return normalizeSessionContext(SessionContext{
		Protocol:         token.Protocol,
		ConnectionMethod: connectionMethod(token.ConnectMethod),
		AssetID:          token.Asset.ID,
		AssetName:        token.Asset.Name,
		AssetAddress:     token.Asset.Address,
		OrganizationID:   token.OrgId,
		PlatformID:       token.Platform.ID,
		PlatformCategory: labelValue(token.Platform.Category),
		PlatformType:     labelValue(token.Platform.Type),
		PlatformName:     token.Platform.Name,
		BaseOS:           token.Platform.BaseOs,
		Charset:          labelValue(token.Platform.Charset),
		Database:         token.Asset.SpecInfo.DBName,
		Attributes:       scalarMetadata(token.Platform.MetaData),
	})
}

func labelValue(value model.LabelValue) string {
	if value.Value != "" {
		return value.Value
	}
	return value.Label
}

func connectionMethod(value model.ConnectMethod) string {
	for _, candidate := range []string{
		value.Value, value.Type, value.Component,
	} {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	return ""
}

func scalarMetadata(values map[string]any) map[string]string {
	result := make(map[string]string)
	for key, value := range values {
		switch value.(type) {
		case string, bool,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			result[key] = fmt.Sprint(value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
