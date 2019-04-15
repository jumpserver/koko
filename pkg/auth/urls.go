package auth

const (
	TerminalRegisterUrl    = "/api/terminal/v2/terminal-registrations/"          // 注册当前coco
	TerminalConfigUrl      = "/api/terminal/v1/terminal/config/"                 // 从jumpserver获取coco的配置
	UserAuthUrl            = "/api/users/v1/auth/"                               // post 验证用户登陆
	UserProfileUrl         = "/api/users/v1/profile/"                            // 获取当前用户的基本信息
	UserAssetsUrl          = "/api/perms/v1/user/%s/assets/"                     //获取用户授权的所有资产
	UserNodesAssetsUrl     = "/api/perms/v1/user/%s/nodes-assets/"               // 获取用户授权的所有节点信息 节点分组
	SystemUserAssetAuthUrl = "/api/assets/v1/system-user/%s/asset/%s/auth-info/" // 该系统用户对某资产的授权
	SystemUserAuthUrl      = "/api/assets/v1/system-user/%s/auth-info/"          // 该系统用户的授权

	ValidateUserAssetPermission = "/api/perms/v1/asset-permission/user/validate/" //0不使用缓存 1 使用缓存 2 刷新缓存
)

/*
/api/assets/v1/system-user/%s/asset/%s/auth-info/
/api/assets/v1/system-user/fbd39f8c-fa3e-4c2b-948e-ce1e0380b4f9/cmd-filter-rules/
*/
