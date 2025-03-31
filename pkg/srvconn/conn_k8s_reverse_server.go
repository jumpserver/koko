package srvconn

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/logger"
)

var (
	gloablTokenMaps     = sync.Map{}
	K8sReverseProxyPort = 5002
	k8sReverseProxyURL  = "https://127.0.0.1:5002"
)

func init() {
	reverseProxy := NewK8sReverseProxy(K8sReverseProxyPort)
	go func() {
		if err := reverseProxy.Start(); err != nil {
			logger.Errorf("k8s reverse proxy start failed: %v", err)
		}
	}()
}

type K8sReverseProxy struct {
	Port int

	server *http.Server
}

func NewK8sReverseProxy(port int) *K8sReverseProxy {
	return &K8sReverseProxy{
		Port: port,
	}
}

func (k *K8sReverseProxy) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", k.ServeHTTP)
	k.server = &http.Server{
		Addr:    ":" + strconv.Itoa(k.Port),
		Handler: mux,
	}
	return k.server.ListenAndServeTLS("server.crt", "server.key")
}

func (k *K8sReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bearerToken := r.Header.Get("Authorization")
	if bearerToken == "" {
		logger.Error("k8s proxy reverse request unauthorized: without token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	tokens := strings.SplitN(bearerToken, " ", 2)
	schemaName := strings.TrimSpace(tokens[0])
	if len(tokens) != 2 || schemaName != "Bearer" {
		logger.Errorf("k8s proxy reverse request unauthorized: invalid token: %s", bearerToken)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tokenId := strings.TrimSpace(tokens[1])
	val, ok := gloablTokenMaps.Load(tokenId)
	if !ok {
		logger.Error("k8s proxy reverse request unauthorized: token not found")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	unixSocketPath := val.(string)
	targetUrl := &url.URL{Scheme: "http", Host: "unix"}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", unixSocketPath)
		},
	}
	proxy := httputil.ReverseProxy{
		Transport: transport,
		Director: func(req *http.Request) {
			req.URL.Scheme = targetUrl.Scheme
			req.URL.Host = targetUrl.Host
			req.Header.Set("Host", targetUrl.Host)
			req.Header.Del("Authorization")
		},
	}
	logger.Debugf("k8s reverse proxy %s request start: %s", tokenId, r.URL.Path)
	proxy.ServeHTTP(w, r)
	logger.Debugf("k8s reverse proxy %s request end: %s", tokenId, r.URL.Path)
}
