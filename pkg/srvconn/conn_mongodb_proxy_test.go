package srvconn

import (
	"strings"
	"testing"
)

func TestMongoProxyValidationDoesNotEchoCredentials(t *testing.T) {
	for _, value := range []string{
		"http://user:password@127.0.0.1:3128",
		"http://127.0.0.1:3128/?token=secret",
		"http://127.0.0.1:3128/#secret",
		"http://127.0.0.1:3128/%zz",
	} {
		_, err := newMongoProxyDialer(value)
		if err == nil || err.Error() != errInvalidMongoProxyURL.Error() ||
			strings.Contains(err.Error(), value) || strings.Contains(err.Error(), "secret") ||
			strings.Contains(err.Error(), "password") {
			t.Fatalf("proxy=%q error=%v", value, err)
		}
	}
	if _, err := newMongoProxyDialer("http://127.0.0.1:3128"); err != nil {
		t.Fatal(err)
	}
}
