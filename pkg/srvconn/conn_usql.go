package srvconn

import (
	"net"
	"net/url"
	"strconv"

	"github.com/jumpserver/koko/pkg/localcommand"
)

var (
	_ ServerConnection = (*USQLConn)(nil)
)

func NewUSQLConnection(opts ...SqlOption) (*USQLConn, error) {
	var (
		lCmd *localcommand.LocalCommand
		err  error
	)

	var args = &sqlOption{}

	for _, setter := range opts {
		setter(args)
	}

	lCmd, err = startUSQLCommand(args)
	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &USQLConn{options: args, LocalCommand: lCmd}, nil
}

type USQLConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *USQLConn) KeepAlive() error { return nil }

func (conn *USQLConn) Close() error {
	_, _ = conn.Write(cleanLineExitCommand)
	return conn.LocalCommand.Close()
}

func startUSQLCommand(opt *sqlOption) (*localcommand.LocalCommand, error) {
	args := opt.USQLCommandArgs()
	lcmd, err := localcommand.New("usql", args, localcommand.WithEnv([]string{
		"PAGE=",
	}))
	if err != nil {
		return nil, err
	}
	return lcmd, nil
}

func (o *sqlOption) USQLCommandArgs() []string {
	var dsnURL url.URL
	dsnURL.Scheme = o.Schema
	dsnURL.Host = net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
	dsnURL.User = url.UserPassword(o.Username, o.Password)
	dsnURL.Path = o.DBName
	dsn := dsnURL.String()
	prompt1 := "--variable=PROMPT1=" + o.AssetName + "%R%#"
	return []string{dsn, prompt1}
}
