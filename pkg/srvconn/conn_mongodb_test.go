package srvconn

import (
	"bytes"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver/koko/pkg/localcommand"
)

func TestMongoDBUsesUSQLAndPreservesConnectionOptions(t *testing.T) {
	opt := &sqlOption{Host: "2001:db8::1", Port: 27017, Username: "user@tenant", Password: "secret-not-in-argv",
		DBName: "audit", AuthSource: "admin", ConnectionOptions: "replicaSet=rs0&readPreference=secondaryPreferred",
		UseSSL: true, CaCertPath: "/tmp/ca.pem", CertKeyPath: "/tmp/client.pem", AllowInvalidCert: true}
	args := opt.MongoDBCommandArgs()
	if len(args) < 2 || args[0] != "--mongo" {
		t.Fatal("MongoDB must select usql Mongo Shell mode")
	}
	u, err := url.Parse(args[1])
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "mongodb" || u.Host != "[2001:db8::1]:27017" || u.Path != "/audit" || u.User != nil || q.Get("authSource") != "admin" || q.Get("replicaSet") != "rs0" || q.Get("readPreference") != "secondaryPreferred" || q.Get("tlsCAFile") != opt.CaCertPath || q.Get("tlsCertificateKeyFile") != opt.CertKeyPath || q.Get("tlsInsecure") != "true" {
		t.Fatal("MongoDB connection options were not preserved")
	}
	if !slices.Contains(args, "--username") || !slices.Contains(args, opt.Username) || strings.Contains(strings.Join(args, " "), opt.Password) {
		t.Fatal("username or PTY password handling changed")
	}
}

func TestLiveMongoDBUSQLPTY(t *testing.T) {
	uri, binary := os.Getenv("KOKO_TEST_MONGODB_URI"), os.Getenv("KOKO_TEST_USQL_BIN")
	if uri == "" || binary == "" {
		t.Skip("set KOKO_TEST_MONGODB_URI and KOKO_TEST_USQL_BIN for the disposable MongoDB PTY test")
	}
	u, err := url.Parse(uri)
	if err != nil || u.User == nil {
		t.Fatal("test MongoDB URI must include credentials")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	opt := &sqlOption{Host: u.Hostname(), Port: port, DBName: strings.TrimPrefix(u.Path, "/"), Username: u.User.Username(), Password: password, AuthSource: u.Query().Get("authSource")}
	if err := checkMongoDBAccount(opt); err != nil {
		t.Fatal(err)
	}
	command, err := localcommand.New(binary, opt.MongoDBCommandArgs(), localcommand.WithPtyWin(80, 24))
	if err != nil {
		t.Fatal(err)
	}
	defer command.Close()
	timer := time.AfterFunc(10*time.Second, func() { _ = command.Close() })
	defer timer.Stop()
	if _, err = MatchLoginPrefix(mongodbPrompt, "MongoDB", command); err != nil {
		t.Fatal(err)
	}
	if _, err = DoLogin(opt, command, "MongoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err = command.Write([]byte("print('KOKO_'+'MONGO_OK', db.runCommand({ping:1}).ok)\r\n")); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	buf := make([]byte, 4096)
	for output.Len() < 32768 {
		n, err := command.Read(buf)
		output.Write(buf[:n])
		if bytes.Contains(output.Bytes(), []byte("KOKO_MONGO_OK 1")) {
			if bytes.Contains(output.Bytes(), []byte(password)) {
				t.Fatal("PTY echoed the password")
			}
			return
		}
		if err != nil {
			t.Fatalf("MongoDB PTY command did not complete: %v", err)
		}
	}
	t.Fatal("MongoDB PTY output exceeded its test limit")
}
