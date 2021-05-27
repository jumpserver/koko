package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetUserNodeAssets(userID, nodeID string,
	params model.PaginationParam) (resp model.PaginationResponse, err error) {
	Url := fmt.Sprintf(UserPermsNodeAssetsListURL, userID, nodeID)
	return s.getPaginationResult(Url, params)
}

func (s *JMService) GetUserNodes(userId string) (nodes model.NodeList, err error) {
	Url := fmt.Sprintf(UserPermsNodesListURL, userId)
	_, err = s.authClient.Get(Url, &nodes)
	return
}

func (s *JMService) RefreshUserNodes(userId string) (nodes model.NodeList, err error) {
	params := map[string]string{
		"rebuild_tree": "1",
	}
	Url := fmt.Sprintf(UserPermsNodesListURL, userId)
	_, err = s.authClient.Get(Url, &nodes, params)
	return
}
