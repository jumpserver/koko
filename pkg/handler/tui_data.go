package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

const tuiPageSize = 50
const tuiTreeBatchSize = 100
const tuiGlobalOrganizationID = "00000000-0000-0000-0000-000000000000"
const tuiDefaultOrganizationID = "00000000-0000-0000-0000-000000000002"

type tuiOrganization struct{ ID, Name string }

type tuiTreeNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"pId"`
	Meta   struct {
		Type      string `json:"type"`
		Category  string `json:"category"`
		AssetType string `json:"_type"`
		Data      struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Root bool   `json:"is_root"`
		} `json:"data"`
	} `json:"meta"`
}

type tuiTreePage struct {
	Results    []tuiTreeNode `json:"results"`
	Pagination struct {
		Next string `json:"next"`
	} `json:"node_pagination"`
}

func (p *tuiTreePage) UnmarshalJSON(b []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(b)), "[") {
		return json.Unmarshal(b, &p.Results)
	}
	type page tuiTreePage
	return json.Unmarshal(b, (*page)(p))
}

type tuiScope struct {
	Org                                                               tuiOrganization
	Mode                                                              int
	NodeID, Key, FolderID, Category, AssetType, Platform, Label, Path string
}

type tuiData struct {
	api          *service.JMService
	userID, lang string
}

func (d tuiData) client(org string) *service.JMService {
	c := newLangAPIClient(d.api, d.lang)
	c.SetHeader("X-JMS-ORG", org)
	// SDK Copy creates a new transport. These scoped clients cannot pool
	// connections across requests, so do not retain an idle socket per lookup.
	c.SetHeader("Connection", "close")
	return c
}

func (d tuiData) userPath(suffix string) string {
	return "/api/v1/perms/users/" + url.PathEscape(d.userID) + "/" + suffix
}

// The component role cannot read RBAC bindings. Enumerate organization IDs
// internally, then use the org-scoped user list to verify membership. Only
// verified memberships are exposed to the logged-in user.
func (d tuiData) organizations(ctx context.Context, first func(tuiOrganization)) ([]tuiOrganization, error) {
	var orgs []tuiOrganization
	var firstMember sync.Once
	c := d.client("ROOT")
	for offset := 0; offset < 1000; offset += 100 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var page struct {
			Next    string            `json:"next"`
			Results []tuiOrganization `json:"results"`
		}
		_, err := c.Call("GET", "/api/v1/orgs/orgs/", nil, &page,
			map[string]string{"limit": "100", "offset": strconv.Itoa(offset), "fields_size": "mini"})
		if err != nil {
			return nil, err
		}
		// Keep membership checks bounded and parallel, preserving Core's order.
		// A slow organization must not serialize every other membership request.
		members := make([]bool, len(page.Results))
		errs := make([]error, len(page.Results))
		jobs := make(chan int, len(page.Results))
		for i, org := range page.Results {
			if org.ID == "" || org.ID == tuiGlobalOrganizationID || org.ID == "00000000-0000-0000-0000-000000000004" {
				continue
			}
			jobs <- i
		}
		close(jobs)
		var workers sync.WaitGroup
		for range min(4, len(jobs)) {
			workers.Go(func() {
				for i := range jobs {
					if errs[i] = ctx.Err(); errs[i] != nil {
						continue
					}
					var users struct {
						Results []struct {
							ID string `json:"id"`
						} `json:"results"`
					}
					_, errs[i] = d.client(page.Results[i].ID).Call("GET", "/api/v1/users/users/", nil, &users,
						map[string]string{"id": d.userID, "limit": "1", "fields_size": "mini"})
					members[i] = len(users.Results) == 1 && users.Results[0].ID == d.userID
					if errs[i] == nil && members[i] && first != nil {
						firstMember.Do(func() { first(page.Results[i]) })
					}
				}
			})
		}
		workers.Wait()
		for i, org := range page.Results {
			if errs[i] != nil {
				return nil, errs[i]
			}
			if members[i] {
				orgs = append(orgs, org)
			}
		}
		if page.Next == "" {
			return orgs, nil
		}
	}
	return nil, fmt.Errorf("organization lookup exceeds 1000 organizations")
}

func (d tuiData) tree(scope tuiScope, cursor string) (tuiTreePage, error) {
	var page tuiTreePage
	params := map[string]string{"include_assets": "false"}
	path := "nodes/children-with-assets/tree/"
	switch scope.Mode {
	case 0:
		params["include_nodes"] = "true"
		params["node_page_size"] = strconv.Itoa(tuiTreeBatchSize)
		params["parent_key"] = scope.Key
		params["node_cursor"] = cursor
	case 1:
		path = "nodes/children-with-assets/category/tree/"
	case 2:
		path = "favorite-tree/"
		params["parent_id"] = scope.FolderID
	}
	_, err := d.client(scope.Org.ID).Call("GET", d.userPath(path), nil, &page, params)
	return page, err
}

func (d tuiData) assets(scope tuiScope, search string, offset int) (model.PaginationResponse, error) {
	var page model.PaginationResponse
	if scope.Org.ID == "" {
		return page, fmt.Errorf("select an organization first")
	}
	params := map[string]string{"limit": strconv.Itoa(tuiPageSize), "offset": strconv.Itoa(offset),
		"search": search, "order": "name", "is_active": "true"}
	path := "assets/"
	switch scope.Mode {
	case 0:
		if scope.Key == "ungrouped" {
			path = "nodes/ungrouped/assets/"
		} else if scope.NodeID != "" {
			path = "nodes/" + url.PathEscape(scope.NodeID) + "/assets/"
		}
	case 1:
		params["category"], params["type"] = scope.Category, scope.AssetType
		params["platform"] = scope.Platform
	case 2:
		path = "nodes/favorite/assets/"
		params["folder_id"] = scope.FolderID
	}
	_, err := d.client(scope.Org.ID).Call("GET", d.userPath(path), nil, &page, params)
	return page, err
}

func nextTreeCursor(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Query().Get("node_cursor")
}

func (d tuiData) nodeCounts(org string, mode int, ids []string) (map[string]int, error) {
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
	_, err := d.client(org).Call("POST", d.userPath("tree-metrics/"), map[string]any{"tree": tree, "resources": resources}, &response)
	counts := make(map[string]int, len(response.Results))
	for _, item := range response.Results {
		if item.Count >= 0 {
			counts[item.ID] = item.Count
		}
	}
	return counts, err
}
