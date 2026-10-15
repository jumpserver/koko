package httpd

import (
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
)

// Connection setup belongs to the WebSocket adapter; file operations only
// receive an authenticated filesystem and its recorder.
func newWebSFTPVolume(ws *UserWebsocket) (*sftpVolume, error) {
	cfg, err := ws.apiClient.GetTerminalConfig()
	if err != nil {
		return nil, err
	}
	user := ws.CurrentUser()
	opts := []srvconn.UserSftpOption{
		srvconn.WithUser(user), srvconn.WithRemoteAddr(ws.ClientIP()),
		srvconn.WithLoginFrom(model.LoginFromWeb), srvconn.WithTerminalCfg(&cfg),
	}
	if ws.ConnectToken != nil {
		opts = append(opts, srvconn.WithConnectToken(ws.ConnectToken))
	} else {
		assetID := ws.wsParams.AssetId
		if assetID == "" {
			assetID = ws.wsParams.TargetId
		}
		if common.ValidUUIDString(assetID) {
			asset, err := ws.apiClient.GetUserPermAssetDetailById(user.ID, assetID)
			if err != nil {
				return nil, ErrAssetIdInvalid
			}
			opts = append(opts, srvconn.WithAssets([]model.PermAsset{{
				ID: asset.ID, Name: asset.Name, Address: asset.Address, Comment: asset.Comment,
				Platform: asset.Platform, OrgID: asset.OrgID, OrgName: asset.OrgName,
				IsActive: asset.IsActive, Type: asset.Type, Category: asset.Category,
			}}))
		}
	}
	return newSFTPVolume(srvconn.NewUserSftpConn(ws.apiClient, opts...), proxy.GetFTPFileRecorder(ws.apiClient)), nil
}
