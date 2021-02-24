package model

import "fmt"

type k8sAttrs struct {
	Cluster string `json:"cluster"`
}

type dbAttrs struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
}

type BaseApplication struct {
	Id       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	TypeName string `json:"type"`
	Domain   string `json:"domain"`
	Comment  string `json:"comment"`
	OrgId    string `json:"org_id"`
	OrgName  string `json:"org_name"`
}

type K8sApplication struct {
	BaseApplication
	Attrs k8sAttrs `json:"attrs"`
}

type DatabaseApplication struct {
	BaseApplication
	Attrs dbAttrs `json:"attrs"`
}

func (db DatabaseApplication) String() string {
	return fmt.Sprintf("%s://%s:%d/%s",
		db.TypeName,
		db.Attrs.Host,
		db.Attrs.Port,
		db.Attrs.Database)
}

const (
	AppTypeMySQL = "mysql"
	AppTypeK8s   = "k8s"
)

const AppType = "Application"