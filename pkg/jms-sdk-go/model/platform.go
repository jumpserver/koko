package model

type Platform struct {
	Name     string                 `json:"name"`
	BaseOs   string                 `json:"base"`
	Charset  string                 `json:"charset"`
	MetaData map[string]interface{} `json:"meta"`
}
