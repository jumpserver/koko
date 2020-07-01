package model

import (
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

const (
	UTF8 = "utf8"
	GBK = "gbk"
	GB18030 = "gb18030"
	GB2312 = "gb2312"
	BIG5 = "big5"
)

type Platform struct {
	Id string
	Name string
	BaseOs string
	Charset string
	MetaData map[string]interface{}
}

func LookupCharsetDecode(charset string) transform.Transformer{
	switch charset {
	case GBK:
		return simplifiedchinese.GBK.NewDecoder()
	case GB18030:
		return simplifiedchinese.GB18030.NewDecoder()
	case GB2312:
		return simplifiedchinese.HZGB2312.NewDecoder()
	case BIG5:
		return traditionalchinese.Big5.NewDecoder()
	}
	return nil
}
func LookupCharsetEncode(charset string) transform.Transformer{
	switch charset {
	case GBK:
		return simplifiedchinese.GBK.NewEncoder()
	case GB18030:
		return simplifiedchinese.GB18030.NewEncoder()
	case GB2312:
		return simplifiedchinese.HZGB2312.NewEncoder()
	case BIG5:
		return traditionalchinese.Big5.NewEncoder()
	}
	return nil
}