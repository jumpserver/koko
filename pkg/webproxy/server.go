package webproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

const shutdownTimeout = 5 * time.Second

type Server struct {
	server       *http.Server
	transport    *http.Transport
	allowedHosts []string
	recordings   *recordingManager
	credentials  *credentialManager
}

func NewServer(bindHost, port, allowedHosts, recordingRoot, ffmpegPath string, tokenService connectTokenService) (*Server, error) {
	allowed := splitAllowedHosts(allowedHosts)
	if len(allowed) == 0 {
		return nil, errors.New("WEB_PROXY_ALLOWED_HOSTS is required when web proxy is enabled")
	}
	if containsWildcard(allowed) && !isLoopbackHost(bindHost) {
		return nil, errors.New("WEB_PROXY_ALLOWED_HOSTS=* requires a loopback WEB_PROXY_BIND_HOST")
	}

	proxy := &Server{
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		allowedHosts: allowed,
		credentials:  newCredentialManager(tokenService),
	}
	if recordingRoot != "" {
		manager, err := newRecordingManager(recordingRoot, ffmpegPath)
		if err != nil {
			return nil, err
		}
		proxy.recordings = manager
	}
	proxy.server = &http.Server{
		Addr:              net.JoinHostPort(bindHost, port),
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	return proxy, nil
}

func (s *Server) Start() {
	logger.Infof("Start Web proxy server at %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Errorf("Web proxy server stopped unexpectedly: %s", err)
	}
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		logger.Errorf("Stop Web proxy server failed: %s", err)
	}
	s.transport.CloseIdleConnections()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, credentialPathPrefix) && !r.URL.IsAbs() {
		s.serveCredentials(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, recordingPathPrefix) && !r.URL.IsAbs() {
		s.serveRecording(w, r)
		return
	}

	target := r.URL.Host
	if r.Method == http.MethodConnect {
		target = r.Host
	}
	if !s.isAllowed(target) {
		http.Error(w, "target host is not allowed", http.StatusForbidden)
		return
	}

	if r.Method == http.MethodConnect {
		s.serveTunnel(w, r)
		return
	}
	s.serveHTTP(w, r)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		http.Error(w, "unsupported target scheme", http.StatusBadRequest)
		return
	}

	outbound := r.Clone(r.Context())
	outbound.RequestURI = ""
	removeHopByHopHeaders(outbound.Header)

	response, err := s.transport.RoundTrip(outbound)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		logger.Warnf("Web proxy request to %s failed: %s", r.URL.Host, err)
		return
	}
	defer response.Body.Close()

	removeHopByHopHeaders(response.Header)
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	logger.Infof("Web proxy %s %s -> %d", r.Method, r.URL.Host, response.StatusCode)
}

func (s *Server) serveTunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		logger.Warnf("Web proxy CONNECT to %s failed: %s", r.Host, err)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "connection hijacking is unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	defer client.Close()
	defer upstream.Close()

	if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffered.Flush(); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go copyTunnel(upstream, buffered, done)
	go copyTunnel(client, upstream, done)
	<-done
	logger.Infof("Web proxy CONNECT %s closed", r.Host)
}

func (s *Server) isAllowed(authority string) bool {
	host := normalizedHost(authority)
	for _, allowed := range s.allowedHosts {
		switch {
		case allowed == "*":
			return true
		case strings.HasPrefix(allowed, "*."):
			suffix := strings.TrimPrefix(allowed, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		case normalizedHost(allowed) == host:
			return true
		}
	}
	return false
}

func splitAllowedHosts(value string) []string {
	var result []string
	for _, host := range strings.Split(value, ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			result = append(result, host)
		}
	}
	return result
}

func normalizedHost(authority string) string {
	authority = strings.ToLower(strings.TrimSpace(authority))
	if host, _, err := net.SplitHostPort(authority); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(authority, "[]")
}

func containsWildcard(hosts []string) bool {
	for _, host := range hosts {
		if host == "*" {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = normalizedHost(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr := net.ParseIP(host)
	return addr != nil && addr.IsLoopback()
}

func removeHopByHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection",
		"TE", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(name)
	}
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func copyTunnel(dst io.Writer, src io.Reader, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	done <- struct{}{}
}
