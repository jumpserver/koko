package service

const (
	UserProfileURL = "/api/v1/users/profile/"                            // 获取当前用户的基本信息
	UserListURL    = "/api/v1/users/users/"                              // 用户列表地址
	UserDetailURL  = "/api/v1/users/users/%s/"                           // 获取用户信息
	TokenAssetURL  = "/api/v1/authentication/connection-token/?token=%s" // Token name

	SystemUserAssetAuthURL          = "/api/v1/assets/system-users/%s/assets/%s/auth-info/" // 该系统用户对某资产的授权
	SystemUserCmdFilterRulesListURL = "/api/v1/assets/system-users/%s/cmd-filter-rules/"    // 过滤规则url
	SystemUserDetailURL             = "/api/v1/assets/system-users/%s/"                     // 某个系统用户的信息
	AssetDetailURL                  = "/api/v1/assets/assets/%s/"                           // 某一个资产信息
	DomainDetailURL                 = "/api/v1/assets/domains/%s/?gateway=1"

	TerminalRegisterURL  = "/api/v2/terminal/terminal-registrations/" // 注册当前coco
	TerminalConfigURL    = "/api/v1/terminal/terminals/config/"       // 从jumpserver获取coco的配置
	TerminalHeartBeatURL = "/api/v1/terminal/terminals/status/"

	SessionListURL    = "/api/v1/terminal/sessions/"           //上传创建的资产会话session id
	SessionDetailURL  = "/api/v1/terminal/sessions/%s/"        // finish session的时候发送
	SessionReplayURL  = "/api/v1/terminal/sessions/%s/replay/" //上传录像
	SessionCommandURL = "/api/v1/terminal/commands/"           //上传批量命令
	FinishTaskURL     = "/api/v1/terminal/tasks/%s/"

	FTPLogListURL = "/api/v1/audits/ftp-logs/" // 上传 ftp日志

	UserAssetsURL                  = "/api/v1/perms/users/%s/assets/"       //获取用户授权的所有资产
	UserNodesAssetsURL             = "/api/v1/perms/users/%s/nodes-assets/" // 获取用户授权的所有节点信息 节点分组
	UserNodesListURL               = "/api/v1/perms/users/%s/nodes/"
	UserNodeAssetsListURL          = "/api/v1/perms/users/%s/nodes/%s/assets/"
	ValidateUserAssetPermissionURL = "/api/v1/perms/asset-permissions/user/validate/" //0不使用缓存 1 使用缓存 2 刷新缓存
)

// 1.5.3

const (
	UserAssetSystemUsersURL = "/api/v1/perms/users/%s/assets/%s/system-users/" // 获取用户授权资产的系统用户列表
)

// 1.5.5
const (
	UserTokenAuthURL   = "/api/v1/authentication/tokens/" // 用户登录验证
	UserConfirmAuthURL = "/api/v1/authentication/login-confirm-ticket/status/"

	NodeTreeWithAssetURL = "/api/v1/perms/users/%s/nodes/children-with-assets/tree/" // 资产树

	DatabaseAPPURL = "/api/v1/perms/users/%s/database-apps/" //数据库app

	UserDatabaseSystemUsersURL = "/api/v1/perms/users/%s/database-apps/%s/system-users/"

	SystemUserAuthURL = "/api/v1/assets/system-users/%s/auth-info/"

	UserAssetsTreeURL = "/api/v1/perms/users/%s/assets/tree/"

	DatabaseDetailURL = "/api/v1/applications/database-apps/%s/"

	ValidateUserDatabasePermissionURL = "/api/v1/perms/database-app-permissions/user/validate/"
)

// 1.5.7
const (
	AssetGatewaysURL = "/api/v1/assets/assets/%s/gateways/"
)
