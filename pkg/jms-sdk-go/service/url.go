package service

// 与Core交互的API
const (
	UserProfileURL       = "/api/v1/users/profile/"                   // 获取当前用户的基本信息
	TerminalRegisterURL  = "/api/v1/terminal/terminal-registrations/" // 注册
	TerminalConfigURL    = "/api/v1/terminal/terminals/config/"       // 获取配置
	TerminalHeartBeatURL = "/api/v1/terminal/terminals/status/"

	TerminalEncryptedConfigURL = "/api/v1/terminal/encrypted-config/"
)

// 用户登陆认证使用的API
const (
	UserTokenAuthURL   = "/api/v1/authentication/tokens/" // 用户登录验证
	UserConfirmAuthURL = "/api/v1/authentication/login-confirm-ticket/status/"
	AuthMFASelectURL   = "/api/v1/authentication/mfa/select/" // 选择 MFA

)

// Session相关API
const (
	SessionListURL      = "/api/v1/terminal/sessions/"           //上传创建的资产会话session id
	SessionDetailURL    = "/api/v1/terminal/sessions/%s/"        // finish session的时候发送
	SessionReplayURL    = "/api/v1/terminal/sessions/%s/replay/" //上传录像
	SessionCommandURL   = "/api/v1/terminal/commands/"           //上传批量命令
	FinishTaskURL       = "/api/v1/terminal/tasks/%s/"
	JoinRoomValidateURL = "/api/v1/terminal/sessions/join/validate/"
	FTPLogListURL       = "/api/v1/audits/ftp-logs/" // 上传 ftp日志
	FTPLogUpdateURL     = "/api/v1/audits/ftp-logs/%s/"
	FTPLogFileURL       = "/api/v1/audits/ftp-logs/%s/upload/"

	SessionLifecycleLogURL = "/api/v1/terminal/sessions/%s/lifecycle_log/"
)

// 授权相关API
const (
	UserPermsNodesListURL         = "/api/v1/perms/users/%s/nodes/"
	UserPermsNodeAssetsListURL    = "/api/v1/perms/users/%s/nodes/%s/assets/"
	UserPermsNodeTreeWithAssetURL = "/api/v1/perms/users/%s/nodes/children-with-assets/tree/" // 资产树
)

// 各资源详情相关API
const (
	UserListURL      = "/api/v1/users/users/"
	UserDetailURL    = "/api/v1/users/users/%s/"
	AssetPlatFormURL = "/api/v1/assets/assets/%s/platform/"

	DomainDetailWithGateways = "/api/v1/assets/domains/%s/?gateway=1"

	UserSuggestionsURL = "/api/v1/users/users/suggestions/"
)

const (
	NotificationCommandURL = "/api/v1/terminal/commands/insecure-command/"
)

// 命令复核

const (
	ShareCreateURL        = "/api/v1/terminal/session-sharings/"
	ShareSessionJoinURL   = "/api/v1/terminal/session-join-records/"
	ShareSessionFinishURL = "/api/v1/terminal/session-join-records/%s/finished/"
)

const (
	PublicSettingURL = "/api/v1/settings/public/"
)

const (
	TicketSessionURL = "/api/v1/tickets/ticket-session-relation/"
)

const (
	SuperConnectTokenSecretURL = "/api/v1/authentication/super-connection-token/secret/"
	SuperConnectTokenInfoURL   = "/api/v1/authentication/super-connection-token/"

	UserPermsAssetAccountsURL = "/api/v1/perms/users/%s/assets/%s/"
	AccountSecretURL          = "/api/v1/assets/account-secrets/%s/"
	UserPermsAssetsURL        = "/api/v1/perms/users/%s/assets/"

	AssetLoginConfirmURL = "/api/v1/acls/login-asset/check/"
	AclCommandReviewURL  = "/api/v1/acls/command-filter-acls/command-review/"
)

const (
	UserKoKoPreferenceURL = "/api/v1/users/preference/?category=koko"
)
