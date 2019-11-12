package service

type AuthStatus int64

const (
	AuthSuccess AuthStatus = iota + 1
	AuthFailed
	AuthMFARequired
	AuthConfirmRequired
)

type SessionOption func(*SessionOptions)

func Username(username string) SessionOption {
	return func(args *SessionOptions) {
		args.Username = username
	}
}

func Password(password string) SessionOption {
	return func(args *SessionOptions) {
		args.Password = password
	}
}

func PublicKey(publicKey string) SessionOption {
	return func(args *SessionOptions) {
		args.PublicKey = publicKey
	}
}

func RemoteAddr(remoteAddr string) SessionOption {
	return func(args *SessionOptions) {
		args.RemoteAddr = remoteAddr
	}
}

func LoginType(loginType string) SessionOption {
	return func(args *SessionOptions) {
		args.LoginType = loginType
	}
}

type SessionOptions struct {
	Username   string
	Password   string
	PublicKey  string
	RemoteAddr string
	LoginType  string
}
