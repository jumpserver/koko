package auth

import "fmt"

type accessAuth struct {
	accessKey    string
	accessSecret string
}

func (a accessAuth) Signature(date string) string {
	return fmt.Sprintf("Sign %s:%s", a.accessKey, MakeSignature(a.accessSecret, date))
}
