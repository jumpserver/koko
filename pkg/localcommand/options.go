package localcommand

import "syscall"

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
