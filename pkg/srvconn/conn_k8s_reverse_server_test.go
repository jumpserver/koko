package srvconn

import (
	"path/filepath"
	"testing"
)

func TestLoadK8sReverseProxyCertificateWithoutFiles(t *testing.T) {
	dir := t.TempDir()
	certificate, err := loadK8sReverseProxyCertificate(
		filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"),
	)
	if err != nil {
		t.Fatalf("generate ephemeral certificate: %v", err)
	}
	if len(certificate.Certificate) == 0 {
		t.Fatal("generated certificate is empty")
	}
}
