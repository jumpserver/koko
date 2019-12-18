package model

import "fmt"

type Database struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	DBType  string `json:"type"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	DBName  string `json:"database"`
	OrgID   string `json:"org_id"`
	Comment string `json:"comment"`
}

func (db Database) String() string {
	return fmt.Sprintf("%s://%s:%d/%s", db.DBType, db.Host, db.Port, db.DBName)
}
