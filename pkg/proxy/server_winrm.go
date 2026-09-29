package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func (s *Server) getWinRMConn(tunnel *net.TCPAddr) (*srvconn.WinRMConnection, error) {
	if s.suFromAccount != nil {
		return nil, errors.New("WinRM does not support account switching")
	}
	asset := s.connOpts.authInfo.Asset
	config := srvconn.WinRMConfig{Host: asset.Address, ServerName: asset.Address,
		Port: asset.ProtocolPort(srvconn.ProtocolWinRM), Username: s.account.Username,
		Password: s.account.Secret, CACert: asset.SecretInfo.CaCert}
	if tunnel != nil {
		config.Host, config.Port = "127.0.0.1", tunnel.Port
	}
	if setting, ok := s.connOpts.authInfo.Platform.GetProtocolSetting(srvconn.ProtocolWinRM); ok {
		config.UseSSL = parseBoolValue(setting.Setting["use_ssl"])
		config.AllowInvalidCert = parseBoolValue(setting.Setting["allow_invalid_cert"])
	}
	conn, err := srvconn.NewWinRMConnection(config)
	if err == nil {
		window := s.UserConn.Pty().Window
		_ = conn.SetWinSize(window.Width, window.Height)
		conn.Client.ExecutionGuard = s.CheckAgentToolExecution
		if s.OnWinRMConnection != nil {
			s.OnWinRMConnection(conn)
		}
	}
	return conn, err
}

func (s *Server) executeWinRMCommand(conn *srvconn.WinRMConnection, ctx context.Context, command, user string, output io.Writer) (err error) {
	item := &ExecutedCommand{Command: command, CreatedDate: time.Now(), User: CurrentActiveUser{User: user}}
	if user == "" {
		item.User.User = s.connOpts.authInfo.User.String()
	}
	decision := s.MatchCommandACL(command)
	item.CmdFilterACLId, item.CmdGroupId = decision.ACLID, decision.ItemID
	writer := &winRMAuditWriter{output: output}
	defer func() {
		if err != nil {
			_, _ = fmt.Fprintln(&writer.buffer, err)
		}
		item.Output = writer.buffer.String()
		if len(item.Output) > maxBufSize {
			item.Output = item.Output[:maxBufSize]
		}
		s.recordBackgroundCommand(s.GenerateCommandItem(item.User.User, command, item.Output, item))
	}()
	if err = s.CheckAgentToolExecution(); err != nil {
		return err
	}
	if decision.Reviewed {
		item.RiskLevel = model.ReviewAccept
	} else if decision.Action == model.ActionWarning || decision.Action == model.ActionNotifyAndWarn {
		item.RiskLevel = model.WarningLevel
	}
	if !decision.Reviewed {
		switch decision.Action {
		case model.ActionReject:
			item.RiskLevel = model.RejectLevel
			return fmt.Errorf("command rejected by ACL %q", decision.Name)
		case model.ActionReview:
			item.RiskLevel = model.ReviewCancel
			if !conn.Confirm(ctx, s.connOpts.getLang().T("The command requires review. Continue?")) {
				return errors.New("command review cancelled")
			}
			decision, err = s.ReviewCommand(ctx, decision, command, func(current CommandACLDecision) {
				if current.DetailURL != "" {
					_, _ = fmt.Fprintln(output, current.DetailURL)
				}
			})
			if err != nil {
				return err
			}
			if decision.Action != model.ActionAccept {
				item.RiskLevel = model.ReviewReject
				return errors.New("command review rejected")
			}
			item.RiskLevel = model.ReviewAccept
		case model.ActionNotifyAndWarn:
			item.RiskLevel = model.WarningLevel
			if !conn.Confirm(ctx, s.connOpts.getLang().T("The command is risky. Continue?")) {
				return errors.New("command cancelled")
			}
		case model.ActionWarning:
			item.RiskLevel = model.WarningLevel
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.CheckAgentToolExecution(); err != nil {
		return err
	}
	return conn.Client.Execute(ctx, command, writer)
}

type winRMAuditWriter struct {
	output io.Writer
	buffer bytes.Buffer
}

func (w *winRMAuditWriter) Write(data []byte) (int, error) {
	remaining := maxBufSize - w.buffer.Len()
	if remaining > 0 {
		_, _ = w.buffer.Write(data[:min(remaining, len(data))])
	}
	return w.output.Write(data)
}
