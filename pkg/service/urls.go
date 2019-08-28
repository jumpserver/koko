package service

const (
	UserAuthURL    = "/api/authentication/v1/auth/"                      // post 验证用户登陆
	UserProfileURL = "/api/users/v1/profile/"                            // 获取当前用户的基本信息
	UserListURL    = "/api/users/v1/users/"                              // 用户列表地址
	UserDetailURL  = "/api/users/v1/users/%s/"                           // 获取用户信息
	UserAuthOTPURL = "/api/authentication/v1/otp/auth/"                  // 验证OTP
	TokenAssetURL  = "/api/authentication/v1/connection-token/?token=%s" // Token name

	SystemUserAssetAuthURL          = "/api/assets/v1/system-user/%s/asset/%s/auth-info/" // 该系统用户对某资产的授权
	SystemUserCmdFilterRulesListURL = "/api/assets/v1/system-user/%s/cmd-filter-rules/"   // 过滤规则url
	SystemUserDetailURL             = "/api/assets/v1/system-user/%s/"                    // 某个系统用户的信息
	AssetDetailURL                  = "/api/assets/v1/assets/%s/"                         // 某一个资产信息
	DomainDetailURL                 = "/api/assets/v1/domain/%s/?gateway=1"

	TerminalRegisterURL  = "/api/terminal/v2/terminal-registrations/" // 注册当前coco
	TerminalConfigURL    = "/api/terminal/v1/terminal/config/"        // 从jumpserver获取coco的配置
	TerminalHeartBeatURL = "/api/terminal/v1/terminal/status/"

	SessionListURL    = "/api/terminal/v1/sessions/"           //上传创建的资产会话session id
	SessionDetailURL  = "/api/terminal/v1/sessions/%s/"        // finish session的时候发送
	SessionReplayURL  = "/api/terminal/v1/sessions/%s/replay/" //上传录像
	SessionCommandURL = "/api/terminal/v1/command/"            //上传批量命令
	FinishTaskURL     = "/api/terminal/v1/tasks/%s/"

	FTPLogListURL = "/api/audits/v1/ftp-log/" // 上传 ftp日志

	UserAssetsURL                  = "/api/perms/v1/users/%s/assets/"       //获取用户授权的所有资产
	UserNodesAssetsURL             = "/api/perms/v1/users/%s/nodes-assets/" // 获取用户授权的所有节点信息 节点分组
	UserNodesListURL               = "/api/perms/v1/users/%s/nodes/"
	UserNodeAssetsListURL          = "/api/perms/v1/users/%s/nodes/%s/assets/"
	ValidateUserAssetPermissionURL = "/api/perms/v1/asset-permissions/user/validate/" //0不使用缓存 1 使用缓存 2 刷新缓存
)

// 1.5.3

const (
	UserAssetSystemUsersURL = "/api/v1/perms/users/%s/assets/%s/system-users/" // 获取用户授权资产的系统用户列表
)
