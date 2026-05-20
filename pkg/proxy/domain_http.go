package proxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

type domainHTTP struct {
	sshClient       *gossh.Client
	selectedGateway *model.Gateway
	ln              net.Listener
	server          *http.Server

	once sync.Once
}

func (d *domainHTTP) Start() (err error) {
	if !d.getAvailableGateway() {
		return ErrNoAvailable
	}

	d.ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = d.sshClient.Close()
		return err
	}

	d.server = &http.Server{
		Handler: http.HandlerFunc(d.serveHTTP),
	}
	go d.run()
	logger.Infof("Domain HTTP %s start listen on %s", d.selectedGateway.Name, d.ln.Addr())
	return nil
}

func (d *domainHTTP) Stop() {
	d.closeOnce()
}

func (d *domainHTTP) GetListenAddr() *net.TCPAddr {
	return d.ln.Addr().(*net.TCPAddr)
}

func (d *domainHTTP) run() {
	defer d.closeOnce()
	if err := d.server.Serve(d.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Errorf("Domain HTTP %s serve err: %s", d.selectedGateway.Name, err)
	}
	logger.Infof("Domain HTTP %s stop listen on %s", d.selectedGateway.Name, d.ln.Addr())
}

func (d *domainHTTP) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		logger.Errorf("Domain HTTP %s reject unsupported method %s target %s",
			d.selectedGateway.Name, r.Method, r.Host)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dstAddr, err := normalizeConnectAddr(r.Host)
	if err != nil {
		logger.Errorf("Domain HTTP %s invalid connect target %q: %s", d.selectedGateway.Name, r.Host, err)
		http.Error(w, "bad connect target", http.StatusBadRequest)
		return
	}

	dstCon, err := d.sshClient.Dial("tcp", dstAddr)
	if err != nil {
		logger.Errorf("Domain HTTP %s connect %s err: %s", d.selectedGateway.Name, dstAddr, err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = dstCon.Close()
		logger.Errorf("Domain HTTP %s response writer does not support hijacking", d.selectedGateway.Name)
		http.Error(w, "proxy hijack unsupported", http.StatusInternalServerError)
		return
	}

	srcCon, rw, err := hijacker.Hijack()
	if err != nil {
		_ = dstCon.Close()
		logger.Errorf("Domain HTTP %s hijack conn err: %s", d.selectedGateway.Name, err)
		http.Error(w, "proxy hijack failed", http.StatusInternalServerError)
		return
	}
	defer srcCon.Close()
	defer dstCon.Close()

	if _, err = io.WriteString(srcCon, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		logger.Errorf("Domain HTTP %s write connect response to %s err: %s",
			d.selectedGateway.Name, dstAddr, err)
		return
	}
	if buffered := rw.Reader.Buffered(); buffered > 0 {
		if _, err = io.CopyN(dstCon, rw, int64(buffered)); err != nil && !isConnClosedErr(err) {
			logger.Errorf("Domain HTTP %s flush buffered data to %s err: %s",
				d.selectedGateway.Name, dstAddr, err)
			return
		}
	}

	logger.Infof("Domain HTTP %s connected %s(%p)", d.selectedGateway.Name, dstAddr, dstCon)
	done := make(chan struct{}, 2)
	go d.copyTunnel(done, dstCon, srcCon, dstAddr, "write")
	go d.copyTunnel(done, srcCon, dstCon, dstAddr, "read")
	<-done
	logger.Infof("Domain HTTP %s connect %s(%p) done", d.selectedGateway.Name, dstAddr, dstCon)
}

func (d *domainHTTP) copyTunnel(done chan<- struct{}, dst io.Writer, src io.Reader, dstAddr, action string) {
	defer func() {
		done <- struct{}{}
		logger.Debugf("Domain HTTP %s dst %s stop %s", d.selectedGateway.Name, dstAddr, action)
	}()
	if _, err := io.Copy(dst, src); err != nil && !isConnClosedErr(err) {
		logger.Debugf("Domain HTTP %s copy %s err: %s", d.selectedGateway.Name, dstAddr, err)
	}
}

func (d *domainHTTP) getAvailableGateway() bool {
	if d.selectedGateway == nil {
		return false
	}

	sshClient, err := newGatewaySSHClient(d.selectedGateway)
	if err != nil {
		logger.Errorf("Dial select gateway %s err: %s ", d.selectedGateway.Name, err)
		return false
	}
	d.sshClient = sshClient
	return true
}

func (d *domainHTTP) closeOnce() {
	d.once.Do(func() {
		if d.server != nil {
			_ = d.server.Close()
		}
		if d.ln != nil {
			_ = d.ln.Close()
		}
		if d.sshClient != nil {
			_ = d.sshClient.Close()
		}
		if d.selectedGateway != nil {
			logger.Debugf("Domain HTTP %s close listen and gateway ssh client", d.selectedGateway.Name)
		}
	})
}

func normalizeConnectAddr(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errors.New("empty host")
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host, nil
	}

	addr := net.JoinHostPort(host, "443")
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return "", err
	}
	return addr, nil
}

func isConnClosedErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "closed network connection")
}
