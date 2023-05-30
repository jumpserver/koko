package model

type PaginationResponse struct {
	Total       int     `json:"count"`
	NextURL     string  `json:"next"`
	PreviousURL string  `json:"previous"`
	Data        []Asset `json:"results"`
}

type PaginationParam struct {
	PageSize int
	Offset   int
	Searches []string
	Refresh  bool

	Order    string
	Category string
	Type     string
	IsActive bool

	Protocols []string
}
