package srvconn

type sqlOption struct {
	AssetName        string
	Schema           string
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

	AuthSource        string
	ConnectionOptions string
}

type SqlOption func(*sqlOption)

func SqlSchema(schema string) SqlOption {
	return func(args *sqlOption) {
		args.Schema = schema
	}
}

func SqlAssetName(assetName string) SqlOption {
	return func(args *sqlOption) {
		args.AssetName = assetName
	}
}

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

func SqlConnectionOptions(options string) SqlOption {
	return func(args *sqlOption) {
		args.ConnectionOptions = options
	}
}
