package common

import (
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	UTF8      = "utf8"
	GBK       = "gbk"
	GB2312    = "gb2312"
	ISOLatin1 = "ios-8859-1"
)

func LookupCharsetDecode(charset string) transform.Transformer {
	switch charset {
	case GBK, GB2312:
		return simplifiedchinese.GBK.NewDecoder()
	case ISOLatin1:
		return charmap.ISO8859_1.NewDecoder()

	}
	return nil
}
func LookupCharsetEncode(charset string) transform.Transformer {
	switch charset {
	case GBK, GB2312:
		return simplifiedchinese.GBK.NewEncoder()
	case ISOLatin1:
		return charmap.ISO8859_1.NewEncoder()

	}
	return nil
}
