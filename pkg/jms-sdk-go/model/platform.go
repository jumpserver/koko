package model

type Platform struct {
	BaseOs   string                 `json:"base"`
	MetaData map[string]interface{} `json:"meta"`

	ID   int    `json:"id"`
	Name string `json:"name"`

	Protocols     []Protocol   `json:"protocols"`
	Category      Category     `json:"category"`
	Charset       Charset      `json:"charset"`
	Type          PlatformType `json:"type"`
	SuEnabled     bool         `json:"su_enabled"`
	SuMethod      string       `json:"su_method"`
	DomainEnabled bool         `json:"domain_enabled"`
	Comment       string       `json:"comment"`
}

type Protocol struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
	Port int    `json:"port"`
}

type LabelValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Action LabelValue

type Charset LabelValue

type Category LabelValue

type PlatformType LabelValue
