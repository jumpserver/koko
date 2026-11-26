package identity

import (
	"context"
	"net/http"
	"strings"
)

// CoreCredentials stays in memory and is never part of a persisted Principal,
// Run, tool argument or model input. Its private fields cannot be JSON encoded.
type CoreCredentials struct {
	cookie        string
	authorization string
	csrfToken     string
}

func (CoreCredentials) String() string   { return "[REDACTED]" }
func (CoreCredentials) GoString() string { return "[REDACTED]" }

// CredentialsFromRequest must only be used after authenticating the request
// and checking its origin and CSRF token.
func CredentialsFromRequest(request *http.Request) CoreCredentials {
	// Match CoreAuthenticator: use exactly the authentication mechanism that
	// was verified, without a cookie fallback for Authorization headers.
	if authorization := strings.TrimSpace(request.Header.Get("Authorization")); authorization != "" {
		return CoreCredentials{authorization: authorization}
	}
	csrfToken := strings.TrimSpace(request.Header.Get("X-CSRFToken"))
	if csrfToken == "" {
		csrfToken = strings.TrimSpace(request.Header.Get("X-CSRF-Token"))
	}
	return CoreCredentials{
		cookie:    strings.Join(request.Header.Values("Cookie"), "; "),
		csrfToken: csrfToken,
	}
}

type coreCredentialsKey struct{}

func WithCoreCredentials(ctx context.Context, credentials CoreCredentials) context.Context {
	return context.WithValue(ctx, coreCredentialsKey{}, credentials)
}

func CoreCredentialsFromContext(ctx context.Context) CoreCredentials {
	credentials, _ := ctx.Value(coreCredentialsKey{}).(CoreCredentials)
	return credentials
}

// Apply authenticates a request to the configured Core endpoint. The caller
// must disable redirects so credentials cannot be sent to another endpoint.
func (c CoreCredentials) Apply(request *http.Request) error {
	if c.cookie == "" && c.authorization == "" {
		return ErrUnauthenticated
	}
	if c.cookie != "" {
		request.Header.Set("Cookie", c.cookie)
	}
	if c.authorization != "" {
		request.Header.Set("Authorization", c.authorization)
	}
	if c.csrfToken != "" {
		request.Header.Set("X-CSRFToken", c.csrfToken)
	}
	if err := VerifyCSRF(request); err != nil {
		return err
	}
	// Kael already checked the browser request's origin. Core sees this
	// server-to-server request at its own origin, including over HTTPS.
	request.Header.Set("Referer", request.URL.Scheme+"://"+request.URL.Host+"/")
	return nil
}
