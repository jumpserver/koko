package handler

import (
	"strings"
	"testing"
)

func TestCompareIP(t *testing.T) {
	testIPs := [][3]string{
		{"192.168.2.2", "192.168.2.3", "true"},
		{"192.168.2.3", "172.168.1.2", "false"},
		{"10.0.2.1", "172.168.1.2", "true"},
		{"192.168.2.1", "192.168", "false"},
		{"192.168.2.1", "", "false"},
		{"192.168.2.1", "192.168.8.2", "true"},
	}
	for i := range testIPs {
		result := "false"
		if CompareIP(testIPs[i][0], testIPs[i][1]) {
			result = "true"
		}
		if !strings.EqualFold(testIPs[i][2], result) {
			t.Fatalf("test failed %v", testIPs[i])
		}
	}
}

func TestCompareString(t *testing.T) {
	testHostname := [][3]string{
		{"ass", "bb", "true"},
		{"ass", "ba", "true"},
		{"ass", "ab", "false"},
		{"ass", "as", "false"},
		{"ass", "asw", "true"},
		{"ass", "d", "true"},
	}
	for i := range testHostname {
		result := "false"
		if CompareString(testHostname[i][0], testHostname[i][1]) {
			result = "true"
		}
		if !strings.EqualFold(testHostname[i][2], result) {
			t.Fatalf("test failed %v", testHostname[i])
		}
	}
}
