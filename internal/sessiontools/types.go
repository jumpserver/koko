package sessiontools

const (
	MaxToolResultBytes = 128 * 1024
	MaxToolSchemaBytes = 64 * 1024
)

// ContextSnapshot is the explicit, credential-free part of a resource session
// context that a tool consumer is allowed to retain.
type ContextSnapshot struct {
	SessionKind      string `json:"session_kind,omitempty"`
	InteractionMode  string `json:"interaction_mode,omitempty"`
	CommandLanguage  string `json:"command_language,omitempty"`
	Dialect          string `json:"dialect,omitempty"`
	Protocol         string `json:"protocol"`
	ConnectionMethod string `json:"connection_method,omitempty"`
	AssetID          string `json:"asset_id,omitempty"`
	AssetName        string `json:"asset_name,omitempty"`
	AssetAddress     string `json:"asset_address,omitempty"`
	PlatformID       int    `json:"platform_id,omitempty"`
	PlatformCategory string `json:"platform_category,omitempty"`
	PlatformType     string `json:"platform_type,omitempty"`
	PlatformName     string `json:"platform_name,omitempty"`
	BaseOS           string `json:"base_os,omitempty"`
	Charset          string `json:"charset,omitempty"`
	Database         string `json:"database,omitempty"`
	Schema           string `json:"schema,omitempty"`
}

type ToolIcon struct {
	Source   string   `json:"src"`
	MIMEType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	Theme    string   `json:"theme,omitempty"`
}
