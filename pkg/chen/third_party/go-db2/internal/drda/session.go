package drda

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-db2/go-db2/internal/auth"
	"github.com/go-db2/go-db2/internal/codec"
	"github.com/go-db2/go-db2/internal/validate"
)

// SessionConfig holds configuration parameters for establishing a Db2 session.
type SessionConfig struct {
	Host               string
	Port               int
	Database           string
	User               string
	Password           string
	UseSSL             bool
	SSLRootCAPath      string
	SSLClientCertPath  string
	SSLClientKeyPath   string
	TLSConfig          *tls.Config
	Timeout            time.Duration
	DialContext        func(context.Context, string, string) (net.Conn, error)
	BlockSize          int
	SecurityMechanism  uint16
	AllowPlaintextAuth bool
	KerberosSPN        string
	KerberosRealm      string
	Krb5ConfigFile     string
	Krb5KeytabFile     string
	Krb5CCacheFile     string
	ClientApplName     string
	ClientWrkstnName   string
	ClientUserid       string
	ClientAcctng       string
	ClientCorrToken    string
}

// sanitizeLogString removes ASCII control characters (< 32 and 127) for log sanitization.
// Optimization: Fast-path byte scan returns clean strings directly (0 B/op, 0 allocs/op) and uses a single pre-allocated strings.Builder pass for modified strings.
func sanitizeLogString(s string) string {
	idx := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 32 || c == 127 {
			idx = i
			break
		}
	}
	if idx == -1 {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	sb.WriteString(s[:idx])
	for i := idx; i < len(s); i++ {
		c := s[i]
		if c >= 32 && c != 127 {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// String implements fmt.Stringer to ensure sensitive credentials (Password) are redacted in logs.
func (s SessionConfig) String() string {
	return fmt.Sprintf("SessionConfig{Host:%s, Port:%d, Database:%s, User:%s, Password:\"******\", UseSSL:%t, Timeout:%v}",
		sanitizeLogString(s.Host), s.Port, sanitizeLogString(s.Database), sanitizeLogString(s.User), s.UseSSL, s.Timeout)
}

// GoString implements fmt.GoStringer to ensure sensitive credentials (Password) are redacted in logs.
func (s SessionConfig) GoString() string {
	return s.String()
}

// ReplyPacket represents a single decoded DSS reply with its outer DDM codepoint and raw payload.
type ReplyPacket struct {
	Header    *DSSHeader
	CodePoint CodePoint
	Data      []byte
	MoreData  bool
}

// ReservedDirectSec is the reserved DRDA package section number for direct/immediate statements.
const ReservedDirectSec uint16 = 1

// Session manages the underlying network socket and DRDA protocol state with the Db2 server.
type Session struct {
	cfg           SessionConfig
	conn          net.Conn
	mu            sync.Mutex
	correlationID uint16
	secmec        uint16
	secTkn        []byte
	encoding      StringEncoding
	endian        binary.ByteOrder
	prdid         string
	pkgid         string
	pkgcnstkn     string
	pkgsn         uint16
	qryblksz      uint32
	rdbinttkn     []byte
	autoCommit    bool
	closed        bool
	// broken is set once the byte stream can no longer be trusted: a socket
	// read or write failed, or a reply could not be framed. Unread bytes may
	// be left on the socket, so no further request may use it.
	broken atomic.Bool
}

// Broken reports whether the session has seen an I/O or framing error and
// must not be used for further requests.
func (s *Session) Broken() bool {
	return s.broken.Load()
}

// brokenOnErrorConn marks its session broken on the first failed read or write.
type brokenOnErrorConn struct {
	net.Conn
	broken *atomic.Bool
	// interruptedBy holds the context error that cut the current request short.
	interruptedBy atomic.Pointer[error]
}

func (c *brokenOnErrorConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		err = c.fail(err)
	}
	return n, err
}

func (c *brokenOnErrorConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err != nil {
		err = c.fail(err)
	}
	return n, err
}

// fail marks the session broken and, when the I/O was cut short by a context,
// reports the context's error so callers can match it with errors.Is.
func (c *brokenOnErrorConn) fail(err error) error {
	c.broken.Store(true)
	if cause := c.interruptedBy.Load(); cause != nil {
		return fmt.Errorf("%w (%v)", *cause, err)
	}
	return err
}

// watch interrupts the session's socket I/O when ctx is done, by moving the
// socket deadline to now; call the returned function once the request is over.
// The request is abandoned mid-exchange, so the session is marked broken and
// its connection is discarded.
func (s *Session) watch(ctx context.Context) func() {
	c, ok := s.conn.(*brokenOnErrorConn)
	if !ok || ctx.Done() == nil {
		return func() {}
	}
	stop := context.AfterFunc(ctx, func() {
		cause := context.Cause(ctx)
		c.interruptedBy.Store(&cause)
		c.broken.Store(true)
		_ = c.SetDeadline(time.Now())
	})
	return func() { stop() }
}

// NewSession instantiates a new Db2 network session configuration.
func NewSession(cfg SessionConfig) *Session {
	if cfg.Port == 0 {
		if cfg.UseSSL {
			cfg.Port = 50001
		} else {
			cfg.Port = 50000
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	dbPadded := fmt.Sprintf("%-18s", cfg.Database)
	if len(dbPadded) > 18 {
		dbPadded = dbPadded[:18]
	}
	cfg.Database = dbPadded

	blksz := uint32(cfg.BlockSize)
	if blksz < 1024 {
		blksz = 65535
	}

	secmec := cfg.SecurityMechanism
	if secmec == 0 {
		secmec = SecMecUSRIDPWD
	}

	return &Session{
		cfg:           cfg,
		correlationID: 1,
		secmec:        secmec,
		encoding:      EncodingCP500,
		endian:        binary.LittleEndian,
		prdid:         "SQL12010",
		pkgid:         "SYSSH200",
		pkgcnstkn:     "SYSLVL01",
		pkgsn:         ReservedDirectSec,
		qryblksz:      blksz,
		autoCommit:    true,
	}
}

// AutoCommit returns the current auto-commit state of the session.
func (s *Session) AutoCommit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoCommit
}

// SetAutoCommit sets the auto-commit mode for subsequent operations.
func (s *Session) SetAutoCommit(auto bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoCommit = auto
}

// Connect dials the Db2 server via TCP/TLS and completes the DRDA handshake.
func (s *Session) Connect(ctx context.Context) error {
	if err := s.connectRaw(ctx); err != nil {
		return err
	}

	// Apply initial Client Info if configured in DSN (outside session mutex).
	if s.cfg.ClientApplName != "" || s.cfg.ClientWrkstnName != "" || s.cfg.ClientUserid != "" || s.cfg.ClientAcctng != "" || s.cfg.ClientCorrToken != "" {
		appl := s.cfg.ClientApplName
		wrkstn := s.cfg.ClientWrkstnName
		userid := s.cfg.ClientUserid
		acctng := s.cfg.ClientAcctng
		corr := s.cfg.ClientCorrToken

		// ResetClientInfo compares target registers against s.cfg in memory.
		// Clearing s.cfg client info fields ensures SetClientInfo executes the SET CLIENT commands on the Db2 server instead of skipping them thinking they were already set.
		s.mu.Lock()
		s.cfg.ClientApplName = ""
		s.cfg.ClientWrkstnName = ""
		s.cfg.ClientUserid = ""
		s.cfg.ClientAcctng = ""
		s.cfg.ClientCorrToken = ""
		s.mu.Unlock()

		if err := s.SetClientInfo(ctx, appl, wrkstn, userid, acctng, corr); err != nil {
			_ = s.Close()
			return fmt.Errorf("failed to apply client info (session closed): %w", err)
		}
	}

	return nil
}

func (s *Session) connectRaw(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		return errors.New("db2: session already connected")
	}

	if (s.secmec == SecMecUSRIDPWD || s.secmec == 3) && len(s.cfg.Password) > 0 && !s.cfg.UseSSL && !s.cfg.AllowPlaintextAuth {
		return errors.New("db2: plaintext authentication over unencrypted connection is disabled by default; set allow_plaintext_auth=true to override")
	}

	address := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	dial := s.cfg.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: s.cfg.Timeout}).DialContext
	}
	rawConn, err := dial(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("failed to connect to Db2 at %s: %w", address, err)
	}

	if s.cfg.UseSSL {
		if s.cfg.TLSConfig != nil {
			// Clone so raising MinVersion never changes the caller's config.
			tlsConfig := s.cfg.TLSConfig.Clone()
			if tlsConfig.ServerName == "" {
				tlsConfig.ServerName = s.cfg.Host
			}
			if tlsConfig.MinVersion < tls.VersionTLS12 {
				tlsConfig.MinVersion = tls.VersionTLS12
			}
			tlsConn := tls.Client(rawConn, tlsConfig)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = rawConn.Close()
				return fmt.Errorf("TLS handshake failed: %w", err)
			}
			rawConn = tlsConn
		} else {
			caPath := s.cfg.SSLRootCAPath
			if caPath == "" && s.cfg.SSLClientCertPath != "" && s.cfg.SSLClientKeyPath == "" {
				caPath = s.cfg.SSLClientCertPath
			}
			secTLSConfig := auth.TLSConfig{
				EnableTLS:          true,
				InsecureSkipVerify: false,
				ServerName:         s.cfg.Host,
				CACertPath:         caPath,
				ClientCertPath:     s.cfg.SSLClientCertPath,
				ClientKeyPath:      s.cfg.SSLClientKeyPath,
			}
			tlsConn, tlsErr := auth.WrapTLSContext(ctx, rawConn, secTLSConfig)
			if tlsErr != nil {
				_ = rawConn.Close()
				return fmt.Errorf("failed to establish TLS connection: %w", tlsErr)
			}
			rawConn = tlsConn
		}
	}

	if tcpConn, ok := rawConn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}

	s.conn = &brokenOnErrorConn{Conn: rawConn, broken: &s.broken}
	s.closed = false
	defer s.watch(ctx)()

	// Perform full DRDA Handshake
	if err := s.handshake(ctx); err != nil {
		_ = s.conn.Close()
		s.conn = nil
		s.closed = true
		return err
	}

	return nil
}

// handshake runs the DRDA initial protocol exchange (EXCSAT -> ACCSEC -> SECCHK -> ACCRDB).
func (s *Session) handshake(ctx context.Context) error {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "go-db2-client"
	}

	// 1. Pack EXCSAT
	excsat, err := PackEXCSAT("go-db2", hostname, "SQL12010", "go-db2", DefaultManagerLevels())
	if err != nil {
		return fmt.Errorf("failed to pack EXCSAT: %w", err)
	}

	// 2. Pack ACCSEC
	accsec, err := PackACCSEC(s.cfg.Database, s.secmec, nil)
	if err != nil {
		return fmt.Errorf("failed to pack ACCSEC: %w", err)
	}

	// Send EXCSAT (chained) and ACCSEC (last in chain)
	s.correlationID = 1
	s.correlationID, err = WriteRequestDSS(s.conn, excsat, s.correlationID, false, false)
	if err != nil {
		return fmt.Errorf("failed to send EXCSAT: %w", err)
	}
	s.correlationID, err = WriteRequestDSS(s.conn, accsec, s.correlationID, false, true)
	if err != nil {
		return fmt.Errorf("failed to send ACCSEC: %w", err)
	}

	// Read reply for ACCSEC
	negotiatedSecMec, secTkn, err := s.parseACCSECRD()
	if err != nil {
		return fmt.Errorf("ACCSEC negotiation failed: %w", err)
	}

	// S1 / FR-1 / AC-1: Refuse downgrade or any unsolicited security mechanism change offered by server
	if negotiatedSecMec != 0 && negotiatedSecMec != s.secmec {
		return fmt.Errorf("db2: server rejected security mechanism %d and offered %d; refusing downgrade", s.secmec, negotiatedSecMec)
	}

	// S2 / FR-1 / AC-1: Validate server security token for SECMEC 9
	if (s.secmec == SecMecEUSRIDPWD || s.secmec == 9) && len(secTkn) < 20 {
		return errors.New("db2: ACCSEC negotiation failed: invalid server security token")
	}

	// AC-1: Store server security token on session for subsequent re-authentication (SwitchUser)
	s.secTkn = secTkn

	// AC-2: Defense-in-depth check against plaintext authentication before packing SECCHK
	if (s.secmec == SecMecUSRIDPWD || s.secmec == 3) && len(s.cfg.Password) > 0 && !s.cfg.UseSSL && !s.cfg.AllowPlaintextAuth {
		return errors.New("db2: plaintext authentication over unencrypted connection is disabled by default; set allow_plaintext_auth=true to override")
	}

	// 3. Pack SECCHK (supporting Kerberos SECMEC 7/11, and SECMEC 9 with DH key exchange & DES encryption)
	var secchk []byte
	if s.secmec == auth.SecMecKERBEROS || s.secmec == auth.SecMecEUSERIDKERBEROS || s.secmec == 7 || s.secmec == 11 {
		return errors.New("db2: Kerberos security mechanism (SECMEC 7/11) is disabled until a safe, compliant RFC 4120 AP-REQ subpackage is integrated")
	} else if (s.secmec == SecMecEUSRIDPWD || s.secmec == 9) && len(secTkn) >= 20 {
		clientPriv, privErr := auth.GenerateDHPrivateKey()
		if privErr != nil {
			return fmt.Errorf("failed to generate DH private key: %w", privErr)
		}
		clientPub := auth.CalculateDHPublicKey(clientPriv)
		encPassword, encErr := auth.EncryptPasswordSECMEC9(s.cfg.Password, secTkn, clientPriv)
		if encErr != nil {
			return fmt.Errorf("failed to encrypt password with SECMEC 9: %w", encErr)
		}
		secchk, err = PackSECCHKWithBytes(s.secmec, clientPub, s.cfg.Database, s.cfg.User, encPassword, true, s.encoding)
	} else {
		secchk, err = PackSECCHK(s.secmec, secTkn, s.cfg.Database, s.cfg.User, s.cfg.Password, s.encoding)
	}
	if err != nil {
		return fmt.Errorf("failed to pack SECCHK: %w", err)
	}

	// 4. Pack ACCRDB
	accrdb, err := PackACCRDB(s.prdid, s.cfg.Database, s.encoding)
	if err != nil {
		return fmt.Errorf("failed to pack ACCRDB: %w", err)
	}

	// Send SECCHK (chained) and ACCRDB (last in chain)
	s.correlationID, err = WriteRequestDSS(s.conn, secchk, s.correlationID, false, false)
	if err != nil {
		return fmt.Errorf("failed to send SECCHK: %w", err)
	}
	s.correlationID, err = WriteRequestDSS(s.conn, accrdb, s.correlationID, false, true)
	if err != nil {
		return fmt.Errorf("failed to send ACCRDB: %w", err)
	}

	// Parse reply chain for SECCHK and ACCRDB
	replies, err := s.readReplyChain()
	if err != nil {
		return fmt.Errorf("handshake response failed: %w", err)
	}

	// Validate reply status
	for i, r := range replies {
		if r.CodePoint == CodePointSECCHKRM {
			sub, _ := ParseDDMReply(r.Data)
			if secChkCd, ok := sub[CodePointSECCHKCD]; ok && len(secChkCd) > 0 && secChkCd[0] != 0 {
				return NewError(-30082, "08001", fmt.Sprintf("SECCHKRM security check failed (SECCHKCD=%d)", secChkCd[0]), ErrAuthenticationFailed)
			}
		}
		if r.CodePoint == CodePointACCRDBRM {
			sub, _ := ParseDDMReply(r.Data)
			if inttkn, ok := sub[CodePointRDBINTTKN]; ok {
				s.rdbinttkn = inttkn
			}
		}
		if r.CodePoint == CodePointRDBAFLRM || r.CodePoint == CodePointRDBATHRM {
			return errors.New("authorization failed connecting to relational database")
		}
		if r.CodePoint == CodePointSQLERRRM {
			return parseSQLERRRM(replies[i:], s.endian)
		}
	}

	// Post-connect setup: Configure UTF-8 CCSID and client environment
	if err := s.setPostConnectVariables(hostname); err != nil {
		return fmt.Errorf("failed to set post-connect variables: %w", err)
	}

	return nil
}

// setPostConnectVariables sends CCSID and locale initialization queries to Db2.
func (s *Session) setPostConnectVariables(hostname string) error {
	s.correlationID = 1

	// EXCSAT with CCSIDMGR = 1208 (UTF-8)
	excsatMgr := PackEXCSAT_MGRLVLLS([]ManagerLevel{{Manager: CodePointCCSIDMGR, Level: 1208}})
	var err error
	s.correlationID, err = WriteRequestDSS(s.conn, excsatMgr, s.correlationID, false, false)
	if err != nil {
		return err
	}

	targetWrkstn := hostname
	if s.cfg.ClientWrkstnName != "" {
		targetWrkstn = s.cfg.ClientWrkstnName
	}

	// EXCSQLSET
	excSqlSet := PackEXCSQLSET(s.pkgid, "", 1, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, excSqlSet, s.correlationID, true, false)
	if err != nil {
		return err
	}

	// SET CLIENT WRKSTNNAME
	sqlWrkStn := PackSQLSTT(fmt.Sprintf("SET CLIENT WRKSTNNAME '%s'", escapeSingleQuotes(targetWrkstn)))
	s.correlationID, err = WriteRequestDSS(s.conn, sqlWrkStn, s.correlationID, false, false)
	if err != nil {
		return err
	}

	// RDBCMM (Commit setup)
	rdbcmm := PackRDBCMM()
	s.correlationID, err = WriteRequestDSS(s.conn, rdbcmm, s.correlationID, false, true)
	if err != nil {
		return err
	}

	// Drain responses
	_, err = s.readReplyChain()
	return err
}

// parseACCSECRD reads and processes the ACCSECRD reply during security negotiation.
func (s *Session) parseACCSECRD() (uint16, []byte, error) {
	replies, err := s.readReplyChain()
	if err != nil {
		return 0, nil, err
	}

	var secmec uint16
	var sectkn []byte

	for _, r := range replies {
		if r.CodePoint == CodePointRDBNFNRM {
			return 0, nil, NewError(-1013, "42705", "relational database not found (RDBNFNRM)", ErrDatabaseNotFound)
		}
		if r.CodePoint == CodePointACCSECRD {
			sub, err := ParseDDMReply(r.Data)
			if err != nil {
				return 0, nil, err
			}
			if smBytes, ok := sub[CodePointSECMEC]; ok && len(smBytes) >= 2 {
				secmec = binary.BigEndian.Uint16(smBytes[:2])
			}
			if stBytes, ok := sub[CodePointSECTKN]; ok {
				sectkn = stBytes
			}
		}
	}

	return secmec, sectkn, nil
}

// readReplyChain reads all chained DSS reply packets until the end of the chain.
func (s *Session) readReplyChain() ([]ReplyPacket, error) {
	var packets []ReplyPacket
	chained := true

	for chained {
		hdr, cp, data, moreData, err := ReadDSS(s.conn)
		if err != nil {
			// A frame that failed to parse may be only partly consumed.
			s.broken.Store(true)
			return nil, err
		}
		packets = append(packets, ReplyPacket{
			Header:    hdr,
			CodePoint: cp,
			Data:      data,
			MoreData:  moreData,
		})
		chained = hdr.Chained
	}

	return packets, nil
}

// Ping checks if the database session is responsive without affecting active transactions.
func (s *Session) Ping(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1

	// In auto-commit mode (outside an explicit transaction), use RDBCMM as a safe,
	// lightweight ping that preserves RDB connection and package access state.
	// Inside an explicit transaction (!s.autoCommit), issue EXCSAT so that uncommitted user
	// transaction changes are never inadvertently committed (AC-8).
	if s.autoCommit {
		rdbcmm := PackRDBCMM()
		var err error
		s.correlationID, err = WriteRequestDSS(s.conn, rdbcmm, s.correlationID, false, true)
		if err != nil {
			s.closed = true
			s.broken.Store(true)
			if s.conn != nil {
				_ = s.conn.Close()
			}
			return fmt.Errorf("failed to send ping RDBCMM: %w", err)
		}

		replies, err := s.readReplyChain()
		if err != nil {
			s.closed = true
			s.broken.Store(true)
			if s.conn != nil {
				_ = s.conn.Close()
			}
			return fmt.Errorf("failed to read ping reply: %w", err)
		}
		for i, r := range replies {
			if r.CodePoint == CodePointSQLERRRM {
				return parseSQLERRRM(replies[i:], s.endian)
			}
		}
		return nil
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "go-db2-client"
	}

	excsat, err := PackEXCSAT("go-db2", hostname, "SQL12010", "go-db2", DefaultManagerLevels())
	if err != nil {
		return fmt.Errorf("failed to pack EXCSAT for ping: %w", err)
	}

	s.correlationID, err = WriteRequestDSS(s.conn, excsat, s.correlationID, false, true)
	if err != nil {
		s.closed = true
		s.broken.Store(true)
		if s.conn != nil {
			_ = s.conn.Close()
		}
		return fmt.Errorf("failed to send ping EXCSAT: %w", err)
	}

	// Read and validate EXCSATRD consuming entire chain
	replies, err := s.readReplyChain()
	if err != nil {
		s.closed = true
		s.broken.Store(true)
		if s.conn != nil {
			_ = s.conn.Close()
		}
		return fmt.Errorf("failed to read ping reply: %w", err)
	}
	if len(replies) == 0 || replies[0].CodePoint != CodePointEXCSATRD {
		s.closed = true
		s.broken.Store(true)
		if s.conn != nil {
			_ = s.conn.Close()
		}
		var cp CodePoint
		if len(replies) > 0 {
			cp = replies[0].CodePoint
		}
		return fmt.Errorf("unexpected ping reply codepoint: 0x%04X", cp)
	}

	return nil
}

// Commit commits the active transaction.
func (s *Session) Commit(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return ErrConnectionClosed
	}
	defer s.watch(ctx)()

	return s.commitLocked()
}

// commitLocked sends RDBCMM and reads its reply chain. Caller must hold s.mu.
func (s *Session) commitLocked() error {
	if s.closed || s.conn == nil {
		return ErrConnectionClosed
	}

	s.correlationID = 1
	rdbcmm := PackRDBCMM()
	var err error
	s.correlationID, err = WriteRequestDSS(s.conn, rdbcmm, s.correlationID, false, true)
	if err != nil {
		return err
	}

	return s.readTransactionReply()
}

// Rollback rolls back the active transaction.
func (s *Session) Rollback(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1
	rdbrllbck := PackRDBRLLBCK()
	var err error
	s.correlationID, err = WriteRequestDSS(s.conn, rdbrllbck, s.correlationID, false, true)
	if err != nil {
		return err
	}

	return s.readTransactionReply()
}

func (s *Session) readTransactionReply() error {
	replies, err := s.readReplyChain()
	if err != nil {
		return err
	}
	for i, reply := range replies {
		switch reply.CodePoint {
		case CodePointSQLERRRM:
			return parseSQLERRRM(replies[i:], s.endian)
		case CodePointSQLCARD:
			code, state, msg, _, err := ParseSQLCARD(reply.Data, s.endian)
			if err != nil {
				return err
			}
			if code < 0 {
				return NewError(code, state, msg, nil)
			}
		}
	}
	return nil
}

// ExecDirect executes a DDL or DML statement immediately without parameter binding.
func (s *Session) ExecDirect(ctx context.Context, sql string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return 0, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1
	var err error

	// 1. EXCSQLIMM (Execute Immediate)
	imm := PackEXCSQLIMM(s.pkgid, s.pkgcnstkn, ReservedDirectSec, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, imm, s.correlationID, true, false)
	if err != nil {
		return 0, fmt.Errorf("failed to write EXCSQLIMM: %w", err)
	}

	// 2. SQLSTT (SQL Statement Text)
	stt := PackSQLSTT(sql)
	if s.autoCommit {
		s.correlationID, err = WriteRequestDSS(s.conn, stt, s.correlationID, false, false)
		if err != nil {
			return 0, fmt.Errorf("failed to write SQLSTT: %w", err)
		}

		// 3. RDBCMM (Commit)
		cmm := PackRDBCMM()
		s.correlationID, err = WriteRequestDSS(s.conn, cmm, s.correlationID, false, true)
		if err != nil {
			return 0, fmt.Errorf("failed to write RDBCMM: %w", err)
		}
	} else {
		s.correlationID, err = WriteRequestDSS(s.conn, stt, s.correlationID, false, true)
		if err != nil {
			return 0, fmt.Errorf("failed to write SQLSTT: %w", err)
		}
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	var affectedRows int64
	for i, r := range replies {
		if r.CodePoint == CodePointSQLCARD {
			code, state, msg, rows, parseErr := ParseSQLCARD(r.Data, s.endian)
			if parseErr != nil {
				return 0, parseErr
			}
			if code < 0 {
				return 0, NewError(code, state, msg, nil)
			}
			affectedRows += rows
		}
		if r.CodePoint == CodePointSQLERRRM {
			return 0, parseSQLERRRM(replies[i:], s.endian)
		}
	}

	return affectedRows, nil
}

// QueryDirect prepares, describes and executes a query statement returning raw column headers and decoded row data.
// Optimization: Returns decoded rows directly as [][]driver.Value to avoid per-row slice re-allocation and copying in Conn/Stmt wrappers.
func (s *Session) QueryDirect(ctx context.Context, sql string) ([]ColumnDescription, [][]driver.Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return nil, nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1
	var err error

	// 1. PRPSQLSTT
	prp := PackPRPSQLSTT(s.pkgid, s.pkgcnstkn, ReservedDirectSec, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, prp, s.correlationID, true, false)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write PRPSQLSTT: %w", err)
	}

	// 2. SQLSTT
	stt := PackSQLSTT(sql)
	s.correlationID, err = WriteRequestDSS(s.conn, stt, s.correlationID, false, false)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write SQLSTT: %w", err)
	}

	// 3. OPNQRY
	opn := PackOPNQRY(s.pkgid, s.pkgcnstkn, ReservedDirectSec, s.cfg.Database, s.qryblksz)
	s.correlationID, err = WriteRequestDSS(s.conn, opn, s.correlationID, false, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write OPNQRY: %w", err)
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read query response: %w", err)
	}

	return s.fetchQuery(replies, nil, ReservedDirectSec)
}

// PrepareAndDescribe prepares a query/statement and retrieves its output columns and input parameter descriptors.
func (s *Session) PrepareAndDescribe(ctx context.Context, sql string, pkgsn uint16) ([]ColumnDescription, []ColumnDescription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return nil, nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1
	var err error

	// 1. PRPSQLSTT
	prp := PackPRPSQLSTT(s.pkgid, s.pkgcnstkn, pkgsn, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, prp, s.correlationID, true, false)
	if err != nil {
		return nil, nil, err
	}

	// 2. SQLSTT
	stt := PackSQLSTT(sql)
	s.correlationID, err = WriteRequestDSS(s.conn, stt, s.correlationID, false, false)
	if err != nil {
		return nil, nil, err
	}

	// 3. DSCSQLSTT
	dsc := PackDSCSQLSTT(s.pkgid, s.pkgcnstkn, pkgsn, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, dsc, s.correlationID, false, true)
	if err != nil {
		return nil, nil, err
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return nil, nil, err
	}

	var outputCols, paramCols []ColumnDescription
	for i, r := range replies {
		if r.CodePoint == CodePointSQLDARD {
			cols, err := ParseSQLDARD(r.Data, s.endian)
			if err != nil {
				return nil, nil, err
			}
			if len(r.Data) > 0 && r.Data[0] == 0xFF {
				paramCols = cols
			} else {
				outputCols = cols
			}
		} else if r.CodePoint == CodePointSQLCARD {
			code, state, msg, _, parseErr := ParseSQLCARD(r.Data, s.endian)
			if parseErr != nil {
				return nil, nil, parseErr
			}
			if code < 0 {
				return nil, nil, NewError(code, state, msg, nil)
			}
		} else if r.CodePoint == CodePointSQLERRRM {
			return nil, nil, parseSQLERRRM(replies[i:], s.endian)
		}
	}

	return outputCols, paramCols, nil
}

// ExecWithParams executes a prepared statement with parameter arguments and returns affected rows and any output parameters.
func (s *Session) ExecWithParams(ctx context.Context, pkgsn uint16, paramCols []ColumnDescription, args []any) (int64, []any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return 0, nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	colTypes := make([]codec.SQLType, len(paramCols))
	colLens := make([]int64, len(paramCols))
	precs := make([]int, len(paramCols))
	scales := make([]int, len(paramCols))

	for i, c := range paramCols {
		colTypes[i] = codec.SQLType(c.SQLType)
		colLens[i] = c.Length
		precs[i] = c.Precision
		scales[i] = c.Scale
	}

	sqldta, extdtaList, err := codec.BuildSQLDTAWithEXTDTA(colTypes, colLens, precs, scales, args, s.endian)
	if err != nil {
		return 0, nil, err
	}

	// 1. EXCSQLSTT
	exc := PackEXCSQLSTT(s.pkgid, s.pkgcnstkn, pkgsn, s.cfg.Database)
	curID, err := WriteRequestDSS(s.conn, exc, 1, true, false)
	if err != nil {
		return 0, nil, err
	}

	// 2. SQLDTA
	hasMore := len(extdtaList) > 0 || s.autoCommit
	curID, err = WriteRequestDSS(s.conn, sqldta, curID, len(extdtaList) > 0, !hasMore)
	if err != nil {
		return 0, nil, err
	}

	// 3. EXTDTA
	for i, extObj := range extdtaList {
		isLast := (i == len(extdtaList)-1) && !s.autoCommit
		curID, err = WriteRequestDSS(s.conn, extObj, curID, i < len(extdtaList)-1, isLast)
		if err != nil {
			return 0, nil, err
		}
	}

	// 4. RDBCMM if autoCommit
	if s.autoCommit {
		cmm := PackRDBCMM()
		_, err = WriteRequestDSS(s.conn, cmm, 1, false, true)
		if err != nil {
			return 0, nil, err
		}
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return 0, nil, err
	}

	// If the server sent SQLDARD in the first chain, a second chain follows with SQLDTARD / SQLCARD
	hasOnlyDARD := false
	for _, r := range replies {
		if r.CodePoint == CodePointSQLDARD {
			hasOnlyDARD = true
			break
		}
	}
	if hasOnlyDARD {
		replies2, err := s.readReplyChain()
		if err == nil {
			replies = append(replies, replies2...)
		}
	}

	var affectedRows int64
	var outValues []any
	for i, r := range replies {
		if r.CodePoint == CodePointSQLDTARD {
			vals, parseErr := codec.ParseSQLDTARD(r.Data, s.endian)
			if parseErr == nil && len(vals) > 0 {
				outValues = vals
			}
		} else if r.CodePoint == CodePointSQLCARD {
			code, state, msg, rows, parseErr := ParseSQLCARD(r.Data, s.endian)
			if parseErr != nil {
				return 0, nil, parseErr
			}
			if code < 0 {
				return 0, nil, NewError(code, state, msg, nil)
			}
			affectedRows += rows
		} else if r.CodePoint == CodePointSQLERRRM {
			return 0, nil, parseSQLERRRM(replies[i:], s.endian)
		}
	}

	return affectedRows, outValues, nil
}

// ExecBatchWithParams executes a batch of parameter sets atomically against the currently prepared statement.
func (s *Session) ExecBatchWithParams(ctx context.Context, pkgsn uint16, paramCols []ColumnDescription, batchRows [][]any) (int64, error) {
	if len(batchRows) == 0 {
		return 0, nil
	}

	wasAutoCommit := s.AutoCommit()
	if wasAutoCommit {
		s.SetAutoCommit(false)
		defer s.SetAutoCommit(true)
	}

	var totalAffected int64
	for i, rowArgs := range batchRows {
		affected, _, err := s.ExecWithParams(ctx, pkgsn, paramCols, rowArgs)
		if err != nil {
			_ = s.Rollback(ctx)
			return 0, fmt.Errorf("db2: batch execution failed at row %d (rolled back): %w", i+1, err)
		}
		totalAffected += affected
	}

	if wasAutoCommit {
		if err := s.Commit(ctx); err != nil {
			_ = s.Rollback(ctx)
			return 0, fmt.Errorf("db2: failed to commit batch execution: %w", err)
		}
	}

	return totalAffected, nil
}

// QueryWithParams opens a query on a prepared statement with parameter arguments.
// Optimization: Returns decoded rows directly as [][]driver.Value to avoid per-row slice re-allocation and copying in Conn/Stmt wrappers.
func (s *Session) QueryWithParams(ctx context.Context, pkgsn uint16, outputCols, paramCols []ColumnDescription, args []any) ([]ColumnDescription, [][]driver.Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return nil, nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	colTypes := make([]codec.SQLType, len(paramCols))
	colLens := make([]int64, len(paramCols))
	precs := make([]int, len(paramCols))
	scales := make([]int, len(paramCols))

	for i, c := range paramCols {
		colTypes[i] = codec.SQLType(c.SQLType)
		colLens[i] = c.Length
		precs[i] = c.Precision
		scales[i] = c.Scale
	}

	sqldta, extdtaList, err := codec.BuildSQLDTAWithEXTDTA(colTypes, colLens, precs, scales, args, s.endian)
	if err != nil {
		return nil, nil, err
	}

	// 1. OPNQRY with QRYBLKSZ
	opnqry := PackOPNQRY(s.pkgid, s.pkgcnstkn, pkgsn, s.cfg.Database, s.qryblksz)
	curID, err := WriteRequestDSS(s.conn, opnqry, 1, true, false)
	if err != nil {
		return nil, nil, err
	}

	// 2. SQLDTA with parameters
	hasMore := len(extdtaList) > 0
	curID, err = WriteRequestDSS(s.conn, sqldta, curID, hasMore, !hasMore)
	if err != nil {
		return nil, nil, err
	}

	for i, extObj := range extdtaList {
		isLast := (i == len(extdtaList)-1)
		curID, err = WriteRequestDSS(s.conn, extObj, curID, !isLast, isLast)
		if err != nil {
			return nil, nil, err
		}
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return nil, nil, err
	}

	columns, rows, err := s.fetchQuery(replies, outputCols, pkgsn)
	if err != nil {
		return nil, nil, err
	}

	if s.autoCommit {
		// Send commit to close query lock
		s.correlationID = 1
		s.correlationID, err = WriteRequestDSS(s.conn, PackRDBCMM(), s.correlationID, false, true)
		if err == nil {
			_, _ = s.readReplyChain()
		}
	}

	return columns, rows, nil
}

// queryResult accumulates the replies to one query: its descriptors, the row
// data of every query block and the externalized (EXTDTA) values.
type queryResult struct {
	columns []ColumnDescription
	fields  []FieldDescriptor
	// rowData is the QRYDTA of every block, concatenated: a row may start in
	// one block and end in the next.
	rowData  bytes.Buffer
	extdta   [][]byte
	open     bool // OPNQRYRM seen: the cursor is open on the server
	ended    bool // ENDQRYRM or SQLCODE +100 seen: the server closed it
	qryinsid uint64
	cntqryID uint16
}

// consume records one reply chain. It reports whether the chain carried any
// query data, and returns the first error the server reported in it.
func (q *queryResult) consume(replies []ReplyPacket, endian binary.ByteOrder) (bool, error) {
	progress := false
	for i, r := range replies {
		switch r.CodePoint {
		case CodePointSQLDARD:
			cols, err := ParseSQLDARD(r.Data, endian)
			if err != nil {
				return progress, err
			}
			if len(cols) > 0 {
				q.columns = cols
			}
		case CodePointQRYDSC:
			flds, err := ParseQRYDSC(r.Data)
			if err != nil {
				return progress, err
			}
			q.fields = flds
		case CodePointQRYDTA:
			q.rowData.Write(r.Data)
			progress = true
		case CodePointEXTDTA:
			q.extdta = append(q.extdta, r.Data)
			progress = true
		case CodePointOPNQRYRM:
			sub, _ := ParseDDMReply(r.Data)
			if insidBytes, ok := sub[CodePointQRYINSID]; ok && len(insidBytes) >= 8 {
				q.qryinsid = binary.BigEndian.Uint64(insidBytes)
			}
			q.open = true
			q.cntqryID = r.Header.CorrelationID
		case CodePointENDQRYRM:
			q.ended = true
		case CodePointSQLERRRM:
			return progress, parseSQLERRRM(replies[i:], endian)
		case CodePointSQLCARD:
			code, state, msg, _, err := ParseSQLCARD(r.Data, endian)
			if err != nil {
				return progress, err
			}
			if code < 0 {
				return progress, NewError(code, state, msg, nil)
			}
			if code == 100 {
				q.ended = true
			}
		}
	}
	return progress, nil
}

// fetchQuery reads a query's result from the replies to OPNQRY, continuing
// the query with CNTQRY, one query block at a time, until the server ends it.
func (s *Session) fetchQuery(replies []ReplyPacket, columns []ColumnDescription, pkgsn uint16) ([]ColumnDescription, [][]driver.Value, error) {
	q := &queryResult{columns: columns, cntqryID: 1}
	if _, err := q.consume(replies, s.endian); err != nil {
		return nil, nil, err
	}

	for q.open && !q.ended {
		cntqry := PackCNTQRY(s.pkgid, s.pkgcnstkn, pkgsn, s.cfg.Database, s.qryblksz, q.qryinsid)
		if _, err := WriteRequestDSS(s.conn, cntqry, q.cntqryID, false, true); err != nil {
			return nil, nil, fmt.Errorf("failed to write CNTQRY: %w", err)
		}
		cntReplies, err := s.readReplyChain()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read CNTQRY response: %w", err)
		}
		progress, err := q.consume(cntReplies, s.endian)
		if err != nil {
			return nil, nil, err
		}
		if !progress && !q.ended {
			return nil, nil, errors.New("db2: CNTQRY returned no query data and did not end the query")
		}
	}

	rows, err := decodeRows(q.fields, q.rowData.Bytes(), s.endian)
	if err != nil {
		return nil, nil, err
	}
	if err := stitchEXTDTA(q.fields, rows, q.extdta); err != nil {
		return nil, nil, err
	}

	if len(q.columns) == 0 && len(q.fields) > 0 {
		q.columns = make([]ColumnDescription, len(q.fields))
		for i, f := range q.fields {
			q.columns[i] = ColumnDescription{
				Name:    fmt.Sprintf("COL%d", i+1),
				SQLType: uint16(f.Type),
			}
		}
	}
	if len(q.columns) == 0 && len(q.fields) > 0 {
		q.columns = make([]ColumnDescription, len(q.fields))
		for i, f := range q.fields {
			q.columns[i] = ColumnDescription{
				Name:    fmt.Sprintf("COL%d", i+1),
				SQLType: uint16(f.Type),
			}
		}
	}
	return q.columns, rows, nil
}

// decodeRows decodes QRYDTA row data. Each row starts with its SQLCA, which is
// the null indicator 0xFF unless the server attached a warning or an error to
// the row, then the indicator of the row data (0x00 when present), then one
// value per field.
// Optimization: Decodes rows directly as driver.Value slices and allocates a single contiguous backing slice for row buffers to eliminate per-row heap slice allocations.
func decodeRows(fields []FieldDescriptor, data []byte, endian binary.ByteOrder) ([][]driver.Value, error) {
	reader := bytes.NewReader(data)
	numFields := len(fields)

	// Pre-allocate rows capacity based on data length and field count to minimize slice reallocations.
	initCap := 0
	if minRowLen := 2 + numFields; minRowLen > 0 && len(data) >= minRowLen {
		initCap = len(data) / minRowLen
		if initCap > 1024 {
			initCap = 1024
		}
	}
	rows := make([][]driver.Value, 0, initCap)

	// Optimization: Allocate a single contiguous backing slice for all row buffers when field count and initial capacity are known.
	// Sub-slicing row buffers from backing eliminates N individual row slice heap allocations per query result set.
	var backing []driver.Value
	if initCap > 0 && numFields > 0 {
		backing = make([]driver.Value, initCap*numFields)
	}

	for reader.Len() >= 2 {
		first, err := reader.ReadByte()
		if err != nil {
			break
		}
		if first == 0xFF {
			// Fast-path: Null SQLCA indicator (normal row without server warnings or errors)
			dataInd, err := reader.ReadByte()
			if err != nil || dataInd != 0x00 {
				break
			}
		} else if first == 0x00 {
			// Non-null SQLCA indicator present (row has attached warning or error)
			_ = reader.UnreadByte()
			code, state, err := readRowSQLCA(reader, endian)
			if err != nil {
				return nil, fmt.Errorf("failed to read the SQLCA of row %d: %w", len(rows)+1, err)
			}
			if code < 0 {
				return nil, NewError(code, state, fmt.Sprintf("in row %d", len(rows)+1), nil)
			}
			dataInd, err := reader.ReadByte()
			if err != nil || dataInd != 0x00 {
				break
			}
		} else {
			break
		}

		var row []driver.Value
		if len(backing) >= numFields {
			row, backing = backing[:numFields:numFields], backing[numFields:]
		} else {
			row = make([]driver.Value, numFields)
		}

		for i := 0; i < numFields; i++ {
			f := &fields[i]
			val, err := codec.DecodeField(f.Type, f.PS[:], reader, endian)
			if err != nil {
				return nil, fmt.Errorf("failed to decode row %d column %d (Type=0x%02X, PS=%x, RemainingBytes=%d): %w",
					len(rows)+1, i+1, f.Type, f.PS[:], reader.Len(), err)
			}
			row[i] = val
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// readRowSQLCA reads the SQLCA group a QRYDTA row starts with and returns its
// SQLCODE and SQLSTATE; a null SQLCA (0xFF) is SQLCODE 0. The layout is the
// SQLCAGRP of DRDA: SQLCODE, SQLSTATE, SQLERRPROC, then the SQLCAXGRP (SQLERRD,
// SQLWARN, the RDB name and two message texts) and the SQLDIAGGRP, each behind
// its own null indicator.
func readRowSQLCA(r *bytes.Reader, endian binary.ByteOrder) (int32, string, error) {
	ind, err := r.ReadByte()
	if err != nil {
		return 0, "", err
	}
	if ind == 0xFF {
		return 0, "", nil
	}
	var head [4 + 5 + 8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, "", err
	}
	code := int32(endian.Uint32(head[0:4]))
	state := string(head[4:9])

	xInd, err := r.ReadByte()
	if err != nil {
		return 0, "", err
	}
	if xInd != 0xFF {
		if _, err := r.Seek(24+11, io.SeekCurrent); err != nil { // SQLERRD1-6, SQLWARN0-A
			return 0, "", err
		}
		for range 3 { // SQLRDBNAME, SQLERRMSG_m, SQLERRMSG_s
			var ln [2]byte
			if _, err := io.ReadFull(r, ln[:]); err != nil {
				return 0, "", err
			}
			if _, err := r.Seek(int64(binary.BigEndian.Uint16(ln[:])), io.SeekCurrent); err != nil {
				return 0, "", err
			}
		}
	}
	diagInd, err := r.ReadByte()
	if err != nil {
		return 0, "", err
	}
	if diagInd != 0xFF {
		return 0, "", fmt.Errorf("SQLCODE=%d SQLSTATE=%s carries an SQLDIAGGRP, which is not supported", code, state)
	}
	return code, state, nil
}

func isLOBType(t uint8) bool {
	switch t {
	case codec.DRDATypeLOBLOC, codec.DRDATypeNLOBLOC,
		codec.DRDATypeCLOBLOC, codec.DRDATypeNCLOBLOC,
		codec.DRDATypeDBCSCLOBLOC, codec.DRDATypeNDBCSCLOBLOC,
		codec.DRDATypeLOBBytes, codec.DRDATypeNLOBBytes,
		codec.DRDATypeLOBCMIXED, codec.DRDATypeNLOBCMIXED, codec.DRDATypeLOBCDBCS,
		codec.DRDATypeLOBCSBCS, codec.DRDATypeNLOBCSBCS,
		codec.DRDATypeXML, codec.DRDATypeNXML,
		0x10, 0x11, 0xCD, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9:
		return true
	default:
		return false
	}
}

func isCLOBType(t uint8) bool {
	switch t {
	case codec.DRDATypeLOBCMIXED, codec.DRDATypeNLOBCMIXED, codec.DRDATypeLOBCDBCS:
		return true
	case codec.DRDATypeCLOBLOC, codec.DRDATypeNCLOBLOC,
		codec.DRDATypeDBCSCLOBLOC, codec.DRDATypeNDBCSCLOBLOC,
		codec.DRDATypeLOBCSBCS, codec.DRDATypeNLOBCSBCS,
		codec.DRDATypeXML, codec.DRDATypeNXML,
		0xCD, 0xF6, 0xF7, 0xF8, 0xF9:
		return true
	default:
		return false
	}
}

func isDBCLOBType(t uint8) bool {
	switch t {
	case codec.DRDATypeLOBCDBCS:
		return true
	case codec.DRDATypeDBCSCLOBLOC, codec.DRDATypeNDBCSCLOBLOC,
		0xCD, 0xF8, 0xF9:
		return true
	default:
		return false
	}
}

func stitchEXTDTA(fields []FieldDescriptor, rows [][]driver.Value, extdtaList [][]byte) error {
	_, _, _, err := stitchReadyRows(fields, rows, extdtaList, true)
	return err
}

func stitchReadyRows(fields []FieldDescriptor, rows [][]driver.Value, extdtaList [][]byte, ended bool) ([][]driver.Value, [][]driver.Value, [][]byte, error) {
	external := 0
	for rowIndex, row := range rows {
		needed := 0
		for _, value := range row {
			if _, ok := value.(codec.ExternalLOB); ok {
				needed++
			}
		}
		if external+needed > len(extdtaList) {
			if ended {
				return nil, nil, nil, errors.New("db2: incomplete external LOB data")
			}
			return rows[:rowIndex], rows[rowIndex:], extdtaList[external:], nil
		}
		for column, value := range row {
			if _, ok := value.(codec.ExternalLOB); !ok {
				continue
			}
			data := extdtaList[external]
			external++
			// Only nullable columns carry a leading EXTDTA null indicator.
			// A non-nullable BLOB can legitimately start with a zero byte.
			if codec.IsNullableDRDAType(fields[column].Type) {
				if len(data) == 0 || data[0] != 0 {
					return nil, nil, nil, errors.New("db2: invalid external LOB null indicator")
				}
				data = data[1:]
			}
			switch {
			case isDBCLOBType(fields[column].Type):
				if len(data)%2 != 0 {
					return nil, nil, nil, errors.New("db2: incomplete DBCLOB code unit")
				}
				row[column] = codec.DecodeUTF16BE(data)
			case isCLOBType(fields[column].Type):
				row[column] = string(data)
			default:
				row[column] = data
			}
		}
	}
	return rows, nil, extdtaList[external:], nil
}

// decodeRowsPartial decodes complete QRYDTA rows from data and returns rows, consumed bytes, and error.
// If data ends in an incomplete row (Row Spanning across QRYDTA blocks), it stops without error and reports consumed bytes.
func decodeRowsPartial(fields []FieldDescriptor, data []byte, endian binary.ByteOrder) ([][]driver.Value, int, error) {
	reader := bytes.NewReader(data)
	numFields := len(fields)
	consumed := 0

	initCap := 0
	if minRowLen := 2 + numFields; minRowLen > 0 && len(data) >= minRowLen {
		initCap = len(data) / minRowLen
		if initCap > 1024 {
			initCap = 1024
		}
	}
	rows := make([][]driver.Value, 0, initCap)

	for reader.Len() >= 2 {
		first, err := reader.ReadByte()
		if err != nil {
			break
		}

		if first == 0xFF {
			dataInd, err := reader.ReadByte()
			if err != nil || dataInd != 0x00 {
				break
			}
		} else if first == 0x00 {
			_ = reader.UnreadByte()
			code, state, err := readRowSQLCA(reader, endian)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					break // Incomplete row spanning
				}
				return nil, consumed, fmt.Errorf("failed to read the SQLCA of row %d: %w", len(rows)+1, err)
			}
			if code < 0 {
				return nil, consumed, NewError(code, state, fmt.Sprintf("in row %d", len(rows)+1), nil)
			}
			dataInd, err := reader.ReadByte()
			if err != nil || dataInd != 0x00 {
				break
			}
		} else {
			break
		}

		row := make([]driver.Value, numFields)
		rowComplete := true

		for i := 0; i < numFields; i++ {
			f := &fields[i]
			val, err := codec.DecodeField(f.Type, f.PS[:], reader, endian)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					rowComplete = false
					break
				}
				return nil, consumed, fmt.Errorf("failed to decode row %d column %d: %w", len(rows)+1, i+1, err)
			}
			row[i] = val
		}

		if !rowComplete {
			break
		}

		rows = append(rows, row)
		consumed = len(data) - reader.Len()
	}

	return rows, consumed, nil
}

// QueryStream streams query result sets on demand using DRDA CNTQRY / CLSQRY.
type QueryStream struct {
	session     *Session
	ctx         context.Context
	pkgsn       uint16
	columns     []ColumnDescription
	fields      []FieldDescriptor
	qryinsid    uint64
	cntqryID    uint16
	open        bool
	ended       bool
	committed   bool
	closed      bool
	tailBuf     []byte
	pendingRows [][]driver.Value
	pendingExt  [][]byte
}

// FetchNextBatch requests the next QRYDTA block via CNTQRY and decodes complete rows.
func (qs *QueryStream) FetchNextBatch(ctx context.Context) ([][]driver.Value, error) {
	if qs.closed {
		return nil, io.EOF
	}

	watchCtx := ctx
	if (watchCtx == nil || watchCtx == context.Background()) && qs.ctx != nil {
		watchCtx = qs.ctx
	}

	qs.session.mu.Lock()
	defer qs.session.mu.Unlock()
	if watchCtx != nil {
		defer qs.session.watch(watchCtx)()
	}

	return qs.fetchNextBatchLocked()
}

// fetchNextBatchLocked requests CNTQRY blocks until at least one complete row is ready or the query ends.
// Caller must hold qs.session.mu.
func (qs *QueryStream) fetchNextBatchLocked() ([][]driver.Value, error) {
	if qs.closed {
		return nil, io.EOF
	}

	for {
		if !qs.open || qs.ended {
			if len(qs.pendingRows) > 0 {
				ready, _, _, err := stitchReadyRows(qs.fields, qs.pendingRows, qs.pendingExt, true)
				if err != nil {
					return nil, err
				}
				qs.pendingRows = nil
				qs.pendingExt = nil
				if qs.session.autoCommit && !qs.committed && !qs.session.Broken() {
					_ = qs.session.commitLocked()
					qs.committed = true
				}
				if len(ready) > 0 {
					return ready, nil
				}
			}
			if qs.session.autoCommit && !qs.committed && !qs.session.Broken() {
				_ = qs.session.commitLocked()
				qs.committed = true
			}
			return nil, io.EOF
		}

		if qs.session.closed || qs.session.conn == nil || qs.session.Broken() {
			return nil, ErrConnectionClosed
		}

		cntqry := PackCNTQRY(qs.session.pkgid, qs.session.pkgcnstkn, qs.pkgsn, qs.session.cfg.Database, qs.session.qryblksz, qs.qryinsid)
		_, err := WriteRequestDSS(qs.session.conn, cntqry, qs.cntqryID, false, true)
		if err != nil {
			return nil, fmt.Errorf("failed to write CNTQRY: %w", err)
		}

		cntReplies, err := qs.session.readReplyChain()
		if err != nil {
			return nil, fmt.Errorf("failed to read CNTQRY response: %w", err)
		}

		var newQryDta bytes.Buffer
		var newExtdta [][]byte
		progress := false

		for i, r := range cntReplies {
			switch r.CodePoint {
			case CodePointQRYDTA:
				newQryDta.Write(r.Data)
				progress = true
			case CodePointEXTDTA:
				newExtdta = append(newExtdta, r.Data)
				progress = true
			case CodePointENDQRYRM:
				qs.ended = true
			case CodePointSQLERRRM:
				return nil, parseSQLERRRM(cntReplies[i:], qs.session.endian)
			case CodePointSQLCARD:
				code, state, msg, _, err := ParseSQLCARD(r.Data, qs.session.endian)
				if err != nil {
					return nil, err
				}
				if code < 0 {
					return nil, NewError(code, state, msg, nil)
				}
				if code == 100 {
					qs.ended = true
				}
			}
		}

		if !progress && !qs.ended {
			return nil, errors.New("db2: CNTQRY returned no query data and did not end the query")
		}

		// Concatenate previous tail with newly arrived QRYDTA
		combinedData := make([]byte, len(qs.tailBuf)+newQryDta.Len())
		copy(combinedData, qs.tailBuf)
		copy(combinedData[len(qs.tailBuf):], newQryDta.Bytes())

		decodedRows, consumed, err := decodeRowsPartial(qs.fields, combinedData, qs.session.endian)
		if err != nil {
			return nil, err
		}

		qs.tailBuf = append([]byte(nil), combinedData[consumed:]...)

		allRows := append(qs.pendingRows, decodedRows...)
		allExt := append(qs.pendingExt, newExtdta...)
		readyRows, pendingRows, unusedExt, err := stitchReadyRows(qs.fields, allRows, allExt, !qs.open || qs.ended)
		if err != nil {
			return nil, err
		}
		qs.pendingRows = pendingRows
		qs.pendingExt = unusedExt

		if (!qs.open || qs.ended) && len(qs.pendingRows) == 0 && qs.session.autoCommit && !qs.committed && !qs.session.Broken() {
			_ = qs.session.commitLocked()
			qs.committed = true
		}

		if len(readyRows) > 0 {
			return readyRows, nil
		}
	}
}

// Close explicitly closes the cursor via CLSQRY and commits if autocommit.
func (qs *QueryStream) Close(ctx context.Context) error {
	if qs.closed {
		return nil
	}
	qs.closed = true

	var closeErr error
	if qs.open && !qs.ended && !qs.session.Broken() {
		qs.session.mu.Lock()
		clsqry := PackCLSQRY(qs.session.pkgid, qs.session.pkgcnstkn, qs.pkgsn, qs.session.cfg.Database, qs.qryinsid)
		_, err := WriteRequestDSS(qs.session.conn, clsqry, qs.cntqryID, false, true)
		if err != nil {
			closeErr = err
		} else {
			_, _ = qs.session.readReplyChain()
			qs.ended = true
		}
		if qs.session.autoCommit && !qs.committed && !qs.session.Broken() {
			if err := qs.session.commitLocked(); err != nil && closeErr == nil {
				closeErr = err
			}
			qs.committed = true
		}
		qs.session.mu.Unlock()
	} else if qs.session.AutoCommit() && !qs.committed && !qs.session.Broken() {
		closeErr = qs.session.Commit(ctx)
		qs.committed = true
	}

	qs.tailBuf = nil
	qs.pendingRows = nil
	qs.pendingExt = nil
	return closeErr
}

// QueryStreamDirect executes an immediate query and returns columns, initial batch, and active QueryStream.
func (s *Session) QueryStreamDirect(ctx context.Context, sql string) ([]ColumnDescription, [][]driver.Value, *QueryStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return nil, nil, nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1
	var err error

	prp := PackPRPSQLSTT(s.pkgid, s.pkgcnstkn, ReservedDirectSec, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, prp, s.correlationID, true, false)
	if err != nil {
		return nil, nil, nil, err
	}

	stt := PackSQLSTT(sql)
	s.correlationID, err = WriteRequestDSS(s.conn, stt, s.correlationID, false, false)
	if err != nil {
		return nil, nil, nil, err
	}

	opn := PackOPNQRY(s.pkgid, s.pkgcnstkn, ReservedDirectSec, s.cfg.Database, s.qryblksz)
	s.correlationID, err = WriteRequestDSS(s.conn, opn, s.correlationID, false, true)
	if err != nil {
		return nil, nil, nil, err
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return nil, nil, nil, err
	}

	return s.initStreamFromReplies(ctx, replies, nil, ReservedDirectSec)
}

// QueryStreamWithParams opens a parameterized query and returns columns, initial batch, and active QueryStream.
func (s *Session) QueryStreamWithParams(ctx context.Context, pkgsn uint16, outputCols, paramCols []ColumnDescription, args []any) ([]ColumnDescription, [][]driver.Value, *QueryStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return nil, nil, nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	colTypes := make([]codec.SQLType, len(paramCols))
	colLens := make([]int64, len(paramCols))
	precs := make([]int, len(paramCols))
	scales := make([]int, len(paramCols))

	for i, c := range paramCols {
		colTypes[i] = codec.SQLType(c.SQLType)
		colLens[i] = c.Length
		precs[i] = c.Precision
		scales[i] = c.Scale
	}

	sqldta, extdtaList, err := codec.BuildSQLDTAWithEXTDTA(colTypes, colLens, precs, scales, args, s.endian)
	if err != nil {
		return nil, nil, nil, err
	}

	opnqry := PackOPNQRY(s.pkgid, s.pkgcnstkn, pkgsn, s.cfg.Database, s.qryblksz)
	curID, err := WriteRequestDSS(s.conn, opnqry, 1, true, false)
	if err != nil {
		return nil, nil, nil, err
	}

	hasMore := len(extdtaList) > 0
	curID, err = WriteRequestDSS(s.conn, sqldta, curID, hasMore, !hasMore)
	if err != nil {
		return nil, nil, nil, err
	}

	for i, extObj := range extdtaList {
		isLast := (i == len(extdtaList)-1)
		curID, err = WriteRequestDSS(s.conn, extObj, curID, !isLast, isLast)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return nil, nil, nil, err
	}

	return s.initStreamFromReplies(ctx, replies, outputCols, pkgsn)
}

func (s *Session) initStreamFromReplies(ctx context.Context, replies []ReplyPacket, outputCols []ColumnDescription, pkgsn uint16) ([]ColumnDescription, [][]driver.Value, *QueryStream, error) {
	q := &queryResult{columns: outputCols, cntqryID: 1}
	if _, err := q.consume(replies, s.endian); err != nil {
		return nil, nil, nil, err
	}

	decodedRows, consumed, err := decodeRowsPartial(q.fields, q.rowData.Bytes(), s.endian)
	if err != nil {
		return nil, nil, nil, err
	}

	readyRows, pendingRows, unusedExt, err := stitchReadyRows(q.fields, decodedRows, q.extdta, !q.open || q.ended)
	if err != nil {
		return nil, nil, nil, err
	}

	if len(q.columns) == 0 && len(q.fields) > 0 {
		q.columns = make([]ColumnDescription, len(q.fields))
		for i, f := range q.fields {
			q.columns[i] = ColumnDescription{
				Name:    fmt.Sprintf("COL%d", i+1),
				SQLType: uint16(f.Type),
			}
		}
	}

	tail := append([]byte(nil), q.rowData.Bytes()[consumed:]...)

	qs := &QueryStream{
		session:     s,
		ctx:         ctx,
		pkgsn:       pkgsn,
		columns:     q.columns,
		fields:      q.fields,
		qryinsid:    q.qryinsid,
		cntqryID:    q.cntqryID,
		open:        q.open,
		ended:       q.ended,
		tailBuf:     tail,
		pendingRows: pendingRows,
		pendingExt:  unusedExt,
		committed:   false,
	}

	if len(readyRows) == 0 && qs.open && !qs.ended {
		readyRows, err = qs.fetchNextBatchLocked()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, nil, nil, err
		}
	} else if !qs.open || qs.ended {
		if s.autoCommit && !qs.committed && !s.Broken() {
			_ = s.commitLocked()
			qs.committed = true
		}
	}

	return q.columns, readyRows, qs, nil
}

// CallResultSet holds column metadata and row values for a procedure result set.
type CallResultSet struct {
	Columns []ColumnDescription
	Data    [][]driver.Value
}

// QueryCallDirect executes an immediate CALL statement and decodes all dynamic result sets.
func (s *Session) QueryCallDirect(ctx context.Context, sql string) ([]CallResultSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1
	var err error

	imm := PackEXCSQLIMM(s.pkgid, s.pkgcnstkn, ReservedDirectSec, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, imm, s.correlationID, true, false)
	if err != nil {
		return nil, err
	}

	stt := PackSQLSTT(sql)
	if s.autoCommit {
		s.correlationID, err = WriteRequestDSS(s.conn, stt, s.correlationID, false, false)
		if err != nil {
			return nil, err
		}
		cmm := PackRDBCMM()
		s.correlationID, err = WriteRequestDSS(s.conn, cmm, s.correlationID, false, true)
		if err != nil {
			return nil, err
		}
	} else {
		s.correlationID, err = WriteRequestDSS(s.conn, stt, s.correlationID, false, true)
		if err != nil {
			return nil, err
		}
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return nil, err
	}

	if s.autoCommit {
		hasOnlyDARD := false
		for _, r := range replies {
			if r.CodePoint == CodePointSQLDARD {
				hasOnlyDARD = true
				break
			}
		}
		if hasOnlyDARD {
			replies2, err := s.readReplyChain()
			if err == nil {
				replies = append(replies, replies2...)
			}
		}
	}

	sets, _, err := s.parseCallResultSets(replies)
	return sets, err
}

// QueryCallWithParams executes a prepared CALL statement with parameters, returning result sets and OUT values.
func (s *Session) QueryCallWithParams(ctx context.Context, pkgsn uint16, paramCols []ColumnDescription, args []any) ([]CallResultSet, []any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return nil, nil, ErrConnectionClosed
	}
	defer s.watch(ctx)()

	colTypes := make([]codec.SQLType, len(paramCols))
	colLens := make([]int64, len(paramCols))
	precs := make([]int, len(paramCols))
	scales := make([]int, len(paramCols))

	for i, c := range paramCols {
		colTypes[i] = codec.SQLType(c.SQLType)
		colLens[i] = c.Length
		precs[i] = c.Precision
		scales[i] = c.Scale
	}

	sqldta, extdtaList, err := codec.BuildSQLDTAWithEXTDTA(colTypes, colLens, precs, scales, args, s.endian)
	if err != nil {
		return nil, nil, err
	}

	exc := PackEXCSQLSTT(s.pkgid, s.pkgcnstkn, pkgsn, s.cfg.Database)
	curID, err := WriteRequestDSS(s.conn, exc, 1, true, false)
	if err != nil {
		return nil, nil, err
	}

	hasMore := len(extdtaList) > 0 || s.autoCommit
	curID, err = WriteRequestDSS(s.conn, sqldta, curID, len(extdtaList) > 0, !hasMore)
	if err != nil {
		return nil, nil, err
	}

	for i, extObj := range extdtaList {
		isLast := (i == len(extdtaList)-1) && !s.autoCommit
		curID, err = WriteRequestDSS(s.conn, extObj, curID, i < len(extdtaList)-1, isLast)
		if err != nil {
			return nil, nil, err
		}
	}

	if s.autoCommit {
		cmm := PackRDBCMM()
		_, err = WriteRequestDSS(s.conn, cmm, 1, false, true)
		if err != nil {
			return nil, nil, err
		}
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return nil, nil, err
	}

	hasOnlyDARD := false
	for _, r := range replies {
		if r.CodePoint == CodePointSQLDARD {
			hasOnlyDARD = true
			break
		}
	}
	if hasOnlyDARD {
		replies2, err := s.readReplyChain()
		if err == nil {
			replies = append(replies, replies2...)
		}
	}

	return s.parseCallResultSets(replies)
}

func (s *Session) parseCallResultSets(replies []ReplyPacket) ([]CallResultSet, []any, error) {
	var sets []CallResultSet
	var outValues []any

	var currentCols []ColumnDescription
	var currentFields []FieldDescriptor
	var currentData bytes.Buffer
	var currentExt [][]byte

	flushSet := func() error {
		if len(currentFields) > 0 && currentData.Len() > 0 {
			rows, err := decodeRows(currentFields, currentData.Bytes(), s.endian)
			if err != nil {
				return err
			}
			if err := stitchEXTDTA(currentFields, rows, currentExt); err != nil {
				return err
			}
			if len(currentCols) == 0 && len(currentFields) > 0 {
				currentCols = make([]ColumnDescription, len(currentFields))
				for i, f := range currentFields {
					currentCols[i] = ColumnDescription{
						Name:    fmt.Sprintf("COL%d", i+1),
						SQLType: uint16(f.Type),
					}
				}
			}
			sets = append(sets, CallResultSet{Columns: currentCols, Data: rows})
		}
		currentFields = nil
		currentData.Reset()
		currentExt = nil
		currentCols = nil
		return nil
	}

	for i, r := range replies {
		switch r.CodePoint {
		case CodePointSQLDARD:
			cols, err := ParseSQLDARD(r.Data, s.endian)
			if err != nil {
				return nil, nil, err
			}
			if len(cols) > 0 {
				if len(currentFields) > 0 {
					if err := flushSet(); err != nil {
						return nil, nil, err
					}
				}
				currentCols = cols
			}
		case CodePointQRYDSC:
			flds, err := ParseQRYDSC(r.Data)
			if err != nil {
				return nil, nil, err
			}
			if len(currentFields) > 0 {
				if err := flushSet(); err != nil {
					return nil, nil, err
				}
			}
			currentFields = flds
		case CodePointQRYDTA:
			currentData.Write(r.Data)
		case CodePointEXTDTA:
			currentExt = append(currentExt, r.Data)
		case CodePointSQLDTARD:
			vals, err := codec.ParseSQLDTARD(r.Data, s.endian)
			if err == nil && len(vals) > 0 {
				outValues = vals
			}
		case CodePointSQLERRRM:
			return nil, nil, parseSQLERRRM(replies[i:], s.endian)
		case CodePointSQLCARD:
			code, state, msg, _, err := ParseSQLCARD(r.Data, s.endian)
			if err != nil {
				return nil, nil, err
			}
			if code < 0 {
				return nil, nil, NewError(code, state, msg, nil)
			}
		}
	}

	if err := flushSet(); err != nil {
		return nil, nil, err
	}

	return sets, outValues, nil
}

// CurrentUser returns the currently active user on the session.
func (s *Session) CurrentUser() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.cfg.User)
}

// quoteIdentifier safely quotes an SQL identifier with double quotes, escaping any embedded double quotes.
// Optimization: Uses strings.IndexByte segment jumping and block sb.WriteString appends to eliminate character-by-character loops and strings.Count pre-scans when escaping embedded double quotes.
func quoteIdentifier(name string) string {
	idx := strings.IndexByte(name, '"')
	if idx == -1 {
		return `"` + name + `"`
	}
	var sb strings.Builder
	sb.Grow(len(name) + 6)
	sb.WriteByte('"')
	sb.WriteString(name[:idx])
	sb.WriteString(`""`)
	rest := name[idx+1:]
	for {
		nextIdx := strings.IndexByte(rest, '"')
		if nextIdx == -1 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:nextIdx])
		sb.WriteString(`""`)
		rest = rest[nextIdx+1:]
	}
	sb.WriteByte('"')
	return sb.String()
}

// SwitchUser transitions the active user identity on the existing session.
// In Db2 Trusted Context and privileged sessions, identity is switched via
// 'SET SESSION AUTHORIZATION = ?' / 'SET SESSION_USER = ?' (when password is omitted)
// or by issuing DRDA SECCHK with USRID (and encrypted/plaintext password if provided).
func (s *Session) SwitchUser(ctx context.Context, newUser string, password ...string) error {
	trimmedUser := strings.TrimSpace(newUser)
	if trimmedUser == "" {
		return errors.New("db2: switch user name cannot be empty")
	}
	if !validate.IsValidSQLIdentifier(trimmedUser) {
		return fmt.Errorf("db2: invalid switch user identifier %q", newUser)
	}

	hasPassword := len(password) > 0 && password[0] != ""

	// AC-3: If no password was provided, try SQL-level session authorization switch first.
	// If a password was provided, SQL commands cannot verify credentials, so we skip directly to DRDA SECCHK.
	if !hasPassword {
		quotedUser := quoteIdentifier(trimmedUser)

		// First try SQL-level session authorization switch
		setAuthSQL := fmt.Sprintf("SET SESSION AUTHORIZATION = %s", quotedUser)
		if _, err := s.ExecDirect(ctx, setAuthSQL); err == nil {
			s.mu.Lock()
			s.cfg.User = trimmedUser
			s.mu.Unlock()
			return nil
		}

		// Fallback to SET SESSION_USER
		setSessionUserSQL := fmt.Sprintf("SET SESSION_USER = %s", quotedUser)
		if _, err := s.ExecDirect(ctx, setSessionUserSQL); err == nil {
			s.mu.Lock()
			s.cfg.User = trimmedUser
			s.mu.Unlock()
			return nil
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return ErrConnectionClosed
	}
	defer s.watch(ctx)()

	var pwdBytes []byte
	if hasPassword {
		pwdBytes = []byte(password[0])
	}

	var secchk []byte
	var err error

	if s.secmec == SecMecEUSRIDPWD || s.secmec == 9 {
		// AC-4: Require DH token and encrypt password with SECMEC 9
		if len(s.secTkn) < 20 {
			return errors.New("db2: ACCSEC negotiation token is missing or invalid for SECMEC 9")
		}
		clientPriv, privErr := auth.GenerateDHPrivateKey()
		if privErr != nil {
			return fmt.Errorf("failed to generate DH private key: %w", privErr)
		}
		clientPub := auth.CalculateDHPublicKey(clientPriv)
		encPassword, encErr := auth.EncryptPasswordSECMEC9(string(pwdBytes), s.secTkn, clientPriv)
		if encErr != nil {
			return fmt.Errorf("failed to encrypt password with SECMEC 9: %w", encErr)
		}
		secchk, err = PackSECCHKWithBytes(s.secmec, clientPub, s.cfg.Database, trimmedUser, encPassword, true, s.encoding)
	} else if s.secmec == SecMecUSRIDPWD || s.secmec == 3 {
		// AC-6: Block plaintext password switch if unencrypted and not explicitly allowed
		if hasPassword && !s.cfg.UseSSL && !s.cfg.AllowPlaintextAuth {
			return errors.New("db2: plaintext authentication over unencrypted connection is disabled by default; set allow_plaintext_auth=true to override")
		}
		secchk, err = PackSECCHKWithBytes(s.secmec, nil, s.cfg.Database, trimmedUser, pwdBytes, false, s.encoding)
	} else {
		secchk, err = PackSECCHKWithBytes(s.secmec, nil, s.cfg.Database, trimmedUser, pwdBytes, false, s.encoding)
	}
	if err != nil {
		return fmt.Errorf("failed to pack SECCHK for user switch: %w", err)
	}

	s.correlationID = 1
	s.correlationID, err = WriteRequestDSS(s.conn, secchk, s.correlationID, false, true)
	if err != nil {
		return fmt.Errorf("failed to send SECCHK for user switch: %w", err)
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return fmt.Errorf("user switch response failed: %w", err)
	}

	// AC-5: Validate replies and check for failure codepoints (RDBAFLRM, RDBATHRM, SQLERRRM, or missing SECCHKRM / SECCHKCD != 0)
	var seenSECCHKRM bool
	for i, r := range replies {
		if r.CodePoint == CodePointRDBAFLRM || r.CodePoint == CodePointRDBATHRM {
			return errors.New("authorization failed connecting to relational database")
		}
		if r.CodePoint == CodePointSQLERRRM {
			return parseSQLERRRM(replies[i:], s.endian)
		}
		if r.CodePoint == CodePointSECCHKRM {
			seenSECCHKRM = true
			sub, _ := ParseDDMReply(r.Data)
			if secChkCd, ok := sub[CodePointSECCHKCD]; ok && len(secChkCd) > 0 && secChkCd[0] != 0 {
				return NewError(-30082, "08001", fmt.Sprintf("user switch failed: SECCHKCD=%d", secChkCd[0]), ErrAuthenticationFailed)
			}
		}
	}

	if !seenSECCHKRM {
		return NewError(-30082, "08001", "user switch failed: missing SECCHKRM reply", ErrAuthenticationFailed)
	}

	s.cfg.User = trimmedUser
	return nil
}

// ExecSQLSet executes special register statements (such as SET CLIENT APPLNAME) via DRDA EXCSQLSET.
func (s *Session) ExecSQLSet(ctx context.Context, sql string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return ErrConnectionClosed
	}
	defer s.watch(ctx)()

	s.correlationID = 1
	var err error

	excSqlSet := PackEXCSQLSET(s.pkgid, "", 1, s.cfg.Database)
	s.correlationID, err = WriteRequestDSS(s.conn, excSqlSet, s.correlationID, true, false)
	if err != nil {
		return fmt.Errorf("failed to write EXCSQLSET: %w", err)
	}

	sqlStt := PackSQLSTT(sql)
	if s.autoCommit {
		s.correlationID, err = WriteRequestDSS(s.conn, sqlStt, s.correlationID, false, false)
		if err != nil {
			return fmt.Errorf("failed to write SQLSTT: %w", err)
		}

		rdbcmm := PackRDBCMM()
		s.correlationID, err = WriteRequestDSS(s.conn, rdbcmm, s.correlationID, false, true)
		if err != nil {
			return fmt.Errorf("failed to write RDBCMM: %w", err)
		}
	} else {
		s.correlationID, err = WriteRequestDSS(s.conn, sqlStt, s.correlationID, false, true)
		if err != nil {
			return fmt.Errorf("failed to write SQLSTT: %w", err)
		}
	}

	replies, err := s.readReplyChain()
	if err != nil {
		return fmt.Errorf("failed to read reply chain for EXCSQLSET: %w", err)
	}

	for _, r := range replies {
		if r.CodePoint == CodePointSQLCARD {
			code, state, msg, _, parseErr := ParseSQLCARD(r.Data, s.endian)
			if parseErr != nil {
				return parseErr
			}
			if code < 0 {
				return NewError(code, state, msg, nil)
			}
		}
	}

	return nil
}

// ClientInfoMatches returns true if the current session registers match the given values.
func (s *Session) ClientInfoMatches(applName, wrkstnName, userid, acctng, corrToken string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.ClientApplName == applName &&
		s.cfg.ClientWrkstnName == wrkstnName &&
		s.cfg.ClientUserid == userid &&
		s.cfg.ClientAcctng == acctng &&
		s.cfg.ClientCorrToken == corrToken
}

// ResetClientInfo restores all client information special registers back to the given base configuration.
func (s *Session) ResetClientInfo(ctx context.Context, applName, wrkstnName, userid, acctng, corrToken string) error {
	applName = sanitizeClientRegister(applName)
	wrkstnName = sanitizeClientRegister(wrkstnName)
	userid = sanitizeClientRegister(userid)
	acctng = sanitizeClientRegister(acctng)
	corrToken = sanitizeClientRegister(corrToken)

	s.mu.Lock()
	prevAppl := s.cfg.ClientApplName
	prevWrkstn := s.cfg.ClientWrkstnName
	prevUserid := s.cfg.ClientUserid
	prevAcctng := s.cfg.ClientAcctng
	prevCorr := s.cfg.ClientCorrToken
	s.mu.Unlock()

	if applName != prevAppl {
		if err := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT APPLNAME '%s'", escapeSingleQuotes(applName))); err != nil {
			return err
		}
	}
	if wrkstnName != prevWrkstn {
		if err := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT WRKSTNNAME '%s'", escapeSingleQuotes(wrkstnName))); err != nil {
			return err
		}
	}
	if userid != prevUserid {
		if err := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT USERID '%s'", escapeSingleQuotes(userid))); err != nil {
			return err
		}
	}
	if acctng != prevAcctng {
		if err := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT ACCTNG '%s'", escapeSingleQuotes(acctng))); err != nil {
			if err2 := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT_ACCTNG = '%s'", escapeSingleQuotes(acctng))); err2 != nil {
				if err3 := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT ACCTSTR '%s'", escapeSingleQuotes(acctng))); err3 != nil {
					return err3
				}
			}
		}
	}
	if corrToken != prevCorr {
		if err := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT CORR_TOKEN '%s'", escapeSingleQuotes(corrToken))); err != nil {
			if err2 := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT_CORR_TOKEN = '%s'", escapeSingleQuotes(corrToken))); err2 != nil {
				if err3 := s.ExecSQLSet(ctx, fmt.Sprintf("SET CLIENT PROGRAMID '%s'", escapeSingleQuotes(corrToken))); err3 != nil {
					return err3
				}
			}
		}
	}

	s.mu.Lock()
	s.cfg.ClientApplName = applName
	s.cfg.ClientWrkstnName = wrkstnName
	s.cfg.ClientUserid = userid
	s.cfg.ClientAcctng = acctng
	s.cfg.ClientCorrToken = corrToken
	s.mu.Unlock()

	return nil
}

// SetClientInfo updates specified client information registers on the session. Empty fields in args leave existing registers unchanged.
func (s *Session) SetClientInfo(ctx context.Context, applName, wrkstnName, userid, acctng, corrToken string) error {
	s.mu.Lock()
	targetAppl := s.cfg.ClientApplName
	targetWrkstn := s.cfg.ClientWrkstnName
	targetUserid := s.cfg.ClientUserid
	targetAcctng := s.cfg.ClientAcctng
	targetCorr := s.cfg.ClientCorrToken
	s.mu.Unlock()

	if applName != "" {
		targetAppl = applName
	}
	if wrkstnName != "" {
		targetWrkstn = wrkstnName
	}
	if userid != "" {
		targetUserid = userid
	}
	if acctng != "" {
		targetAcctng = acctng
	}
	if corrToken != "" {
		targetCorr = corrToken
	}

	return s.ResetClientInfo(ctx, targetAppl, targetWrkstn, targetUserid, targetAcctng, targetCorr)
}

func sanitizeClientRegister(val string) string {
	idx := strings.IndexAny(val, "\x00\r\n")
	if idx == -1 {
		return val
	}
	var sb strings.Builder
	sb.Grow(len(val))
	sb.WriteString(val[:idx])
	for i := idx; i < len(val); i++ {
		ch := val[i]
		if ch != '\x00' && ch != '\r' && ch != '\n' {
			sb.WriteByte(ch)
		}
	}
	return sb.String()
}

// escapeSingleQuotes sanitizes and escapes single quote characters for SQL literal strings.
// Optimization: Fast-path using strings.IndexAny returns clean strings immediately (0 allocs) and uses a single pre-allocated strings.Builder pass for escaping/sanitizing modified strings.
func escapeSingleQuotes(val string) string {
	idx := strings.IndexAny(val, "\x00\r\n'")
	if idx == -1 {
		return val
	}
	var sb strings.Builder
	sb.Grow(len(val) + 8)
	sb.WriteString(val[:idx])
	for i := idx; i < len(val); i++ {
		ch := val[i]
		switch ch {
		case '\x00', '\r', '\n':
			// skip control characters
		case '\'':
			sb.WriteString("''")
		default:
			sb.WriteByte(ch)
		}
	}
	return sb.String()
}

// Interrupt requests cancellation of any active statement on the session via SQLINTR.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return ErrConnectionClosed
	}

	intr := PackSQLINTR(s.pkgid, s.pkgcnstkn, s.pkgsn, s.cfg.Database)
	_, err := WriteRequestDSS(s.conn, intr, 1, false, true)
	return err
}

// Close gracefully closes the session and underlying network connection.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}
	return nil
}

// decodeSRVDGN decodes Server Diagnostic Information (SRVDGN) from EBCDIC CP500 to a UTF-8 string.
func decodeSRVDGN(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return strings.TrimSpace(codec.DecodeCP500(data))
}

// SQLERRRM reports severity; its SQLCARD supplies SQLCODE, SQLSTATE and tokens.
// The card can follow type definitions within the same command's reply chain.
func parseSQLERRRM(replies []ReplyPacket, endian binary.ByteOrder) error {
	sub, _ := ParseDDMReply(replies[0].Data)
	cardData := sub[CodePointSQLCARD]
	if len(cardData) == 0 {
		for i, next := range replies[1:] {
			prev := replies[i].Header
			if prev == nil || !prev.Chained || next.Header == nil || next.Header.CorrelationID != prev.CorrelationID {
				break
			}
			if next.CodePoint == CodePointTYPDEFNAM || next.CodePoint == CodePointTYPDEFOVR {
				continue
			}
			if next.CodePoint == CodePointSQLCARD {
				cardData = next.Data
			}
			break
		}
	}
	if len(cardData) > 0 {
		code, state, msg, _, err := ParseSQLCARD(cardData, endian)
		if err == nil && code < 0 {
			if srvDgn := decodeSRVDGN(sub[CodePointSRVDGN]); srvDgn != "" {
				return NewError(code, state, fmt.Sprintf("%s (SRVDGN: %s)", msg, srvDgn), nil)
			}
			return NewError(code, state, msg, nil)
		}
	}
	if srvDgn := decodeSRVDGN(sub[CodePointSRVDGN]); srvDgn != "" {
		return fmt.Errorf("db2: %s", srvDgn)
	}
	return errors.New("db2: server error (SQLERRRM)")
}
