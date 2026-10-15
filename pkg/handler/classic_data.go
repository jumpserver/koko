package handler

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

const classicTreeBatchSize = 100
const globalOrganizationID = "00000000-0000-0000-0000-000000000000"

type classicTreeNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"pId"`
	Meta   struct {
		Type      string `json:"type"`
		Category  string `json:"category"`
		AssetType string `json:"_type"`
		Data      struct {
			ID    string `json:"id"`
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"data"`
	} `json:"meta"`
}

type classicTreePage struct {
	Results []classicTreeNode `json:"results"`
}

func (p *classicTreePage) UnmarshalJSON(data []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		return json.Unmarshal(data, &p.Results)
	}
	type page classicTreePage
	return json.Unmarshal(data, (*page)(p))
}

type classicData struct {
	api    *service.JMService
	userID string
	lang   string
}

func (d classicData) userPath(suffix string) string {
	return "/api/v1/perms/users/" + url.PathEscape(d.userID) + "/" + suffix
}

func (d classicData) authorizationNodes() (model.NodeList, error) {
	client := newLangAPIClient(d.api, d.lang)
	permittedNodes, err := client.GetUserNodes(d.userID)
	if err != nil {
		return nil, err
	}
	nodes := make(model.NodeList, 0, len(permittedNodes))
	for _, node := range permittedNodes {
		if node.ID == "favorite" || node.Key == "favorite" {
			continue
		}
		if node.ID == "" || node.Key == "" {
			return nil, fmt.Errorf("authorization node response is missing id or key")
		}
		node.Name = strings.TrimSpace(node.Name)
		if node.Name == "" {
			node.Name = strings.TrimSpace(node.Value)
		}
		if node.Name == "" {
			return nil, fmt.Errorf("authorization node response is missing name")
		}
		node.AssetsAmount = 0
		nodes = append(nodes, node)
	}
	ids := make([]string, len(nodes))
	for i := range nodes {
		ids[i] = nodes[i].ID
	}
	for start := 0; start < len(ids); start += classicTreeBatchSize {
		end := min(start+classicTreeBatchSize, len(ids))
		counts, countErr := d.nodeCounts("", 0, ids[start:end])
		if countErr != nil {
			return nil, countErr
		}
		for i := start; i < end; i++ {
			nodes[i].AssetsAmount = counts[nodes[i].ID]
		}
	}
	return nodes, nil
}

func (d classicData) nodeCounts(org string, mode int, ids []string) (map[string]int, error) {
	resources := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		resources = append(resources, map[string]string{"type": "node", "id": id})
	}
	var response struct {
		Results []struct {
			ID    string `json:"id"`
			Count int    `json:"count"`
		} `json:"results"`
	}
	tree := "authorization"
	if mode == 2 {
		tree = "favorite"
	}
	client := newLangAPIClient(d.api, d.lang)
	if org != "" {
		client.SetHeader("X-JMS-ORG", org)
	}
	client.SetHeader("Connection", "close")
	_, err := client.Call("POST", d.userPath("tree-metrics/"), map[string]any{
		"tree": tree, "resources": resources,
	}, &response)
	counts := make(map[string]int, len(response.Results))
	for _, item := range response.Results {
		if item.Count >= 0 {
			counts[item.ID] = item.Count
		}
	}
	return counts, err
}
