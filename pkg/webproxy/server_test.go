package webproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHTTPProxyForwardsAllowedTarget(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "proxied")
	}))
	defer upstream.Close()

	proxy, err := NewServer("127.0.0.1", "0", "127.0.0.1", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	response, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "proxied" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestDefaultTargetsWorkOnContainerBind(t *testing.T) {
	proxy, err := NewServer("0.0.0.0", "5001", "*", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"qq.com:443", "example.com:80", "host.docker.internal:8080"} {
		if !proxy.isAllowed(target) {
			t.Fatalf("default proxy rejected %s", target)
		}
	}
}
