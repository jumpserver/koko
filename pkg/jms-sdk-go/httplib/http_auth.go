package httplib

import (
	"fmt"
	"net/http"

	"github.com/LeeEirc/httpsig"
)

var (
	_ AuthSign = (*SigAuth)(nil)

	_ AuthSign = (*BasicAuth)(nil)

	_ AuthSign = (*BearerTokenAuth)(nil)
)

const (
	signHeaderRequestTarget = "(request-target)"
	signHeaderDate          = "date"
	signAlgorithm           = "hmac-sha256"
)

type SigAuth struct {
	KeyID    string
	SecretID string
}

func (auth *SigAuth) Sign(r *http.Request) error {
	headers := []string{signHeaderRequestTarget, signHeaderDate}
	signer, err := httpsig.NewRequestSigner(auth.KeyID, auth.SecretID, signAlgorithm)
	if err != nil {
		return err
	}
	return signer.SignRequest(r, headers, nil)
}

type BasicAuth struct {
	Username string
	Password string
}

func (auth *BasicAuth) Sign(r *http.Request) error {
	r.SetBasicAuth(auth.Username, auth.Password)
	return nil
}

type BearerTokenAuth struct {
	Token string
}

func (auth *BearerTokenAuth) Sign(r *http.Request) error {
	r.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth.Token))
	return nil
}
