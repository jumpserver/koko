package model

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
	NodeMeta
	AssetMeta
}

type NodeMeta struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type AssetMeta struct {
	OrgName     string `json:"org_name"`
	SupportSFTP bool   `json:"sftp"`
}

const (
	TreeTypeNode  = "node"
	TreeTypeAsset = "asset"
)
