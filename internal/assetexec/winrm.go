package assetexec

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type winRMConnection interface {
	ExecuteCommand(context.Context, string, io.Writer, io.Writer) (srvconn.WinRMResult, error)
	Close() error
}

func (c *Connection) openWinRM(ctx context.Context, token *model.ConnectToken) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.ConnectTimeout)
	defer cancel()
	asset := token.Asset
	config := srvconn.WinRMConfig{Host: asset.Address, ServerName: asset.Address,
		Port: asset.ProtocolPort(srvconn.ProtocolWinRM), Username: token.Account.Username,
		Password: token.Account.Secret, CACert: asset.SecretInfo.CaCert}
	if setting, ok := token.Platform.GetProtocolSetting(srvconn.ProtocolWinRM); ok {
		config.UseSSL, _ = strconv.ParseBool(stringSetting(setting.Setting["use_ssl"]))
		config.AllowInvalidCert, _ = strconv.ParseBool(stringSetting(setting.Setting["allow_invalid_cert"]))
	}
	if token.Gateway != nil {
		tunnel, err := openGatewayTunnel(ctx, token.Gateway, net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), c.opts.ConnectTimeout)
		if err != nil {
			return err
		}
		config.Host, config.Port = "127.0.0.1", tunnel.listener.Addr().(*net.TCPAddr).Port
		c.cleanup = func() error { tunnel.Close(); return nil }
	}
	client, err := srvconn.NewWinRMClientContext(ctx, config)
	if err != nil {
		return err
	}
	c.winRM = client
	return nil
}

// Platform settings come from the authorized token, never from tool arguments.
func stringSetting(value any) string {
	switch value := value.(type) {
	case bool:
		return strconv.FormatBool(value)
	case string:
		return value
	default:
		return ""
	}
}

func (c *Connection) executeWinRM(ctx context.Context, request Request, emit Emit) (Result, error) {
	if request.Cwd != "" || len(request.Env) != 0 {
		return Result{}, errors.New("cwd and env apply only to SSH")
	}
	if _, err := PrepareCommand(request); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer func() { stop(); cancel() }()
	sink := &outputSink{emit: emit, remaining: c.opts.MaxOutputBytes, cancel: cancel}
	evidence, err := c.winRM.ExecuteCommand(ctx, request.Command, outputWriter{sink, "stdout"}, outputWriter{sink, "stderr"})
	if err == nil && !evidence.Completed {
		err = ErrContextLost
	}
	if ctx.Err() != nil || !evidence.Completed {
		_ = c.Close()
	}
	return Result{Dispatched: evidence.Dispatched, Completed: evidence.Completed,
		ExitCode:  evidence.ExitCode,
		Truncated: sink.truncated, ContextLost: c.closed.Load(), StopConfirmed: evidence.PipelineState == "Stopped",
		PipelineState: evidence.PipelineState, HadErrors: evidence.HadErrors}, errors.Join(err, ctx.Err())
}
