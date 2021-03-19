package model

import (
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

type Asset struct {
	ID        string   `json:"id"`
	Hostname  string   `json:"hostname"`
	IP        string   `json:"ip"`
	Os        string   `json:"os"`
	Domain    string   `json:"domain"`
	Comment   string   `json:"comment"`
	Protocols []string `json:"protocols,omitempty"`
	OrgID     string   `json:"org_id"`
	OrgName   string   `json:"org_name"`
	Platform  string   `json:"platform,omitempty"`
	IsActive  bool     `json:"is_active"` // 判断资产是否禁用
}

func (a *Asset) ProtocolPort(protocol string) int {
	for _, item := range a.Protocols {
		if strings.Contains(strings.ToLower(item), strings.ToLower(protocol)) {
			proAndPort := strings.Split(item, "/")
			if len(proAndPort) == 2 {
				if port, err := strconv.Atoi(proAndPort[1]); err == nil {
					return port
				}
			}
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
	for _, item := range a.Protocols {
		if strings.Contains(strings.ToLower(item), strings.ToLower(protocol)) {
			return true
		}
	}
	return false
}

func (a *Asset) Active() bool {
	return a.IsActive
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
