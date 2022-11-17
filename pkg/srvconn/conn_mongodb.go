package srvconn

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/jumpserver/koko/pkg/localcommand"
)

const (
	mongodbPrompt = "Enter password:"
)

var (
	_ ServerConnection = (*MongoDBConn)(nil)
)

func NewMongoDBConnection(ops ...SqlOption) (*MongoDBConn, error) {
	var (
		lCmd *localcommand.LocalCommand
		err  error
	)
	args := &sqlOption{
		Username:         os.Getenv("USER"),
		Password:         os.Getenv("PASSWORD"),
		Host:             "127.0.0.1",
		Port:             27017,
		DBName:           "test",
		UseSSL:           false,
		CaCert:           "",
		CertKey:          "",
		AllowInvalidCert: false,
		win: Windows{
			Width:  80,
			Height: 120,
		},
	}
	for _, setter := range ops {
		setter(args)
	}

	if args.UseSSL {
		caCertPath, err := StoreCAFileToLocal(args.CaCert)
		if err != nil {
			return nil, err
		}
		certKeyPath, err := StoreCAFileToLocal(args.CertKey)
		if err != nil {
			return nil, err
		}
		args.CaCertPath = caCertPath
		args.CertKeyPath = certKeyPath
		defer ClearTempFileDelay(time.Minute, caCertPath, certKeyPath)
	}

	if err := checkMongoDBAccount(args); err != nil {
		return nil, err
	}
	lCmd, err = startMongoDBCommand(args)

	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &MongoDBConn{options: args, LocalCommand: lCmd}, nil
}

type MongoDBConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *MongoDBConn) KeepAlive() error {
	return nil
}

func (conn *MongoDBConn) Close() error {
	_, _ = conn.Write([]byte("\r\nexit\r\n"))
	return conn.LocalCommand.Close()
}

func startMongoDBCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	cmd := opt.MongoDBCommandArgs()
	lcmd, err = localcommand.New("mongosh", cmd, localcommand.WithPtyWin(opt.win.Width, opt.win.Height))
	if err != nil {
		return nil, err
	}
	if opt.Password != "" {
		lcmd, err = MatchLoginPrefix(mongodbPrompt, "MongoDB", lcmd)
		if err != nil {
			return lcmd, err
		}
		lcmd, err = DoLogin(opt, lcmd, "MongoDB")
		if err != nil {
			return lcmd, err
		}
	}
	return lcmd, nil
}

func addMongoParamsWithSSL(args *sqlOption, params map[string]string) {
	if args.UseSSL {
		params["tls"] = "true"
		if args.CaCertPath != "" {
			params["tlsCAFile"] = args.CaCertPath
		}
		if args.CertKeyPath != "" {
			params["tlsCertificateKeyFile"] = args.CertKeyPath
		}
		if args.AllowInvalidCert {
			params["tlsInsecure"] = "true"
		}
	}
}

func (opt *sqlOption) MongoDBCommandArgs() []string {
	host := net.JoinHostPort(opt.Host, strconv.Itoa(opt.Port))
	params := map[string]string{
		"authSource": "admin",
	}
	addMongoParamsWithSSL(opt, params)
	uri := BuildMongoDBURI(
		MongoHost(host),
		MongoDBName(opt.DBName),
		MongoParams(params),
	)
	uriParams := []string{
		uri, "--username", opt.Username,
	}
	return uriParams
}

func checkMongoDBAccount(args *sqlOption) error {
	host := net.JoinHostPort(args.Host, strconv.Itoa(args.Port))
	// todo: authSource 暂且只使用 admin， 待后续可配置后，修改这个认证的值
	// https://www.mongodb.com/docs/manual/reference/connection-string/#mongodb-urioption-urioption.authSource
	params := map[string]string{
		"authSource": "admin",
		"connect":    "direct",
	}
	addMongoParamsWithSSL(args, params)
	uri := BuildMongoDBURI(
		MongoHost(host),
		MongoAuth(args.Username, args.Password),
		MongoDBName(args.DBName),
		MongoParams(params),
	)
	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		return err
	}
	defer func() {
		_ = client.Disconnect(context.TODO())
	}()
	err = client.Ping(context.TODO(), nil)
	if err != nil {
		return err
	}
	return nil
}

type MongoOpt func(*url.URL)

func BuildMongoDBURI(opts ...MongoOpt) string {
	var mongoURI url.URL
	mongoURI.Scheme = "mongodb"
	for _, setter := range opts {
		setter(&mongoURI)
	}
	return mongoURI.String()
}

func MongoHost(host string) MongoOpt {
	return func(u *url.URL) {
		u.Host = host
	}
}

func MongoAuth(user, password string) MongoOpt {
	return func(u *url.URL) {
		if user == "" || password == "" {
			return
		}
		u.User = url.UserPassword(user, password)
	}
}

func MongoDBName(dbName string) MongoOpt {
	return func(u *url.URL) {
		u.Path = dbName
	}
}

func MongoParams(params ...map[string]string) MongoOpt {
	return func(u *url.URL) {
		values := url.Values{}
		for i := range params {
			for k, v := range params[i] {
				values.Set(k, v)
			}
		}
		u.RawQuery = values.Encode()
	}
}
