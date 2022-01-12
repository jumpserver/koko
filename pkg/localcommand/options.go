package localcommand

import (
	"syscall"

	"github.com/creack/pty"
)

type Option func(*LocalCommand)

func WithEnv(env []string) Option {
	return func(lcmd *LocalCommand) {
		lcmd.env = env
	}
}

func WithCmdCredential(credential *syscall.Credential) Option {
	return func(lcmd *LocalCommand) {
		lcmd.cmdCredential = credential
	}
}

func WithPtyWin(width, height int) Option {
	return func(lcmd *LocalCommand) {
		win := pty.Winsize{
			Rows: uint16(height),
			Cols: uint16(width),
		}
		lcmd.ptyWin = &win
	}
}
