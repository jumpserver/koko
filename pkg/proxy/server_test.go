package proxy

import (
	"testing"

	"github.com/jumpserver/koko/pkg/srvconn"
)

func TestReleaseManagedSSHClientWithoutReuseClosesClient(t *testing.T) {
	t.Cleanup(func() {
		closeSSHClient = func(client *srvconn.SSHClient) {
			_ = client.Close()
		}
		releaseClientCacheKey = srvconn.ReleaseClientCacheKey
	})

	var (
		closeCalled   bool
		releaseCalled bool
	)
	closeSSHClient = func(client *srvconn.SSHClient) {
		closeCalled = true
	}
	releaseClientCacheKey = func(key string, client *srvconn.SSHClient) {
		releaseCalled = true
	}

	releaseManagedSSHClient("cache-key", &srvconn.SSHClient{}, false)

	if !closeCalled {
		t.Fatal("expected ssh client to be closed when reuse is disabled")
	}
	if releaseCalled {
		t.Fatal("expected cache release to be skipped when reuse is disabled")
	}
}

func TestReleaseManagedSSHClientWithReuseReleasesCacheKey(t *testing.T) {
	t.Cleanup(func() {
		closeSSHClient = func(client *srvconn.SSHClient) {
			_ = client.Close()
		}
		releaseClientCacheKey = srvconn.ReleaseClientCacheKey
	})

	var (
		closeCalled   bool
		releaseCalled bool
	)
	closeSSHClient = func(client *srvconn.SSHClient) {
		closeCalled = true
	}
	releaseClientCacheKey = func(key string, client *srvconn.SSHClient) {
		releaseCalled = true
	}

	releaseManagedSSHClient("cache-key", &srvconn.SSHClient{}, true)

	if closeCalled {
		t.Fatal("expected direct close to be skipped when reuse is enabled")
	}
	if !releaseCalled {
		t.Fatal("expected cache release to run when reuse is enabled")
	}
}

func TestReleaseManagedSSHClientNilClientNoop(t *testing.T) {
	t.Cleanup(func() {
		closeSSHClient = func(client *srvconn.SSHClient) {
			_ = client.Close()
		}
		releaseClientCacheKey = srvconn.ReleaseClientCacheKey
	})

	var called bool
	closeSSHClient = func(client *srvconn.SSHClient) {
		called = true
	}
	releaseClientCacheKey = func(key string, client *srvconn.SSHClient) {
		called = true
	}

	releaseManagedSSHClient("cache-key", nil, true)
	releaseManagedSSHClient("cache-key", nil, false)

	if called {
		t.Fatal("expected nil ssh client release to be a no-op")
	}
}
