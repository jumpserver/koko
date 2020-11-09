package proxy

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestReplaceURLHostPort(t *testing.T) {
	ip := "127.0.0.1"
	port := 9443

	testUrls := [][2]string{
		{"http://172.168.2.2/local", fmt.Sprintf("http://%s:%d/local", ip, port)},
		{"http://172.168.2.2:80/local", fmt.Sprintf("http://%s:%d/local", ip, port)},
		{"https://192.168.1.2:8443?sad=asda", fmt.Sprintf("https://%s:%d?sad=asda", ip, port)},
		{"https://admin:admin@192.168.1.2:8443", fmt.Sprintf("https://admin:admin@%s:%d", ip, port)},
		{"https://192.168.1.2", fmt.Sprintf("https://%s:%d", ip, port)},
	}
	for i := range testUrls {
		urlObj, err := url.Parse(testUrls[i][0])
		if err != nil {
			t.Fatal(err)
		}
		hostAndPort := strings.Split(urlObj.Host, ":")
		var host string
		var httpPort string
		switch len(hostAndPort) {
		case 2:
			host = hostAndPort[0]
			httpPort = hostAndPort[1]
		default:
			host = hostAndPort[0]
			switch urlObj.Scheme {
			case "https":
				httpPort = "443"
			default:
				httpPort = "80"
			}
		}
		t.Logf("%s ==> host:%s ;  port: %s ;\n", testUrls[i][0], host, httpPort)
		if ReplaceURLHostAndPort(urlObj, ip, port) != testUrls[i][1] {
			t.Fatalf("err: %s => %s ", testUrls[i][0], testUrls[i][1])
		}

	}
}
