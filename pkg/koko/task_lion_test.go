package koko

import (
	"reflect"
	"testing"
)

func TestMergeSessionIDs(t *testing.T) {
	got := mergeSessionIDs(
		[]string{"koko-1", "shared"},
		[]string{"lion-1", "shared"},
	)
	want := []string{"koko-1", "shared", "lion-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeSessionIDs() = %v, want %v", got, want)
	}
}
