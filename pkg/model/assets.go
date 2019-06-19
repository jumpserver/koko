package model

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type AssetList []Asset

func (a AssetList) SortBy(tp string) AssetList {
	var sortedAssets = make(AssetList, len(a))
	copy(sortedAssets, a)
	switch tp {
	case "ip":
		sorter := &assetSorter{
			data:   sortedAssets,
			sortBy: assetSortByIP,
		}
		sort.Sort(sorter)
	default:
		sorter := &assetSorter{
			data:   sortedAssets,
			sortBy: assetSortByHostName,
		}
		sort.Sort(sorter)
	}
	return sortedAssets
}

type assetSorter struct {
	data   []Asset
	sortBy func(asset1, asset2 *Asset) bool
}

func (s *assetSorter) Len() int {
	return len(s.data)
}

func (s *assetSorter) Swap(i, j int) {
	s.data[i], s.data[j] = s.data[j], s.data[i]
}

func (s *assetSorter) Less(i, j int) bool {
	return s.sortBy(&s.data[i], &s.data[j])
}

func assetSortByIP(asset1, asset2 *Asset) bool {
	iIPs := strings.Split(asset1.IP, ".")
	jIPs := strings.Split(asset2.IP, ".")
	for i := 0; i < len(iIPs); i++ {
		if i >= len(jIPs) {
			return false
		}
		if len(iIPs[i]) == len(jIPs[i]) {
			if iIPs[i] == jIPs[i] {
				continue
			} else {
				return iIPs[i] < jIPs[i]
			}
		} else {
			return len(iIPs[i]) < len(jIPs[i])
		}

	}
	return true

}

func assetSortByHostName(asset1, asset2 *Asset) bool {
	return asset1.Hostname < asset2.Hostname
}

type NodeList []Node

type Asset struct {
	ID              string         `json:"id"`
	Hostname        string         `json:"hostname"`
	IP              string         `json:"ip"`
	Port            int            `json:"port"`
	SystemUsers     []SystemUser   `json:"system_users_granted"`
	IsActive        bool           `json:"is_active"`
	SystemUsersJoin string         `json:"system_users_join"`
	Os              string         `json:"os"`
	Domain          string         `json:"domain"`
	Platform        string         `json:"platform"`
	Comment         string         `json:"comment"`
	Protocol        string         `json:"protocol"`
	Protocols       []protocolItem `json:"protocols,omitempty"`
	OrgID           string         `json:"org_id"`
	OrgName         string         `json:"org_name"`
}

func (a *Asset) ProtocolPort(protocol string) int {
	// 向下兼容
	if a.Protocols == nil {
		return a.Port
	}
	for _, item := range a.Protocols {
		if strings.ToLower(item.Name) == strings.ToLower(protocol) {
			return item.Port
		}
	}
	switch strings.ToLower(protocol) {
	case "telnet":
		return 23
	case "vnc":
		return 5901
	case "rdp":
		return 3389
	default:
		return 22
	}
}

func (a *Asset) IsSupportProtocol(protocol string) bool {
	if a.Protocols == nil {
		return a.Protocol == protocol
	}
	for _, item := range a.Protocols {
		if strings.ToLower(item.Name) == strings.ToLower(protocol) {
			return true
		}
	}
	return false
}

type protocolItem struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type Gateway struct {
	ID         string `json:"id"`
	Name       string `json:"Name"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Protocol   string `json:"protocol"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
}

type Domain struct {
	ID       string    `json:"id"`
	Gateways []Gateway `json:"gateways"`
	Name     string    `json:"name"`
}

type Node struct {
	ID            string  `json:"id"`
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	Value         string  `json:"value"`
	Parent        string  `json:"parent"`
	AssetsGranted []Asset `json:"assets_granted"`
	AssetsAmount  int     `json:"assets_amount"`
	OrgID         string  `json:"org_id"`
}

type nodeSortBy func(node1, node2 *Node) bool

func (by nodeSortBy) Sort(assetNodes []Node) {
	nodeSorter := &AssetNodeSorter{
		assetNodes: assetNodes,
		sortBy:     by,
	}
	sort.Sort(nodeSorter)
}

type AssetNodeSorter struct {
	assetNodes []Node
	sortBy     func(node1, node2 *Node) bool
}

func (a *AssetNodeSorter) Len() int {
	return len(a.assetNodes)
}

func (a *AssetNodeSorter) Swap(i, j int) {
	a.assetNodes[i], a.assetNodes[j] = a.assetNodes[j], a.assetNodes[i]
}

func (a *AssetNodeSorter) Less(i, j int) bool {
	return a.sortBy(&a.assetNodes[i], &a.assetNodes[j])
}

/*
key的排列顺序：
1 1:3 1:3:0 1:4 1:5 1:8
*/
func keySort(node1, node2 *Node) bool {
	node1Keys := strings.Split(node1.Key, ":")
	node2Keys := strings.Split(node2.Key, ":")
	for i := 0; i < len(node1Keys); i++ {
		if i >= len(node2Keys) {
			return false
		}
		node1num, _ := strconv.Atoi(node1Keys[i])
		node2num, _ := strconv.Atoi(node2Keys[i])
		if node1num == node2num {
			continue
		} else if node1num-node2num > 0 {
			return false
		} else {
			return true
		}

	}
	return true

}

func SortAssetNodesByKey(assetNodes []Node) {
	nodeSortBy(keySort).Sort(assetNodes)
}

const LoginModeManual = "manual"

const (
	ConnectAction  = "connect"
	UploadAction   = "upload_file"
	DownloadAction = "download_file"
)

type SystemUser struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Username   string   `json:"username"`
	Priority   int      `json:"priority"`
	Protocol   string   `json:"protocol"`
	Comment    string   `json:"comment"`
	LoginMode  string   `json:"login_mode"`
	Password   string   `json:"password"`
	PrivateKey string   `json:"private_key"`
	Actions    []string `json:"actions"`
}

type SystemUserAuthInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UserName   string `json:"username"`
	Protocol   string `json:"protocol"`
	LoginMode  string `json:"login_mode"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
}

type systemUserSortBy func(user1, user2 *SystemUser) bool

func (by systemUserSortBy) Sort(users []SystemUser) {
	nodeSorter := &systemUserSorter{
		users:  users,
		sortBy: by,
	}
	sort.Sort(nodeSorter)
}

type systemUserSorter struct {
	users  []SystemUser
	sortBy func(user1, user2 *SystemUser) bool
}

func (s *systemUserSorter) Len() int {
	return len(s.users)
}

func (s *systemUserSorter) Swap(i, j int) {
	s.users[i], s.users[j] = s.users[j], s.users[i]
}

func (s *systemUserSorter) Less(i, j int) bool {
	return s.sortBy(&s.users[i], &s.users[j])
}

func systemUserPrioritySort(use1, user2 *SystemUser) bool {
	return use1.Priority < user2.Priority
}

func SortSystemUserByPriority(users []SystemUser) {
	systemUserSortBy(systemUserPrioritySort).Sort(users)
}

type RuleAction int

const (
	ActionDeny    RuleAction = 0
	ActionAllow   RuleAction = 1
	ActionUnknown RuleAction = 2

	TypeRegex = "regex"
	TypeCmd   = "command"
)

type SystemUserFilterRule struct {
	Priority int `json:"priority"`
	Type     struct {
		Value string `json:"value"`
	} `json:"type"`
	Content string `json:"content"`
	Action  struct {
		Value RuleAction `json:"value"`
	} `json:"action"`

	pattern  *regexp.Regexp
	compiled bool
}

func (sf *SystemUserFilterRule) Pattern() *regexp.Regexp {
	if sf.compiled {
		return sf.pattern
	}
	var regexs string
	if sf.Type.Value == TypeCmd {
		var regex []string
		for _, cmd := range strings.Split(sf.Content, "\r\n") {
			cmd = strings.Replace(cmd, " ", "\\s+", 1)
			regex = append(regex, fmt.Sprintf("\\b%s\\b", cmd))
		}
		regexs = strings.Join(regex, "|")
	} else {
		regexs = sf.Content
	}
	pattern, err := regexp.Compile(regexs)
	if err == nil {
		sf.pattern = pattern
		sf.compiled = true
	}
	return pattern
}

func (sf *SystemUserFilterRule) Match(cmd string) (RuleAction, string) {
	pattern := sf.Pattern()
	if pattern == nil{
		return ActionUnknown, ""
	}
	found := pattern.FindString(cmd)
	if found == "" {
		return ActionUnknown, ""
	}
	return sf.Action.Value, found
}
