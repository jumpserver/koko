package agenthttp

import "testing"

func TestSessionRoute(t *testing.T) {
	for path, expected := range map[string][]string{
		"/koko/agent/sessions/session-1/messages": {"session-1", "messages"},
		"/koko/agent/sessions/session-1":          {"session-1"},
		"/koko/agent/sessions/session-1/":         nil,
		"/koko/agent/other":                       nil,
	} {
		actual, ok := sessionRoute(path)
		if expected == nil {
			if ok {
				t.Errorf("sessionRoute(%q) unexpectedly matched %v", path, actual)
			}
			continue
		}
		if !ok || len(actual) != len(expected) {
			t.Errorf("sessionRoute(%q) = %v, %v", path, actual, ok)
			continue
		}
		for index := range expected {
			if actual[index] != expected[index] {
				t.Errorf("sessionRoute(%q) = %v, want %v", path, actual, expected)
				break
			}
		}
	}
}
