package model

import "encoding/json"

type NodeTreeList []NodeTreeAsset

type NodeTreeAsset struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Title    string                 `json:"title"`
	Pid      string                 `json:"pId"`
	IsParent bool                   `json:"isParent"`
	Meta     map[string]interface{} `json:"meta"`
}

func ConvertMetaToNode(body []byte) (node Node, err error) {
	err = json.Unmarshal(body, &node)
	return
}

func ConvertMetaToAsset(body []byte) (asset Asset, err error) {
	err = json.Unmarshal(body, &asset)
	return
}
