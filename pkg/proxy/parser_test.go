package proxy

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestSplitArr(t *testing.T) {
	var s0 = "abcdefghijk"
	var s1 = make([]string, 0)
	i0 := strings.Index(s0, "a")
	fmt.Println(append(s1, s0[:i0], s0[i0+len("a"):]))
}

func TestTabKey(t *testing.T) {

	var b = []byte(`12321 \rb79789 \r\n32789732 \rklsj \r\nklfd \rb78937289`)
	b_len := len(b)
	b_copy := make([]byte, b_len)
	copy(b_copy, b)
	currentIndex := 0
	for {
		i1 := bytes.Index(b_copy[currentIndex:], []byte(" \\r\\n"))
		i0 := bytes.Index(b_copy[currentIndex:], []byte(" \\r"))
		if i0 == -1 {
			break
		}

		if i0 != i1 {
			// 匹配字节：' \r'
			// 命令超过一行的情况下，服务器端返回的命令会被截断并截断处插入' \r'
			b_copy = append(b_copy[:currentIndex+i0], b_copy[currentIndex+i0+len([]byte(" \\r")):]...)
			currentIndex = currentIndex + i0
		} else {
			// 匹配字节： ' \r\n'
			// 批量粘贴命令的情况下会出现回车换行符
			currentIndex = currentIndex + i1 + len([]byte(" \\r\\n"))
		}

		if currentIndex >= b_len {
			break
		}
	}
	fmt.Println(string(b))
	fmt.Println(string(b_copy))
}
