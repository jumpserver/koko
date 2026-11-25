package chen

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/engine"
)

// webHandler serves a generated Luna tree from the same origin as the API.
// Only the standalone entry gets an SPA fallback; missing assets remain 404s.
func webHandler(api http.Handler, listen, webDir string) (http.Handler, func(), error) {
	var root *os.Root
	var files http.Handler
	closeWeb := func() {}
	if webDir != "" {
		var err error
		root, err = os.OpenRoot(webDir)
		if err != nil {
			return nil, nil, errors.New("web_directory_unreadable")
		}
		closeWeb = func() { _ = root.Close() }
		if info, err := root.Stat("index.html"); err != nil || !info.Mode().IsRegular() {
			closeWeb()
			return nil, nil, errors.New("web_directory_requires_generated_luna_index")
		}
		files = http.StripPrefix("/luna/", http.FileServerFS(root.FS()))
	}
	listenHost, _, err := net.SplitHostPort(listen)
	if err != nil {
		closeWeb()
		return nil, nil, errors.New("listen_address_must_be_host_port")
	}
	loopbackOnly := isLoopbackHost(listenHost)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		// A hostile hostname resolving to loopback must not turn the selector
		// endpoint into an unauthenticated DNS-rebinding bridge to the database.
		host := r.Host
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		if loopbackOnly && !isLoopbackHost(host) {
			http.Error(w, "Untrusted host", http.StatusForbidden)
			return
		}
		path := r.URL.Path
		if strings.HasPrefix(path, engine.PathPrefix+"/") || root == nil {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if path == "/" || path == "/luna" || path == "/luna/" || path == "/luna/chen" {
			location := "/luna/chen/"
			if r.URL.RawQuery != "" {
				location += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, location, http.StatusTemporaryRedirect)
			return
		}
		if path == "/luna/chen/" {
			f, err := root.Open("index.html")
			if err != nil {
				http.Error(w, "Workspace is unavailable", http.StatusServiceUnavailable)
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				http.Error(w, "Workspace is unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			http.ServeContent(w, r, "index.html", info.ModTime(), f)
			return
		}
		if strings.HasPrefix(path, "/luna/") {
			name := strings.TrimPrefix(path, "/luna/")
			for _, part := range strings.Split(name, "/") {
				if part == "" || strings.HasPrefix(part, ".") {
					http.NotFound(w, r)
					return
				}
			}
			if info, err := root.Stat(name); err == nil && info.Mode().IsRegular() {
				files.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	}), closeWeb, nil
}

func isLoopbackHost(host string) bool {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
