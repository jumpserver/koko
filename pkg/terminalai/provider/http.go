package provider

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultRequestTimeout = 5 * time.Minute

func newHTTPClient(config Config) (*http.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if value := strings.TrimSpace(config.Proxy); value != "" {
		proxyURL, err := url.Parse(value)
		if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
			return nil, fmt.Errorf("terminal AI proxy URL is invalid")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	timeout := config.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func responseRequestID(response *http.Response) string {
	if response == nil {
		return ""
	}
	if value := response.Header.Get("x-request-id"); value != "" {
		return value
	}
	return response.Header.Get("request-id")
}

func observableBaseURL(value string) string {
	if strings.TrimSpace(value) == "" {
		return "https://api.openai.com/v1"
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
