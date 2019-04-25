package sdk

import (
	"sort"
	"strconv"
	"strings"
)

type Asset struct {
	Id              string       `json:"id"`
	Hostname        string       `json:"hostname"`
	Ip              string       `json:"ip"`
	Port            int          `json:"port"`
	SystemUsers     []SystemUser `json:"system_users_granted"`
	IsActive        bool         `json:"is_active"`
	SystemUsersJoin string       `json:"system_users_join"`
	Os              string       `json:"os"`
	Domain          string       `json:"domain"`
	Platform        string       `json:"platform"`
	Comment         string       `json:"comment"`
	Protocol        string       `json:"protocol"`
	OrgID           string       `json:"org_id"`
	OrgName         string       `json:"org_name"`
}

type Node struct {
	Id            string  `json:"id"`
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	Value         string  `json:"value"`
	Parent        string  `json:"parent"`
	AssetsGranted []Asset `json:"assets_granted"`
	AssetsAmount  int     `json:"assets_amount"`
	OrgId         string  `json:"org_id"`
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

type SystemUser struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	UserName   string `json:"username"`
	Priority   int    `json:"priority"`
	Protocol   string `json:"protocol"`
	Comment    string `json:"comment"`
	LoginMode  string `json:"login_mode"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
}

type SystemUserAuthInfo struct {
	Id         string `json:"id"`
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
	return use1.Priority <= user2.Priority
}

func SortSystemUserByPriority(users []SystemUser) {
	systemUserSortBy(systemUserPrioritySort).Sort(users)
}
