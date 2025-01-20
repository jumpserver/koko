package srvconn

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
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
	return k.server.ListenAndServe()
}

func (k *K8sReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger.Infof("k8s proxy request: %s", r.URL.Path)
	token := r.Header.Get("Authorization")
	if token == "" {
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
	value := val.(string)
	targetUrl, err := url.Parse(value)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(targetUrl)
	if isWebSocketRequest(r) {
		logger.Infof("k8s proxy websocket: %s", r.URL.Path)
		serveWebSocket(w, r, targetUrl)
		return
	}
	proxy.ServeHTTP(w, r)
}

func isWebSocketRequest(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}

func serveWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL) {
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = target.Path
		req.Header.Set("Host", target.Host)
	}
	proxy := &httputil.ReverseProxy{Director: director}
	proxy.ServeHTTP(w, r)
}
