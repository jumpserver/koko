package common

import (
	"strings"
	"unicode/utf8"

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

// NormalizeCharset maps aliases used by platform / connect options to internal names.
func NormalizeCharset(charset string) string {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8":
		return UTF8
	case "gbk":
		return GBK
	case "gb2312":
		return GB2312
	case "ios-8859-1", "iso-8859-1", "latin1", "ascii":
		return ISOLatin1
	default:
		return charset
	}
}

// ResolveConnectCharset matches SSH session charset resolution:
// platform charset, optionally overridden by connect options.
func ResolveConnectCharset(platformCharset string, override *string) string {
	charset := NormalizeCharset(platformCharset)
	if override == nil {
		return charset
	}
	switch strings.ToLower(strings.TrimSpace(*override)) {
	case "utf-8", "utf8":
		return UTF8
	case "gbk":
		return GBK
	case "gb2312":
		return GB2312
	case "ios-8859-1", "iso-8859-1", "latin1", "ascii":
		return ISOLatin1
	default:
		return charset
	}
}

// ConvertByCharset converts a path or filename between UTF-8 and the remote charset.
// encode=true converts UTF-8 to the remote charset; encode=false does the reverse.
// UTF-8 and unknown charsets are returned unchanged. Conversion errors keep the original.
func ConvertByCharset(s, charset string, encode bool) string {
	if s == "" {
		return s
	}
	charset = NormalizeCharset(charset)
	if charset == UTF8 {
		return s
	}
	var tr transform.Transformer
	if encode {
		tr = LookupCharsetEncode(charset)
	} else {
		tr = LookupCharsetDecode(charset)
	}
	if tr == nil {
		return s
	}
	result, _, err := transform.String(tr, s)
	if err != nil || result == "" {
		return s
	}
	if !encode && !utf8.ValidString(result) {
		return s
	}
	return result
}

// DecodeRemoteName turns a remote filename into UTF-8 for the web UI.
// Valid UTF-8 is left untouched so a GBK-configured host with a UTF-8
// filesystem does not get its names corrupted.
func DecodeRemoteName(raw, charset string) string {
	if raw == "" || utf8.ValidString(raw) {
		return raw
	}
	tried := make(map[string]struct{}, 2)
	for _, candidate := range []string{NormalizeCharset(charset), GBK} {
		if candidate == UTF8 {
			continue
		}
		if _, ok := tried[candidate]; ok {
			continue
		}
		tried[candidate] = struct{}{}
		decoded := ConvertByCharset(raw, candidate, false)
		if decoded != raw && utf8.ValidString(decoded) {
			return decoded
		}
	}
	return raw
}

// EncodeRemoteName converts a UTF-8 name back to the remote charset.
// fallbackGBK covers SSH assets whose platform charset is still utf-8
// but whose filenames were recovered as GBK.
func EncodeRemoteName(name, charset string, fallbackGBK bool) string {
	if name == "" || !utf8.ValidString(name) {
		return name
	}
	normalized := NormalizeCharset(charset)
	if normalized != UTF8 {
		return ConvertByCharset(name, normalized, true)
	}
	if fallbackGBK {
		return ConvertByCharset(name, GBK, true)
	}
	return name
}
