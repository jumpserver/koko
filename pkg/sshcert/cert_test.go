package sshcert

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// makeKeyAndCert generates a fresh ed25519 key, returns the
// PEM-armoured OpenSSH private key blob, the matching
// certificate's authorized-keys line, and the underlying signer.
func makeKeyAndCert(t *testing.T, keyID string, principals []string, serial uint64) ([]byte, string, ssh.Signer) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("ssh.NewSignerFromKey: %v", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("ssh.MarshalPrivateKey: %v", err)
	}
	keyBytes := pem.EncodeToMemory(pemBlock)

	cert := &ssh.Certificate{
		Key:             signer.PublicKey(),
		Serial:          serial,
		CertType:        ssh.UserCert,
		KeyId:           keyID,
		ValidPrincipals: principals,
		ValidAfter:      uint64(0),
		ValidBefore:     ssh.CertTimeInfinity,
	}
	if err := cert.SignCert(rand.Reader, signer); err != nil {
		t.Fatalf("cert.SignCert: %v", err)
	}
	certLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(cert)))

	return keyBytes, certLine, signer
}

func TestNewSigner_PlainKey(t *testing.T) {
	keyBytes, _, _ := makeKeyAndCert(t, "k", []string{"alice"}, 1)
	signer, err := NewSigner(keyBytes)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if signer == nil {
		t.Fatal("expected non-nil signer")
	}
	if signer.PublicKey().Type() == ssh.CertAlgoED25519v01 {
		t.Fatalf("plain key should not produce a cert signer, got type %q",
			signer.PublicKey().Type())
	}
}

func TestNewSigner_WithCert(t *testing.T) {
	keyBytes, certLine, _ := makeKeyAndCert(t, "ops", []string{"root"}, 42)

	var secret []byte
	secret = append(secret, keyBytes...)
	secret = append(secret, '\n')
	secret = append(secret, certLine...)
	secret = append(secret, '\n')

	signer, err := NewSigner(secret)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if signer.PublicKey().Type() != ssh.CertAlgoED25519v01 {
		t.Fatalf("expected certificate signer, got type %q",
			signer.PublicKey().Type())
	}
}

func TestNewSigner_CertKeyMismatch(t *testing.T) {
	keyA, _, _ := makeKeyAndCert(t, "kA", []string{"alice"}, 1)
	_, certLineB, _ := makeKeyAndCert(t, "kB", []string{"bob"}, 2)

	var secret []byte
	secret = append(secret, keyA...)
	secret = append(secret, '\n')
	secret = append(secret, certLineB...)
	secret = append(secret, '\n')

	if _, err := NewSigner(secret); err != ErrCertMismatch {
		t.Fatalf("expected ErrCertMismatch, got %v", err)
	}
}

func TestParse_ReportsHasCert(t *testing.T) {
	keyBytes, certLine, _ := makeKeyAndCert(t, "ops", []string{"root", "deploy"}, 100)

	var secret []byte
	secret = append(secret, keyBytes...)
	secret = append(secret, '\n')
	secret = append(secret, certLine...)
	secret = append(secret, '\n')

	res, err := Parse(secret)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCert {
		t.Fatal("expected HasCert=true")
	}
	if res.Cert == nil {
		t.Fatal("expected non-nil Cert")
	}
	if res.Cert.Serial != 100 {
		t.Fatalf("expected serial=100, got %d", res.Cert.Serial)
	}
	if !slices.Equal(res.Cert.ValidPrincipals, []string{"root", "deploy"}) {
		t.Fatalf("principals: got %v, want [root deploy]",
			res.Cert.ValidPrincipals)
	}
}

func TestParse_NoCert(t *testing.T) {
	keyBytes, _, _ := makeKeyAndCert(t, "ops", []string{"root"}, 1)
	res, err := Parse(keyBytes)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.HasCert {
		t.Fatal("expected HasCert=false for key-only secret")
	}
	if res.Cert != nil {
		t.Fatal("expected nil Cert for key-only secret")
	}
}
