package utils

import (
	"fmt"
	"testing"
)

func TestWrapperString(t *testing.T) {
	s := "Hello world"
	s1 := WrapperString(s, Red, true)
	fmt.Println(s1)

	s2 := WrapperString(s, Green)
	fmt.Println(s2)

	s3 := WrapperTitle(s)
	fmt.Println(s3)

	s4 := WrapperWarn(s)
	fmt.Println(s4)
}
