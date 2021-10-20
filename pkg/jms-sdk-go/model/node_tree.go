package model

import "strings"

type NodeTreeList []NodeTree

type NodeTree struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Pid      string   `json:"pId"`
	IsParent bool     `json:"isParent"`
	Meta     TreeMeta `json:"meta"`

	ChkDisabled bool `json:"chkDisabled"` // 判断资产是否禁用
}

type TreeMeta struct {
	Type string       `json:"type"`
	Data NodeTreeMeta `json:"data"`
}

type NodeTreeMeta struct {
	ID string `json:"id"`

	NodeMeta
	AssetMeta
}

func (n NodeTreeMeta) IsSupportProtocol(protocol string) bool {
	for _, item := range n.Protocols {
		if strings.Contains(strings.ToLower(item),
			strings.ToLower(protocol)) {
			return true
		}
	}
	return false
}

type NodeMeta struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type AssetMeta struct {
	Hostname  string   `json:"hostname"`
	IP        string   `json:"ip"`
	Protocols []string `json:"protocols"`
	Platform  string   `json:"platform"`
	OrgName   string   `json:"org_name"`
}

const (
	TreeTypeNode  = "node"
	TreeTypeAsset = "asset"
)
