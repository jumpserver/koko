package auth

import "github.com/gliderlabs/ssh"

type Service struct {
}

var (
	service = new(Service)
)

func NewService() *Service {
	return service
}

func (s *Service) SSHPassword(ctx ssh.Context, password string) bool {
	ctx.SessionID()
	Username := "softwareuser1"
	Password := "123456"

	if ctx.User() == Username && password == Password {
		return true
	}
	return false
}
