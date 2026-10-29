package srvconn

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/pkg/sftp"
)

// InterruptTransports unblocks outstanding SFTP requests without waiting for
// asset session locks. Close still owns normal session/audit cleanup.
func (u *UserSftpConn) InterruptTransports() {
	if u.assetDir == nil {
		return
	}
	u.assetDir.sftpSessions.Range(func(_, value any) bool {
		if sess, ok := value.(*SftpSession); ok {
			_ = sess.sshClient.Close()
		}
		return true
	})
}

// RemoveDirectoryExact preserves delete authorization and FTP audit while
// refusing nonempty directories. Agent removal never implies recursive delete.
func (u *UserSftpConn) RemoveDirectoryExact(name string) error {
	if u.assetDir == nil {
		return sftp.ErrSshFxPermissionDenied
	}
	ad := u.assetDir
	if _, err := ad.ResolveAgentToolPath(name); err != nil {
		return err
	}
	account, unique := ad.IsUniqueSu()
	if !unique {
		return errNoAccountUser
	}
	su := ad.suMaps[account]
	if !su.Actions.EnableDelete() {
		return sftp.ErrSshFxPermissionDenied
	}
	conn, realPath := ad.GetSFTPAndRealPath(su, name)
	if conn == nil || conn.isClosed {
		return sftp.ErrSshFxConnectionLost
	}
	if conn.IsRootPath(realPath) {
		return sftp.ErrSshFxPermissionDenied
	}
	conn.IncreaseRef()
	defer conn.DecreaseRef()
	err := conn.client.RemoveDirectory(realPath)
	ad.CreateFTPLog(su, model.OperateRemoveDir, realPath, err == nil)
	return err
}

// ReadDirBounded uses a separate channel with a wire-byte budget, so the SFTP
// library cannot accumulate an unbounded directory before applying the limit.
func (u *UserSftpConn) ReadDirBounded(ctx context.Context, name string, limit int) ([]os.FileInfo, bool, error) {
	if u.assetDir == nil || limit < 1 || limit > 1000 {
		return nil, false, sftp.ErrSshFxPermissionDenied
	}
	ad := u.assetDir
	resolved, err := ad.ResolveAgentToolPath(name)
	if err != nil {
		return nil, false, err
	}
	account, unique := ad.IsUniqueSu()
	if !unique {
		return nil, false, errNoAccountUser
	}
	conn, realPath := ad.GetSFTPAndRealPath(ad.suMaps[account], resolved)
	if conn == nil || conn.isClosed {
		return nil, false, sftp.ErrSshFxConnectionLost
	}
	conn.IncreaseRef()
	defer conn.DecreaseRef()
	stop := context.AfterFunc(ctx, func() { _ = conn.sshClient.Close() })
	defer stop()
	sess, err := conn.sshClient.AcquireSession()
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = sess.Close(); conn.sshClient.ReleaseSession(sess) }()
	if err = sess.RequestSubsystem("sftp"); err != nil {
		return nil, false, err
	}
	writer, err := sess.StdinPipe()
	if err != nil {
		return nil, false, err
	}
	reader, err := sess.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	limited := &io.LimitedReader{R: reader, N: 512 << 10}
	client, err := sftp.NewClientPipe(limited, writer, sftpClientOptions()...)
	if err != nil {
		return nil, false, err
	}
	defer client.Close()
	values, err := client.ReadDirContext(ctx, realPath)
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	truncated := limited.N == 0
	if err != nil && !truncated {
		return nil, false, err
	}
	result := make([]os.FileInfo, 0, min(len(values), limit))
	for _, value := range values {
		if !ad.ShowHidden && strings.HasPrefix(value.Name(), ".") {
			continue
		}
		if len(result) == limit {
			truncated = true
			break
		}
		result = append(result, NewSftpFileInfo(value, conn.token.Account.Username == "root", ad.isFromWebTerminal))
	}
	return result, truncated, nil
}
