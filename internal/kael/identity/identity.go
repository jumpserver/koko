package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
)

var (
	ErrUnauthenticated     = errors.New("request is unauthenticated")
	ErrIdentityUnavailable = errors.New("identity provider is unavailable")
	ErrOriginRejected      = errors.New("request origin is not allowed")
	ErrCSRFRejected        = errors.New("csrf token is invalid")
)

type CoreAuthenticator struct {
	BaseURL string
	Client  *http.Client
}

func NewCoreAuthenticator(baseURL string, verifyTLS bool, timeout time.Duration) *CoreAuthenticator {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !verifyTLS} //nolint:gosec -- deployment-controlled compatibility option
	return &CoreAuthenticator{BaseURL: strings.TrimRight(baseURL, "/"), Client: &http.Client{
		Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (a *CoreAuthenticator) Authenticate(ctx context.Context, source *http.Request) (domain.Principal, error) {
	organizationID := strings.TrimSpace(source.Header.Get("X-JMS-ORG"))
	if organizationID == "" {
		return domain.Principal{}, ErrUnauthenticated
	}
	var profile struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Username    string   `json:"username"`
		IsActive    *bool    `json:"is_active"`
		IsValid     *bool    `json:"is_valid"`
		IsExpired   bool     `json:"is_expired"`
		IsSuperuser bool     `json:"is_superuser"`
		IsOrgAdmin  bool     `json:"is_org_admin"`
		Permissions []string `json:"permissions"`
		Perms       []string `json:"perms"`
	}
	var permissions struct {
		ID    string   `json:"id"`
		Perms []string `json:"perms"`
	}
	var profileErr, permissionsErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		profileErr = a.coreJSON(ctx, source, organizationID, "/api/v1/users/profile/", &profile)
	}()
	go func() {
		defer wait.Done()
		permissionsErr = a.coreJSON(ctx, source, organizationID, "/api/v1/users/profile/permissions/", &permissions)
	}()
	wait.Wait()
	if errors.Is(profileErr, ErrUnauthenticated) || errors.Is(permissionsErr, ErrUnauthenticated) {
		return domain.Principal{}, ErrUnauthenticated
	}
	if profileErr != nil || permissionsErr != nil {
		return domain.Principal{}, errors.Join(ErrIdentityUnavailable, profileErr, permissionsErr)
	}
	if profile.ID == "" || profile.IsExpired ||
		profile.IsActive != nil && !*profile.IsActive || profile.IsValid != nil && !*profile.IsValid ||
		permissions.ID != "" && permissions.ID != profile.ID {
		return domain.Principal{}, ErrUnauthenticated
	}
	fingerprintSource := source.Header.Get("Authorization")
	if fingerprintSource == "" {
		for _, cookie := range source.Cookies() {
			fingerprintSource += cookie.Name + "=" + cookie.Value + ";"
		}
	}
	fingerprint := sha256.Sum256([]byte(fingerprintSource))
	resolvedPermissions := append(append(append([]string(nil), profile.Permissions...), profile.Perms...), permissions.Perms...)
	return domain.Principal{SubjectID: profile.ID, Name: profile.Name, Username: profile.Username, OrganizationID: organizationID, AuthSource: "core", Fingerprint: hex.EncodeToString(fingerprint[:]), IsSuperuser: profile.IsSuperuser, IsOrgAdmin: profile.IsOrgAdmin, Permissions: uniqueStrings(resolvedPermissions)}, nil
}

func (a *CoreAuthenticator) coreJSON(ctx context.Context, source *http.Request, organizationID, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+path, nil)
	if err != nil {
		return errors.Join(ErrIdentityUnavailable, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-JMS-ORG", organizationID)
	if authorization := strings.TrimSpace(source.Header.Get("Authorization")); authorization != "" {
		request.Header.Set("Authorization", authorization)
	} else {
		// Header authentication bypasses CSRF checks, so Core must not fall back
		// to session cookies when that header is unrecognized or invalid.
		for _, cookie := range source.Cookies() {
			if cookie.Name != "" && cookie.Value != "" {
				request.AddCookie(cookie)
			}
		}
	}
	response, err := a.Client.Do(request)
	if err != nil {
		return errors.Join(ErrIdentityUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return ErrUnauthenticated
		}
		return ErrIdentityUnavailable
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	if err = decoder.Decode(target); err != nil {
		return errors.Join(ErrIdentityUnavailable, err)
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

type OriginVerifier struct {
	Allowed               map[string]struct{}
	TrustForwardedHeaders bool
	enabled               bool
}

func NewOriginVerifier(allowed []string, trustForwarded bool) *OriginVerifier {
	values := make(map[string]struct{}, len(allowed))
	enabled := false
	for _, value := range allowed {
		if strings.TrimSpace(value) != "" {
			enabled = true
		}
		if normalized := normalizeOrigin(value); normalized != "" {
			values[normalized] = struct{}{}
		}
	}
	return &OriginVerifier{Allowed: values, TrustForwardedHeaders: trustForwarded, enabled: enabled}
}

func (v *OriginVerifier) Verify(request *http.Request) error {
	if !v.enabled {
		return nil
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return nil
	}
	normalized := normalizeOrigin(origin)
	if normalized == "" {
		return ErrOriginRejected
	}
	if _, ok := v.Allowed[normalized]; ok {
		return nil
	}
	host, scheme := request.Host, "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if v.TrustForwardedHeaders {
		if value := firstForwarded(request.Header.Get("X-Forwarded-Host")); value != "" {
			host = value
		}
		if value := strings.ToLower(firstForwarded(request.Header.Get("X-Forwarded-Proto"))); value != "" {
			scheme = value
		}
	}
	if host == "" || scheme != "http" && scheme != "https" || !strings.EqualFold(normalized, scheme+"://"+host) {
		return ErrOriginRejected
	}
	return nil
}

func VerifyCSRF(request *http.Request) error {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return nil
	}
	if strings.TrimSpace(request.Header.Get("Authorization")) != "" {
		return nil
	}
	provided := strings.TrimSpace(request.Header.Get("X-CSRFToken"))
	if provided == "" {
		provided = strings.TrimSpace(request.Header.Get("X-CSRF-Token"))
	}
	var expected string
	for _, name := range []string{"jms_csrftoken", "jms_csrf_token", "csrftoken", "csrf_token"} {
		if cookie, err := request.Cookie(name); err == nil && cookie.Value != "" {
			expected = cookie.Value
			break
		}
	}
	if provided == "" || expected == "" || !hmac.Equal([]byte(provided), []byte(expected)) {
		return ErrCSRFRejected
	}
	return nil
}

func normalizeOrigin(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func firstForwarded(value string) string { return strings.TrimSpace(strings.Split(value, ",")[0]) }
