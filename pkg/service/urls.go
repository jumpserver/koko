package service

const (
	UserAuthURL    = "/api/users/v1/auth/"     // post 验证用户登陆
	UserProfileURL = "/api/users/v1/profile/"  // 获取当前用户的基本信息
	UserUserURL    = "/api/users/v1/users/%s/" // 获取用户信息
	UserAuthOTPURL = "/api/users/v1/otp/auth/" // 验证OTP

	AuthMFAURL = "/api/authentication/v1/otp/auth/" // MFA 验证用户信息

	SystemUserAssetAuthURL   = "/api/assets/v1/system-user/%s/asset/%s/auth-info/" // 该系统用户对某资产的授权
	SystemUserAuthInfoURL    = "/api/assets/v1/system-user/%s/auth-info/"          // 该系统用户的授权
	SystemUserCmdFilterRules = "/api/assets/v1/system-user/%s/cmd-filter-rules/"   // 过滤规则url
	SystemUser               = "/api/assets/v1/system-user/%s"                     //	某个系统用户的信息
	Asset                    = "/api/assets/v1/assets/%s/"                         // 某一个资产信息
	TokenAsset               = "/api/users/v1/connection-token/?token=%s"          // Token name

	TerminalRegisterURL  = "/api/terminal/v2/terminal-registrations/" // 注册当前coco
	TerminalConfigURL    = "/api/terminal/v1/terminal/config/"        // 从jumpserver获取coco的配置
	TerminalHeartBeatURL = "/api/terminal/v1/terminal/status/"

	SessionListURL   = "/api/terminal/v1/sessions/"           //上传创建的资产会话session id
	SessionDetailURL = "/api/terminal/v1/sessions/%s/"        // finish session的时候发送
	SessionReplayURL = "/api/terminal/v1/sessions/%s/replay/" //上传录像

	FinishTaskURL = "/api/terminal/v1/tasks/%s/"

	UserAssetsURL                  = "/api/perms/v1/user/%s/assets/"                 //获取用户授权的所有资产
	UserNodesAssetsURL             = "/api/perms/v1/user/%s/nodes-assets/"           // 获取用户授权的所有节点信息 节点分组
	ValidateUserAssetPermissionURL = "/api/perms/v1/asset-permission/user/validate/" //0不使用缓存 1 使用缓存 2 刷新缓存
)
