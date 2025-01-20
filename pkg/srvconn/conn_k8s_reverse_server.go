package srvconn

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"

	"github.com/jumpserver/koko/pkg/logger"
)

var gloablTokenMaps = sync.Map{}
var K8sReverseProxyPort = 6000

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
	logger.Infof("k8s proxy reverse request start: %s", r.URL.Path)
	token := r.Header.Get("Authorization")
	if token == "" {
		logger.Errorf("k8s proxy reverse request unauthorized")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token = token[len("Bearer "):]
	logger.Infof("k8s proxy token: %s", token)
	val, ok := gloablTokenMaps.Load(token)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	unixSocketPath := val.(string)
	targetUrl := &url.URL{Scheme: "http", Host: "unix"}
	// Create custom transport for Unix socket
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
	proxy.ServeHTTP(w, r)
	logger.Infof("k8s proxy reverse request done: %s", r.URL.Path)
}
