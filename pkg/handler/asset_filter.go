package handler

import (
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

const assetFilterBatchSize = 100

func supportedAssetProtocols() string {
	protocols := srvconn.SupportedProtocols()
	sort.Strings(protocols)
	return strings.Join(protocols, ",")
}

// Apply connection filters before pagination. The second, one-row request keeps
// the unfiltered count in the same node, type, favorite folder and search scope.
func (u *UserSelectHandler) retrieveRemoteAssetPage(req model.PaginationParam, path string, filters map[string]string) []model.PermAsset {
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = assetFilterBatchSize
	}
	params := map[string]string{
		"limit": strconv.Itoa(pageSize), "offset": strconv.Itoa(max(0, req.Offset)),
		"order": req.Order,
	}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Type != "" {
		params["type"] = req.Type
	}
	if len(req.Searches) > 0 {
		searches := make([]string, len(req.Searches))
		for i, search := range req.Searches {
			searches[i] = strings.TrimSpace(search)
		}
		params["search"] = strings.Join(searches, ",")
	}
	maps.Copy(params, filters)
	totalParams := maps.Clone(params)
	totalParams["limit"], totalParams["offset"] = "1", "0"
	params["is_active"] = "true"
	params["protocols"] = supportedAssetProtocols()

	var response, unfiltered model.PaginationResponse
	_, err := u.h.jmsService.Call("GET", path, nil, &response, params)
	if err == nil {
		_, err = u.h.jmsService.Call("GET", path, nil, &unfiltered, totalParams)
	}
	if err != nil {
		logger.Errorf("Get filtered user assets and counts failed: %s", err)
		u.loadErr = err
		return nil
	}
	if req.PageSize <= 0 {
		all := response.Data
		for response.NextURL != "" {
			response, err = u.h.jmsService.GetNextURLPermAssets(response.NextURL)
			if err != nil {
				logger.Errorf("Get next filtered user assets failed: %s", err)
				u.loadErr = err
				return nil
			}
			all = append(all, response.Data...)
		}
		response.Data, response.Total = all, len(all)
		response.NextURL, response.PreviousURL = "", ""
	}
	u.allAssetCount = &unfiltered.Total
	assets := u.updateRemotePageData(req, response)
	u.markConnectableAssets(assets)
	return assets
}

// The optional all-assets cache has no protocol fields. Resolve eligibility in
// bounded batches once per cache load, retaining local search/hidden-field rules.
func (u *UserSelectHandler) filterLocalAssets(assets []model.PermAsset) []model.PermAsset {
	if u.localConnectable == nil {
		u.localConnectable = make(map[string]bool)
	}
	if u.h != nil && u.h.jmsService != nil {
		ids := make([]string, 0, assetFilterBatchSize)
		lookup := func() error {
			var response model.PaginationResponse
			_, err := u.h.jmsService.Call("GET", u.assetListPath(), nil, &response, map[string]string{
				"limit": strconv.Itoa(len(ids)), "offset": "0",
				"id__in": strings.Join(ids, ","), "is_active": "true",
				"protocols": supportedAssetProtocols(),
			})
			if err != nil {
				return err
			}
			for _, id := range ids {
				u.localConnectable[id] = false
			}
			for _, asset := range response.Data {
				u.localConnectable[asset.ID] = asset.IsActive
			}
			ids = ids[:0]
			return nil
		}
		for i, asset := range assets {
			if _, known := u.localConnectable[asset.ID]; asset.IsActive && !known {
				ids = append(ids, asset.ID)
			}
			if len(ids) == assetFilterBatchSize || i == len(assets)-1 && len(ids) > 0 {
				if err := lookup(); err != nil {
					logger.Errorf("Get local asset protocol support failed: %s", err)
					u.loadErr = err
					return nil
				}
			}
		}
	}
	filtered := make([]model.PermAsset, 0, len(assets))
	for _, asset := range assets {
		if asset.IsActive && (u.h == nil || u.h.jmsService == nil || u.localConnectable[asset.ID]) {
			filtered = append(filtered, asset)
		}
	}
	return filtered
}

func (u *UserSelectHandler) markConnectableAssets(assets []model.PermAsset) {
	u.connectable = make([]bool, len(assets))
	for i := range assets {
		u.connectable[i] = true
	}
}
