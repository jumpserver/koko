package srvconn

import "github.com/jumpserver-dev/sdk-go/model"

type PreparedDirectSFTP struct {
	Token              *model.ConnectToken
	Client             *SSHClient
	DisableIdleRecycle bool
}

func (p *PreparedDirectSFTP) IsValid() bool {
	return p != nil && p.Token != nil && p.Client != nil
}

func WithPreparedDirectSFTP(prepared *PreparedDirectSFTP) UserSftpOption {
	return func(o *userSftpOption) {
		o.preparedDirectSFTP = prepared
	}
}

func WithPreparedDirectSFTPAsset(prepared *PreparedDirectSFTP) FolderBuilderOption {
	return func(info *folderOptions) {
		info.preparedDirectSFTP = prepared
	}
}
