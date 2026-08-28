package srvconn

import "testing"

func TestFileAIPathWithinRoot(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/", want: true},
		{path: "/tmp", want: true},
		{path: "/tmp/x.txt", want: true},
		{path: "x.txt", want: true},
		{path: "/tmp2/x.txt", want: false},
		{path: "/a.txt", want: false},
		{path: "/root/x.txt", want: false},
		{path: "/tmp/../root/x.txt", want: false},
		{path: "../root/x.txt", want: false},
	}
	for _, test := range tests {
		if got := fileAIPathWithinRoot("/tmp", test.path); got != test.want {
			t.Errorf("fileAIPathWithinRoot(/tmp, %q) = %v", test.path, got)
		}
	}
}
