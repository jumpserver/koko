// Package sshcert recognises OpenSSH user certificates that are
// stored alongside an OpenSSH private key in a koko "PrivateKey"
// secret, and exposes helpers that build an ssh.Signer ready for
// srvconn.SSHClientPrivateAuth.
//
// The format is the conventional concatenation of a private key
// PEM block and a single certificate line emitted by tools such
// as ssh-keygen -s or smallstep step-ca:
//
//	-----BEGIN OPENSSH PRIVATE KEY-----
//	<base64>
//	-----END OPENSSH PRIVATE KEY-----
//	ecdsa-sha2-nistp256-cert-v01@openssh.com AAAA... <key-id>
//
// Parse opens such a blob, validates that the certificate's
// public key matches the embedded private key, and returns the
// signer and the certificate separately. NewSigner wraps the two
// with ssh.NewCertSigner when a certificate is present, otherwise
// it returns the plain signer produced by ssh.ParsePrivateKey - so
// existing koko deployments that store only a private key
// continue to authenticate exactly as before.
//
// The package is deliberately minimal: it relies only on
// golang.org/x/crypto/ssh and never touches the filesystem, the
// network or koko's session lifecycle.
package sshcert

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

// certAlgorithmMarker is the substring that identifies an OpenSSH
// user certificate line in an authorized_keys-style blob.
const certAlgorithmMarker = "-cert-v01@openssh.com"

// ErrCertMismatch is returned when a secret blob contains a
// certificate whose embedded public key does not match the
// private key in the same blob. The mismatch is treated as a hard
// error so that operators do not accidentally authenticate with
// the wrong identity (e.g. an old certificate that outlived a
// key rotation).
var ErrCertMismatch = errors.New("sshcert: certificate public key does not match private key")

// ParseResult holds the artefacts of parsing an OpenSSH secret
// blob that may or may not contain an SSH certificate.
type ParseResult struct {
	// Signer is always non-nil on success. It signs using the
	// embedded private key; when HasCert is true the caller
	// should wrap it with ssh.NewCertSigner.
	Signer ssh.Signer

	// Cert is the parsed certificate, or nil when HasCert is false.
	Cert *ssh.Certificate

	// HasCert reports whether the secret blob carried a matching
	// certificate line.
	HasCert bool
}

// Parse inspects secret and returns the underlying signer together
// with any bundled SSH certificate. A returned ParseResult.Signer
// is always safe to pass to ssh.PublicKeys; when HasCert is true
// the caller should wrap it via ssh.NewCertSigner before use so
// that the certificate is presented during authentication.
//
// Parse is the lower-level entry point; most callers should use
// NewSigner directly.
func Parse(secret []byte) (ParseResult, error) {
	var res ParseResult

	signer, err := ssh.ParsePrivateKey(secret)
	if err != nil {
		return res, err
	}
	res.Signer = signer

	signerPub := signer.PublicKey().Marshal()

	for _, line := range strings.Split(string(secret), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, certAlgorithmMarker) {
			continue
		}
		parsed, _, _, _, parseErr := ssh.ParseAuthorizedKey([]byte(line))
		if parseErr != nil {
			// Skip malformed lines but keep scanning so a
			// single bad line does not deny authentication with
			// the remaining valid key material.
			continue
		}
		cert, ok := parsed.(*ssh.Certificate)
		if !ok {
			continue
		}
		if !bytes.Equal(cert.Key.Marshal(), signerPub) {
			return res, ErrCertMismatch
		}
		res.Cert = cert
		res.HasCert = true
		break
	}

	return res, nil
}

// NewSigner returns an ssh.Signer built from secret. If secret
// contains a certificate line whose public key matches the
// embedded private key, the returned signer is a CertSigner that
// presents the certificate during SSH user-auth. Otherwise the
// returned signer is the plain signer produced by
// ssh.ParsePrivateKey.
//
// The function never returns (nil, nil): either a usable signer is
// returned together with a nil error, or the underlying parse
// error is propagated.
func NewSigner(secret []byte) (ssh.Signer, error) {
	res, err := Parse(secret)
	if err != nil {
		return nil, err
	}
	if !res.HasCert {
		return res.Signer, nil
	}
	return ssh.NewCertSigner(res.Cert, res.Signer)
}
