package model

import (
	"encoding/json"
	"testing"
)

func TestSortAssetNodesByKey(t *testing.T) {
	var jsonString = `
		{
			"id": "2b8f37ad-1580-4275-962a-7ea0f53c40b3",
			"name": "www",
			"domain": null,
			"category": "db",
			"type": "mysql",
			"attrs": {
			  "host": "www",
			  "port": 32342,
			  "database": null
			},
			"comment": "",
			"org_id": "",
			"category_display": "数据库",
			"type_display": "MySQL",
			"org_name": "DEFAULT"
		}
`
	var app DatabaseApplication
	err := json.Unmarshal([]byte(jsonString), &app)
	t.Log(err)
	t.Logf("%v", app)
}
