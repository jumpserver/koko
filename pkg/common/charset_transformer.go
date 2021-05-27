package common

import (
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	UTF8 = "utf8"
	GBK  = "gbk"
)

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
