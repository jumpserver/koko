package service

// KoKo与Core交互的API
const (
	UserProfileURL       = "/api/v1/users/profile/"                   // 获取当前用户的基本信息
	TerminalRegisterURL  = "/api/v2/terminal/terminal-registrations/" // 注册
	TerminalConfigURL    = "/api/v1/terminal/terminals/config/"       // 获取配置
	TerminalHeartBeatURL = "/api/v1/terminal/terminals/status/"
)

// 用户登陆认证使用的API
const (
	TokenAssetURL      = "/api/v1/authentication/connection-token/?token=%s" // Token name
	UserTokenAuthURL   = "/api/v1/authentication/tokens/"                    // 用户登录验证
	UserConfirmAuthURL = "/api/v1/authentication/login-confirm-ticket/status/"
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
)

// 授权相关API
const (
	UserPermsAssetsURL                 = "/api/v1/perms/users/%s/assets/"
	UserPermsNodesListURL              = "/api/v1/perms/users/%s/nodes/"
	UserPermsNodeAssetsListURL         = "/api/v1/perms/users/%s/nodes/%s/assets/"
	UserPermsNodeTreeWithAssetURL      = "/api/v1/perms/users/%s/nodes/children-with-assets/tree/" // 资产树
	UserPermsApplicationsURL           = "/api/v1/perms/users/%s/applications/?type=%s"
	UserPermsAssetSystemUsersURL       = "/api/v1/perms/users/%s/assets/%s/system-users/"
	UserPermsApplicationSystemUsersURL = "/api/v1/perms/users/%s/applications/%s/system-users/"
	ValidateUserAssetPermissionURL     = "/api/v1/perms/asset-permissions/user/validate/"
	ValidateApplicationPermissionURL   = "/api/v1/perms/application-permissions/user/validate/"
)

// 系统用户密码相关API
const (
	SystemUserAuthURL      = "/api/v1/assets/system-users/%s/auth-info/"
	SystemUserAssetAuthURL = "/api/v1/assets/system-users/%s/assets/%s/auth-info/" // 该系统用户对某资产的授权
)

// 各资源详情相关API
const (
	UserDetailURL        = "/api/v1/users/users/%s/"
	AssetDetailURL       = "/api/v1/assets/assets/%s/"
	AssetPlatFormURL     = "/api/v1/assets/assets/%s/platform/"
	AssetGatewaysURL     = "/api/v1/assets/assets/%s/gateways/"
	SystemUserDetailURL  = "/api/v1/assets/system-users/%s/"
	ApplicationDetailURL = "/api/v1/applications/applications/%s/"

	SystemUserCmdFilterRulesListURL = "/api/v1/assets/system-users/%s/cmd-filter-rules/" // 过滤规则url

	DomainDetailWithGateways = "/api/v1/assets/domains/%s/?gateway=1"
)

const (
	NotificationCommandURL = "/api/v1/terminal/commands/insecure-command/"
)

const (
	StatURL = "/api/v1/terminal/components/state/"
)
