package model

type Terminal struct {
	Name           string `json:"name"`
	Comment        string `json:"comment"`
	ServiceAccount struct {
		Id        string `json:"id"`
		Name      string `json:"name"`
		AccessKey struct {
			Id     string `json:"id"`
			Secret string `json:"secret"`
		}
	} `json:"service_account"`
}

type TerminalConf struct {
	AssetListPageSize   string            `json:"TERMINAL_ASSET_LIST_PAGE_SIZE"`
	AssetListSortBy     string            `json:"TERMINAL_ASSET_LIST_SORT_BY"`
	HeaderTitle         string            `json:"TERMINAL_HEADER_TITLE"`
	HostKey             string            `json:"TERMINAL_HOST_KEY" yaml:"HOST_KEY"`
	PasswordAuth        bool              `json:"TERMINAL_PASSWORD_AUTH" yaml:"PASSWORD_AUTH"`
	PublicKeyAuth       bool              `json:"TERMINAL_PUBLIC_KEY_AUTH" yaml:"PUBLIC_KEY_AUTH"`
	CommandStorage      map[string]string `json:"TERMINAL_COMMAND_STORAGE"`
	ReplayStorage       map[string]string `json:"TERMINAL_REPLAY_STORAGE" yaml:"REPLAY_STORAGE"`
	SessionKeepDuration int               `json:"TERMINAL_SESSION_KEEP_DURATION"`
	TelnetRegex         string            `json:"TERMINAL_TELNET_REGEX"`
}

type TerminalTask struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	Args       string `json:"args"`
	IsFinished bool
}
