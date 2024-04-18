package srvconn

import (
	"database/sql"
	"time"
)

type sqlOption struct {
	Username         string
	Password         string
	DBName           string
	Host             string
	Port             int
	UseSSL           bool
	CaCert           string
	CaCertPath       string
	ClientCert       string
	ClientCertPath   string
	CertKey          string
	CertKeyPath      string
	AllowInvalidCert bool

	win Windows

	disableMySQLAutoRehash bool

	AuthSource string
}

type SqlOption func(*sqlOption)

func SqlUsername(username string) SqlOption {
	return func(args *sqlOption) {
		args.Username = username
	}
}

func SqlPassword(password string) SqlOption {
	return func(args *sqlOption) {
		args.Password = password
	}
}

func SqlDBName(dbName string) SqlOption {
	return func(args *sqlOption) {
		args.DBName = dbName
	}
}

func SqlHost(host string) SqlOption {
	return func(args *sqlOption) {
		args.Host = host
	}
}

func SqlPort(port int) SqlOption {
	return func(args *sqlOption) {
		args.Port = port
	}
}

func SqlUseSSL(useSSL bool) SqlOption {
	return func(args *sqlOption) {
		args.UseSSL = useSSL
	}
}

func SqlCaCert(caCert string) SqlOption {
	return func(args *sqlOption) {
		args.CaCert = caCert
	}
}

func SqlCertKey(certKey string) SqlOption {
	return func(args *sqlOption) {
		args.CertKey = certKey
	}
}

func SqlClientCert(clientCert string) SqlOption {
	return func(args *sqlOption) {
		args.ClientCert = clientCert
	}
}

func SqlAllowInvalidCert(allowInvalidCert bool) SqlOption {
	return func(args *sqlOption) {
		args.AllowInvalidCert = allowInvalidCert
	}
}

func SqlPtyWin(win Windows) SqlOption {
	return func(args *sqlOption) {
		args.win = win
	}
}

func SqlAuthSource(authSource string) SqlOption {
	return func(args *sqlOption) {
		args.AuthSource = authSource
	}
}

const (
	maxSQLConnCount = 1
	maxIdleTime     = time.Second * 15
)

func checkDatabaseAccountValidate(driveName, datasourceName string) error {
	db, err := sql.Open(driveName, datasourceName)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(maxSQLConnCount)
	db.SetMaxIdleConns(maxSQLConnCount)
	db.SetConnMaxLifetime(maxIdleTime)
	db.SetConnMaxIdleTime(maxIdleTime)
	defer db.Close()
	err = db.Ping()
	if err != nil {
		return err
	}
	return nil
}
