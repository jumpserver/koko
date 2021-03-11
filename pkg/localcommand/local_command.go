package localcommand

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type LocalCommand struct {
	command string
	argv    []string

	env           []string
	cmdCredential *syscall.Credential

	cmd       *exec.Cmd
	ptyFd     *os.File
	ptyClosed chan struct{}
}

func New(command string, argv []string, options ...Option) (*LocalCommand, error) {
	ptyClosed := make(chan struct{})
	lcmd := &LocalCommand{
		command:   command,
		argv:      argv,
		ptyClosed: ptyClosed,
	}

	for _, option := range options {
		option(lcmd)
	}
	cmd := exec.Command(command, argv...)
	if lcmd.env != nil {
		cmd.Env = lcmd.env
	}
	if lcmd.cmdCredential != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = lcmd.cmdCredential
	}
	ptyFd, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	lcmd.cmd = cmd
	lcmd.ptyFd = ptyFd
	// When the process is closed by the user,
	// close pty so that Read() on the pty breaks with an EOF.
	go func() {
		defer func() {
			lcmd.ptyFd.Close()
			close(lcmd.ptyClosed)
		}()
		_ = lcmd.cmd.Wait()
	}()

	return lcmd, nil
}

func (lcmd *LocalCommand) Read(p []byte) (n int, err error) {
	return lcmd.ptyFd.Read(p)
}

func (lcmd *LocalCommand) Write(p []byte) (n int, err error) {
	return lcmd.ptyFd.Write(p)
}

func (lcmd *LocalCommand) Close() error {
	select {
	case <-lcmd.ptyClosed:
		return nil
	default:
		if lcmd.cmd != nil && lcmd.cmd.Process != nil {
			return lcmd.cmd.Process.Signal(syscall.SIGKILL)
		}
	}
	return nil
}

func (lcmd *LocalCommand) SetWinSize(width int, height int) error {
	win := pty.Winsize{
		Rows: uint16(width),
		Cols: uint16(height),
	}
	return pty.Setsize(lcmd.ptyFd, &win)
}
