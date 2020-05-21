package service

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

type Option func(*option)

type option struct {
	name           string
	baseHost       string
	bootStrapToken string
	timeout        int64
	authKey        AccessKey
}

func HttpClientBootStrapToken(token string) Option {
	return func(o *option) {
		o.bootStrapToken = token
	}
}

func HttpClientTimeout(timeout int64) Option {
	return func(o *option) {
		o.timeout = timeout
	}
}

func HttpClientBaseHost(baseHost string) Option {
	return func(o *option) {
		o.baseHost = baseHost
	}
}

func HttpClientAuthKey(authKey *AccessKey) Option {
	return func(o *option) {
		o.authKey = *authKey
	}
}

func NewHttpClient(Options ...Option) *HttpClient {
	newOption := &option{
		baseHost: "http://127.0.0.1:8080",
	}
	for _, setter := range Options {
		setter(newOption)
	}
	newClient := common.NewClient(time.Duration(newOption.timeout)*time.Second,
		newOption.baseHost)
	newClient.SetHeader("X-JMS-ORG", "ROOT")
	newClient.Auth = newOption.authKey
	//cf := config.GetConf()
	//keyPath := cf.AccessKeyFile
	//newClient.BaseHost = cf.CoreHost
	//newClient.SetHeader("X-JMS-ORG", "ROOT")
	//
	//if !path.IsAbs(cf.AccessKeyFile) {
	//	keyPath = filepath.Join(cf.RootPath, keyPath)
	//}
	//ak := AccessKey{Value: cf.AccessKey, Path: keyPath}
	//_ = ak.Load()

	return &HttpClient{
		opts:       newOption,
		authClient: &newClient,
	}
}

type HttpClient struct {
	opts       *option
	authClient *common.Client
}

func (h *HttpClient) getNoAuthClient() common.Client {
	return common.NewClient(time.Duration(h.opts.timeout)*time.Second, h.opts.baseHost)
}

func (h *HttpClient) GetSystemUserAssetAuthInfo(systemUserID, assetID string) (info model.SystemUserAuthInfo) {
	return h.getUserAssetAuthInfo(systemUserID, assetID, "", "")
}

func (h *HttpClient) GetUserAssetAuthInfo(systemUserID, assetID, userID, username string) (info model.SystemUserAuthInfo) {
	return h.getUserAssetAuthInfo(systemUserID, assetID, userID, username)
}

func (h *HttpClient) getUserAssetAuthInfo(systemUserID, assetID, userID, username string) (info model.SystemUserAuthInfo) {
	Url := fmt.Sprintf(SystemUserAssetAuthURL, systemUserID, assetID)
	params := make(map[string]string)
	if username != "" {
		params["username"] = username
	}
	if userID != "" {
		params["user_id"] = userID
	}
	_, err := authClient.Get(Url, &info, params)
	if err != nil {
		logger.Errorf("Get system user %s asset %s auth info failed：%s", systemUserID, assetID, err)
	}
	return
}

func (h *HttpClient) GetSystemUserFilterRules(systemUserID string) (rules []model.SystemUserFilterRule, err error) {
	/*[
	    {
	        "id": "12ae03a4-81b7-43d9-b356-2db4d5d63927",
	        "org_id": "",
	        "type": {
	            "value": "command",
	            "display": "命令"
	        },
	        "priority": 50,
	        "content": "reboot\r\nrm",
	        "action": {
	            "value": 0,
	            "display": "拒绝"
	        },
	        "comment": "",
	        "date_created": "2019-04-29 11:32:12 +0800",
	        "date_updated": "2019-04-29 11:32:12 +0800",
	        "created_by": "Administrator",
	        "filter": "de7693ca-75d5-4639-986b-44ed390260a0"
	    },
	    {
	        "id": "c1fe1ebf-8fdc-4477-b2cf-dd9bc12de832",
	        "org_id": "",
	        "type": {
	            "value": "regex",
	            "display": "正则表达式"
	        },
	        "priority": 49,
	        "content": "shutdown|echo|df",
	        "action": {
	            "value": 1,
	            "display": "允许"
	        },
	        "comment": "",
	        "date_created": "2019-04-29 11:32:39 +0800",
	        "date_updated": "2019-04-29 11:32:50 +0800",
	        "created_by": "Administrator",
	        "filter": "de7693ca-75d5-4639-986b-44ed390260a0"
	    }
	]`*/
	Url := fmt.Sprintf(SystemUserCmdFilterRulesListURL, systemUserID)

	_, err = authClient.Get(Url, &rules)
	if err != nil {
		logger.Errorf("Get system user %s filter rule failed", systemUserID)
	}
	return
}

func (h *HttpClient) GetSystemUser(systemUserID string) (info model.SystemUser) {
	Url := fmt.Sprintf(SystemUserDetailURL, systemUserID)
	_, err := authClient.Get(Url, &info)
	if err != nil {
		logger.Errorf("Get system user %s failed", systemUserID)
	}
	return
}

func (h *HttpClient) GetAsset(assetID string) (asset model.Asset) {
	Url := fmt.Sprintf(AssetDetailURL, assetID)
	_, err := authClient.Get(Url, &asset)
	if err != nil {
		logger.Errorf("Get Asset %s failed: %s", assetID, err)
	}
	return
}

func (h *HttpClient) GetDomainWithGateway(gID string) (domain model.Domain) {
	url := fmt.Sprintf(DomainDetailURL, gID)
	_, err := authClient.Get(url, &domain)
	if err != nil {
		logger.Errorf("Get domain %s failed: %s", gID, err)
	}
	return
}

func (h *HttpClient) GetTokenAsset(token string) (tokenUser model.TokenUser) {
	Url := fmt.Sprintf(TokenAssetURL, token)
	_, err := authClient.Get(Url, &tokenUser)
	if err != nil {
		logger.Error("Get Token Asset info failed: ", err)
	}
	return
}

func (h *HttpClient) GetAssetGateways(assetID string) (gateways []model.Gateway) {
	Url := fmt.Sprintf(AssetGatewaysURL, assetID)
	_, err := authClient.Get(Url, &gateways)
	if err != nil {
		logger.Errorf("Get Asset %s gateways failed: %s", assetID, err)
	}
	return
}

func (h *HttpClient) GetUserDatabases(uid string) (res []model.Database) {
	Url := fmt.Sprintf(DatabaseAPPURL, uid)
	_, err := authClient.Get(Url, &res)
	if err != nil {
		logger.Errorf("Get User databases err: %s", err)
	}
	return
}

func (h *HttpClient) GetUserDatabaseSystemUsers(userID, assetID string) (sysUsers []model.SystemUser) {
	Url := fmt.Sprintf(UserDatabaseSystemUsersURL, userID, assetID)
	_, err := authClient.Get(Url, &sysUsers)
	if err != nil {
		logger.Error("Get user asset system users error: ", err)
	}
	return
}

func (h *HttpClient) GetSystemUserDatabaseAuthInfo(systemUserID string) (info model.SystemUserAuthInfo) {
	Url := fmt.Sprintf(SystemUserAuthURL, systemUserID)
	_, err := authClient.Get(Url, &info)
	if err != nil {
		logger.Errorf("Get system user %s auth info failed", systemUserID)
	}
	return
}

func (h *HttpClient) GetDatabase(dbID string) (res model.Database) {
	Url := fmt.Sprintf(DatabaseDetailURL, dbID)
	_, err := authClient.Get(Url, &res)
	if err != nil {
		logger.Errorf("Get User databases err: %s", err)
	}
	return
}

func (h *HttpClient) GetUserAssets(userID string, pageSize, offset int, searches ...string) (resp model.AssetsPaginationResponse) {
	if pageSize < 0 {
		pageSize = 0
	}
	paramsArray := make([]map[string]string, 0, len(searches)+2)
	for i := 0; i < len(searches); i++ {
		paramsArray = append(paramsArray, map[string]string{
			"search": searches[i],
		})
	}
	params := map[string]string{
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(offset),
	}
	paramsArray = append(paramsArray, params)
	Url := fmt.Sprintf(UserAssetsURL, userID)
	var err error
	if pageSize > 0 {
		_, err = authClient.Get(Url, &resp, paramsArray...)
	} else {
		var data model.AssetList
		_, err = authClient.Get(Url, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	if err != nil {
		logger.Error("Get user assets error: ", err)
	}
	return
}

func (h *HttpClient) ForceRefreshUserPemAssets(userID string) error {
	params := map[string]string{
		"limit":  "1",
		"offset": "0",
		"cache":  "2",
	}
	Url := fmt.Sprintf(UserAssetsURL, userID)
	var resp model.AssetsPaginationResponse
	_, err := authClient.Get(Url, &resp, params)
	if err != nil {
		logger.Errorf("Refresh user assets error: %s", err)
	}
	return err
}

func (h *HttpClient) GetUserAllAssets(userID string) (assets []model.Asset) {
	Url := fmt.Sprintf(UserAssetsURL, userID)
	_, err := authClient.Get(Url, &assets)
	if err != nil {
		logger.Error("Get user all assets error: ", err)
	}
	return
}

func (h *HttpClient) GetUserAssetByID(userID, assertID string) (assets []model.Asset) {
	params := map[string]string{
		"id": assertID,
	}
	Url := fmt.Sprintf(UserAssetsURL, userID)
	_, err := authClient.Get(Url, &assets, params)
	if err != nil {
		logger.Error("Get user asset by ID error: ", err)
	}
	return
}

func (h *HttpClient) GetUserNodes(userID, cachePolicy string) (nodes model.NodeList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	Url := fmt.Sprintf(UserNodesListURL, userID)
	_, err := authClient.Get(Url, &nodes, payload)
	if err != nil {
		logger.Error("Get user nodes error: ", err)
	}
	return
}

func (h *HttpClient) GetUserAssetSystemUsers(userID, assetID string) (sysUsers []model.SystemUser) {
	Url := fmt.Sprintf(UserAssetSystemUsersURL, userID, assetID)
	_, err := authClient.Get(Url, &sysUsers)
	if err != nil {
		logger.Error("Get user asset system users error: ", err)
	}
	return
}

func (h *HttpClient) GetUserNodeAssets(userID, nodeID, cachePolicy string) (assets model.AssetList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	payload := map[string]string{"cache_policy": cachePolicy, "all": "1"}
	Url := fmt.Sprintf(UserNodeAssetsListURL, userID, nodeID)
	_, err := authClient.Get(Url, &assets, payload)
	if err != nil {
		logger.Error("Get user node assets error: ", err)
		return
	}
	return
}

func (h *HttpClient) GetUserNodePaginationAssets(userID, nodeID string, pageSize, offset int, searches ...string) (resp model.AssetsPaginationResponse) {
	if pageSize < 0 {
		pageSize = 0
	}
	paramsArray := make([]map[string]string, 0, len(searches)+2)
	for i := 0; i < len(searches); i++ {
		paramsArray = append(paramsArray, map[string]string{
			"search": url.QueryEscape(searches[i]),
		})
	}

	params := map[string]string{
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(offset),
	}
	paramsArray = append(paramsArray, params)
	Url := fmt.Sprintf(UserNodeAssetsListURL, userID, nodeID)
	var err error
	if pageSize > 0 {
		_, err = authClient.Get(Url, &resp, paramsArray...)
	} else {
		var data model.AssetList
		_, err = authClient.Get(Url, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	if err != nil {
		logger.Error("Get user node assets error: ", err)
	}
	return
}

func (h *HttpClient) ValidateUserAssetPermission(userID, assetID, systemUserID, action string) bool {
	payload := map[string]string{
		"user_id":        userID,
		"asset_id":       assetID,
		"system_user_id": systemUserID,
		"action_name":    action,
		"cache_policy":   "1",
	}
	Url := ValidateUserAssetPermissionURL
	var res struct {
		Msg bool `json:"msg"`
	}
	_, err := authClient.Get(Url, &res, payload)

	if err != nil {
		logger.Error(err)
		return false
	}

	return res.Msg
}

func (h *HttpClient) ValidateUserDatabasePermission(userID, databaseID, systemUserID string) bool {
	payload := map[string]string{
		"user_id":         userID,
		"database_app_id": databaseID,
		"system_user_id":  systemUserID,
	}
	Url := ValidateUserDatabasePermissionURL
	var res struct {
		Msg bool `json:"msg"`
	}
	_, err := authClient.Get(Url, &res, payload)

	if err != nil {
		logger.Error(err)
		return false
	}

	return res.Msg
}

func (h *HttpClient) GetUserNodeTreeWithAsset(userID, nodeID, cachePolicy string) (nodeTrees model.NodeTreeList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}

	payload := map[string]string{"cache_policy": cachePolicy}
	if nodeID != "" {
		payload["id"] = nodeID
	}
	Url := fmt.Sprintf(NodeTreeWithAssetURL, userID)
	_, err := authClient.Get(Url, &nodeTrees, payload)
	if err != nil {
		logger.Error("Get user node tree error: ", err)
	}
	return
}

func (h *HttpClient) SearchPermAsset(uid, key string) (res model.NodeTreeList, err error) {
	Url := fmt.Sprintf(UserAssetsTreeURL, uid)
	payload := map[string]string{"search": key}
	_, err = authClient.Get(Url, &res, payload)
	if err != nil {
		logger.Error("Get user node tree error: ", err)
	}
	return
}

func (h *HttpClient) RegisterTerminal(name, token, comment string) (res model.Terminal) {
	client := newClient()
	client.Headers["Authorization"] = fmt.Sprintf("BootstrapToken %s", token)
	data := map[string]string{"name": name, "comment": comment}
	_, err := client.Post(TerminalRegisterURL, data, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func (h *HttpClient) TerminalHeartBeat(sIds []string) (res []model.TerminalTask) {

	data := map[string][]string{
		"sessions": sIds,
	}
	_, err := authClient.Post(TerminalHeartBeatURL, data, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func (h *HttpClient) CreateSession(data map[string]interface{}) bool {
	var res map[string]interface{}
	_, err := authClient.Post(SessionListURL, data, &res)
	if err == nil {
		return true
	}
	logger.Error(err)
	return false
}

func (h *HttpClient) FinishSession(data map[string]interface{}) {

	var res map[string]interface{}
	if sid, ok := data["id"]; ok {
		payload := map[string]interface{}{
			"is_finished": true,
			"date_end":    data["date_end"],
		}
		Url := fmt.Sprintf(SessionDetailURL, sid)
		_, err := authClient.Patch(Url, payload, &res)
		if err != nil {
			logger.Error(err)
		}
	}

}

func (h *HttpClient) FinishReply(sid string) bool {
	var res map[string]interface{}
	data := map[string]bool{"has_replay": true}
	Url := fmt.Sprintf(SessionDetailURL, sid)
	_, err := authClient.Patch(Url, data, &res)
	if err != nil {
		logger.Error(err)
		return false
	}
	return true
}

func (h *HttpClient) FinishTask(tid string) bool {
	var res map[string]interface{}
	data := map[string]bool{"is_finished": true}
	Url := fmt.Sprintf(FinishTaskURL, tid)
	_, err := authClient.Patch(Url, data, &res)
	if err != nil {
		logger.Error(err)
		return false
	}
	return true
}

func (h *HttpClient) PushSessionReplay(sessionID, gZipFile string) (err error) {
	var res map[string]interface{}
	Url := fmt.Sprintf(SessionReplayURL, sessionID)
	err = authClient.UploadFile(Url, gZipFile, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func (h *HttpClient) PushSessionCommand(commands []*model.Command) (err error) {
	_, err = authClient.Post(SessionCommandURL, commands, nil)
	if err != nil {
		logger.Error(err)
	}
	return
}

func (h *HttpClient) PushFTPLog(data *model.FTPLog) (err error) {
	_, err = authClient.Post(FTPLogListURL, data, nil)
	if err != nil {
		logger.Error(err)
	}
	return
}

func (h *HttpClient) JoinRoomValidate(userID, sessionID string) bool {
	data := map[string]string{
		"session_id": sessionID,
		"user_id":    userID,
	}
	var result struct {
		Ok  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	_, err := authClient.Post(JoinRoomValidateURL, data, &result)
	if err != nil {
		logger.Errorf("Validate join room err: %s", err)
		return false
	}
	if !result.Ok && result.Msg != "" {
		logger.Errorf("Validate result err msg: %s", result.Msg)
	}

	return result.Ok
}
