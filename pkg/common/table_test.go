package common

import (
	"fmt"
	"testing"
)

func TestNewTable_CalculateColumnsSize(t *testing.T) {
	table := WrapperTable{
		Fields: []string{"ID", "主机名", "IP", "系统用户", "Comment"},
		Data: []map[string]string{
			{"ID": "1", "主机名": "asdfasdf", "IP": "192.168.1.1", "系统用户": "123", "Comment": "你好"},
			{"ID": "2", "主机名": "bbb", "IP": "255.255.255.255", "系统用户": "o", "Comment": ""},
			{"ID": "3", "主机名": "3", "IP": "1.1.1.1", "系统用户": "", "Comment": "aaaa"},
			{"ID": "3", "主机名": "22323", "IP": "1.1.2.1", "系统用户": "", "Comment": ""},
			{"ID": "2", "主机名": "22323", "IP": "192.168.1.1", "系统用户": "", "Comment": ""},
		},
		FieldsSize: map[string][3]int{
			"ID":      {0, 0, 5},
			"主机名":     {0, 8, 25},
			"IP":      {15, 0, 0},
			"系统用户":    {0, 12, 20},
			"Comment": {0, 0, 0},
		},
		TotalSize: 140,
	}
	table.Initial()

	data := table.Display()
	fmt.Println(data)
	fmt.Println(table.fieldsSize)
}


func TestGetCorrectString(t *testing.T) {
	foo := "主2erert机名"
	a:=GetValidString(foo,2,false)
	t.Log(a == "2erert机名")
}