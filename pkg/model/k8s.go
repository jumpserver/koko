package model

type K8sCluster struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Cluster string `json:"cluster"`
	Comment string `json:"comment"`
	Type    string `json:"type"` // k8s
	OrgID   string `json:"org_id"`
}

type K8sClustersPaginationResponse struct {
	Total       int          `json:"count"`
	NextURL     string       `json:"next"`
	PreviousURL string       `json:"previous"`
	Data        []K8sCluster `json:"results"`
}
