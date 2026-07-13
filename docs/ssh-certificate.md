# SSH user certificate support in koko

`pkg/sshcert` recognises OpenSSH user certificates that are
stored alongside an OpenSSH private key in a koko `PrivateKey`
secret, and exposes helpers that build an `ssh.Signer` ready for
`srvconn.SSHClientPrivateAuth`.

## Wire format

A koko `PrivateKey` secret may now carry an OpenSSH user
certificate line after the private key PEM block:

```
-----BEGIN OPENSSH PRIVATE KEY-----
<base64 ed25519 / ecdsa / rsa private key>
-----END OPENSSH PRIVATE KEY-----
ecdsa-sha2-nistp256-cert-v01@openssh.com AAAA... <key-id>
```

`pkg/handler.buildSSHClientOptions` calls `sshcert.NewSigner`:

- if the secret contains a matching `-cert-v01@openssh.com` line,
  the returned signer is a `CertSigner` that presents the
  certificate during SSH user-auth;
- otherwise the returned signer is the plain signer produced by
  `ssh.ParsePrivateKey`, so existing deployments are unaffected.

## Operator workflow

The secret blob can be produced by any tool that emits the
conventional private key + certificate concatenation. Common
options include:

- `ssh-keygen -s ca_key -I key-id id_key.pub` after appending the
  signed `*-cert.pub` to the secret;
- `step ssh certificate <id> <key>` from
  [smallstep step-ca](https://smallstep.com/docs/step-ca/),
  which writes the certificate line into the same file as the
  private key;
- HashiCorp Vault SSH secrets engine, AWS SSM Session Manager,
  or any other CA whose signed output is written alongside the
  private key.

The certificate can then be put into the JumpServer asset
account `private_key` field via the JMS REST API, or pasted into
the Luna UI as a single text blob. Once the field is saved, koko
will negotiate the `*-cert-v01@openssh.com` SSH user-auth
algorithm with the target host, and the host's sshd will validate
the certificate against its `TrustedUserCAKeys` instead of its
`authorized_keys`.

## Failure modes

| Scenario | Behaviour |
| --- | --- |
| Secret is a plain private key (no certificate line) | `NewSigner` returns the plain signer; existing deployments are unaffected. |
| Secret contains a matching certificate | `NewSigner` returns a `CertSigner`; SSH user-auth is performed with the certificate. |
| Secret contains a certificate whose public key does not match the embedded private key | `NewSigner` returns `ErrCertMismatch`; the existing log line `Parse account X private key failed: ...` is emitted and the SSH user-auth method is left empty. |
| Secret is malformed | `NewSigner` returns the underlying parse error; the existing log line is emitted. |

## Reference

- [OpenSSH certificates PROTOCOL.certkeys](https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.certkeys)
- [step-ca SSH certificate workflow](https://smallstep.com/docs/step-ca/ssh/)
