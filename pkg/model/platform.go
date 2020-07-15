package model

import (
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	UTF8 = "utf8"
	GBK  = "gbk"
)

type Platform struct {
	Name     string                 `json:"name"`
	BaseOs   string                 `json:"base"`
	Charset  string                 `json:"charset"`
	MetaData map[string]interface{} `json:"meta"`
}

func LookupCharsetDecode(charset string) transform.Transformer {
	switch charset {
	case GBK:
		return simplifiedchinese.GBK.NewDecoder()
	}
	return nil
}
func LookupCharsetEncode(charset string) transform.Transformer {
	switch charset {
	case GBK:
		return simplifiedchinese.GBK.NewEncoder()
	}
	return nil
}
