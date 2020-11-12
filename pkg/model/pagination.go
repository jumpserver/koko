package model

type PaginationResponse struct {
	Total       int                      `json:"count"`
	NextURL     string                   `json:"next"`
	PreviousURL string                   `json:"previous"`
	Data        []map[string]interface{} `json:"results"`
}

type PaginationParam struct {
	PageSize int
	Offset   int
	Searches []string
}
