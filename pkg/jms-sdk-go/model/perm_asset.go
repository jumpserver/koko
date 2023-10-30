package model

import (
	"fmt"
	"sort"
	"strings"
)

type PermAsset struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Address  string       `json:"address"`
	Comment  string       `json:"comment"`
	Platform BasePlatform `json:"platform"`
	OrgID    string       `json:"org_id"`
	OrgName  string       `json:"org_name"`
	IsActive bool         `json:"is_active"`
	Type     LabelField   `json:"type"`
	Category LabelField   `json:"category"`
}

func (a *PermAsset) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Address)
}

type PermAssetList []PermAsset

func (a PermAssetList) SortBy(tp string) PermAssetList {
	var sortedAssets = make(PermAssetList, len(a))
	copy(sortedAssets, a)
	switch tp {
	case "ip":
		sorter := &permAssetSorter{
			data: sortedAssets,
			sortBy: func(asset1, asset2 *PermAsset) bool {
				return sortByIP(asset1.Address, asset2.Address)
			},
		}
		sort.Sort(sorter)
	default:
		sorter := &permAssetSorter{
			data: sortedAssets,
			sortBy: func(asset1, asset2 *PermAsset) bool {
				return asset1.Name < asset2.Name
			},
		}
		sort.Sort(sorter)
	}
	return sortedAssets
}

type permAssetSorter struct {
	data   []PermAsset
	sortBy func(asset1, asset2 *PermAsset) bool
}

func (s *permAssetSorter) Len() int {
	return len(s.data)
}

func (s *permAssetSorter) Swap(i, j int) {
	s.data[i], s.data[j] = s.data[j], s.data[i]
}

func (s *permAssetSorter) Less(i, j int) bool {
	return s.sortBy(&s.data[i], &s.data[j])
}

type PermAssetDetail struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Address         string        `json:"address"`
	Comment         string        `json:"comment"`
	Platform        BasePlatform  `json:"platform"`
	OrgID           string        `json:"org_id"`
	OrgName         string        `json:"org_name"`
	IsActive        bool          `json:"is_active"`
	Type            LabelField    `json:"type"`
	Category        LabelField    `json:"category"`
	PermedAccounts  []PermAccount `json:"permed_accounts"`
	PermedProtocols []Protocol    `json:"permed_protocols"`
}

func (a *PermAssetDetail) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Address)
}

func (a *PermAssetDetail) SupportProtocol(protocol string) bool {
	for _, p := range a.PermedProtocols {
		if strings.EqualFold(p.Name, protocol) {
			return true
		}
	}
	return false
}

func sortByIP(ipA, ipB string) bool {
	iIPs := strings.Split(ipA, ".")
	jIPs := strings.Split(ipB, ".")
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
