package agentauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/internal/agentapi"
)

var (
	ErrUnauthenticated = errors.New("agent request is unauthenticated")
	ErrOriginRejected  = errors.New("agent request origin is not allowed")
)

const organizationHeader = "X-JMS-ORG"

type RequestAuthenticator interface {
	Authenticate(ctx context.Context, request *http.Request) (agentapi.Principal, error)
}

type OriginVerifier interface {
	VerifyOrigin(request *http.Request) error
}

type CoreCookieVerifier interface {
	CheckUserCookie(cookies map[string]string, organizationID string) (*model.User, error)
}

type CoreHeaderVerifier interface {
	CheckUserHeaders(headers map[string]string, organizationID string) (*model.User, error)
}

// JMServiceVerifier is the SDK implementation used by the Koko agent API. A per-request
// service clone carries the organization header; cookies and bearer headers
// exist only for the duration of the Core verification request.
type JMServiceVerifier struct {
	Service *service.JMService
}

func (v *JMServiceVerifier) CheckUserCookie(
	cookies map[string]string,
	organizationID string,
) (*model.User, error) {
	if v == nil || v.Service == nil {
		return nil, ErrUnauthenticated
	}
	client := v.Service.Copy()
	client.SetHeader(organizationHeader, organizationID)
	return client.CheckUserCookie(cookies)
}

func (v *JMServiceVerifier) CheckUserHeaders(
	headers map[string]string,
	organizationID string,
) (*model.User, error) {
	if v == nil || v.Service == nil {
		return nil, ErrUnauthenticated
	}
	client := v.Service.Copy()
	client.SetHeader(organizationHeader, organizationID)
	return client.CheckUserHeaders(headers)
}

type CoreAuthenticator struct {
	Cookies CoreCookieVerifier
	Headers CoreHeaderVerifier
}

func (a *CoreAuthenticator) Authenticate(
	_ context.Context,
	request *http.Request,
) (agentapi.Principal, error) {
	organizationID := strings.TrimSpace(request.Header.Get(organizationHeader))
	if organizationID == "" {
		return agentapi.Principal{}, ErrUnauthenticated
	}
	cookies := make(map[string]string)
	for _, cookie := range request.Cookies() {
		if cookie.Name != "" && cookie.Value != "" {
			cookies[cookie.Name] = cookie.Value
		}
	}
	var (
		user *model.User
		err  error
	)
	if len(cookies) > 0 && a != nil && a.Cookies != nil {
		user, err = a.Cookies.CheckUserCookie(cookies, organizationID)
	}
	if (err != nil || user == nil) && a != nil && a.Headers != nil {
		headers := requestAuthHeaders(request)
		if len(headers) > 0 {
			user, err = a.Headers.CheckUserHeaders(headers, organizationID)
		}
	}
	if err != nil || user == nil || user.ID == "" || !user.IsActive ||
		!user.IsValid || user.IsExpired {
		return agentapi.Principal{}, ErrUnauthenticated
	}
	return agentapi.Principal{
		UserID: user.ID, OrganizationID: organizationID,
	}, nil
}

func requestAuthHeaders(request *http.Request) map[string]string {
	result := make(map[string]string)
	for _, name := range []string{"Authorization", "Date", organizationHeader} {
		if value := strings.TrimSpace(request.Header.Get(name)); value != "" {
			result[name] = value
		}
	}
	return result
}

// SameOriginVerifier accepts non-browser requests without Origin and checks
// browser origins against the forwarded/public host or an explicit allowlist.
type SameOriginVerifier struct {
	AllowedOrigins        map[string]struct{}
	TrustForwardedHeaders bool
}

func (v *SameOriginVerifier) VerifyOrigin(request *http.Request) error {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return ErrOriginRejected
	}
	normalized := strings.ToLower(parsed.Scheme + "://" + parsed.Host)
	if _, ok := v.AllowedOrigins[normalized]; ok {
		return nil
	}
	host := request.Host
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if v.TrustForwardedHeaders {
		if forwarded := strings.TrimSpace(request.Header.Get("Forwarded")); forwarded != "" {
			for _, parameter := range strings.Split(strings.Split(forwarded, ",")[0], ";") {
				name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
				if !ok {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), `"`)
				switch strings.ToLower(name) {
				case "host":
					host = value
				case "proto":
					scheme = strings.ToLower(value)
				}
			}
		} else {
			if forwardedHost := firstForwarded(request.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
				host = forwardedHost
			}
			if forwardedProto := strings.ToLower(firstForwarded(request.Header.Get("X-Forwarded-Proto"))); forwardedProto != "" {
				scheme = forwardedProto
			}
		}
	}
	if (scheme != "http" && scheme != "https") || host == "" {
		return ErrOriginRejected
	}
	if strings.EqualFold(normalized, scheme+"://"+host) {
		return nil
	}
	return ErrOriginRejected
}

func firstForwarded(value string) string {
	return strings.TrimSpace(strings.Split(value, ",")[0])
}
