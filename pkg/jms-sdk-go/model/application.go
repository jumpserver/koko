package model

import (
	"fmt"
)

type k8sAttrs struct {
	Cluster string `json:"cluster"`
}

type dbAttrs struct {
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Database          string `json:"database"`
	UseSSL            bool   `json:"use_ssl"`
	CaCert            string `json:"ca_cert"`
	ClientCert        string `json:"client_cert"`
	CertKey           string `json:"cert_key"`
	AllowInvalidCert  bool   `json:"allow_invalid_cert"`
}

const (
	AppTypeMySQL = "mysql"
	AppTypeK8s   = "k8s"

	AppTypeMariaDB     = "mariadb"
	AppTypeSQLServer   = "sqlserver"
	AppTypePostgres    = "postgresql"
	AppTypeClickhouse  = "clickhouse"
	AppTypeRedis       = "redis"
	AppTypeMongoDB     = "mongodb"
)

var (
	SupportedDBTypes = []string{AppTypeMySQL, AppTypeMariaDB, AppTypeSQLServer,
		AppTypePostgres, AppTypeRedis, AppTypeMongoDB, AppTypeClickhouse}
)

const AppType = "Application"

type Application struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	TypeName string `json:"type"`
	Domain   string `json:"domain"`
	Comment  string `json:"comment"`
	OrgID    string `json:"org_id"`
	OrgName  string `json:"org_name"`

	Attrs    Attrs  `json:"attrs"`
}

type Attrs struct {
	k8sAttrs

	dbAttrs
}

func (app Application) String() string {
	switch app.Category {
	case categoryDB:
		return fmt.Sprintf("%s://%s:%d/%s",
			app.TypeName,
			app.Attrs.Host,
			app.Attrs.Port,
			app.Attrs.Database)
	case categoryCloud:
	}
	return fmt.Sprintf("%s://%s",
		app.TypeName,
		app.Name)
}

const (
	categoryDB    = "db"
	categoryCloud = "cloud"
)

type ConnectType string

const (
	ConnectApplication ConnectType = "application"
	ConnectAsset       ConnectType = "asset"
)
