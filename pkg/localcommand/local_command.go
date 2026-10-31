package localcommand

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type LocalCommand struct {
	command string
	argv    []string
	workDir string

	env           []string
	cmdCredential *syscall.Credential

	cmd       *exec.Cmd
	ptyFd     *os.File
	ptyClosed chan struct{}

	ptyWin *pty.Winsize
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
	if lcmd.workDir != "" {
		cmd.Dir = lcmd.workDir
	}
	ptyFd, err := pty.StartWithSize(cmd, lcmd.ptyWin)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	if runtime.GOOS == "linux" {
		// PTY ioctls call File.Fd(), which switches an OpenFile descriptor back
		// to blocking I/O. Wrap an explicitly nonblocking duplicate so later
		// resizing preserves polling and Close can interrupt pending reads/writes.
		fd, err := unix.FcntlInt(ptyFd.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err == nil {
			err = unix.SetNonblock(fd, true)
			if err != nil {
				_ = unix.Close(fd)
			}
		}
		_ = ptyFd.Close()
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("configure nonblocking PTY: %w", err)
		}
		ptyFd = os.NewFile(uintptr(fd), ptyFd.Name())
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
		Rows: uint16(height),
		Cols: uint16(width),
	}
	return pty.Setsize(lcmd.ptyFd, &win)
}
