package sshx11

import (
	"context"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
)

type testValueContext struct {
	context.Context
	values map[interface{}]interface{}
}

func (c *testValueContext) SetValue(key, value interface{}) {
	c.values[key] = value
}

func (c *testValueContext) Value(key interface{}) interface{} {
	if value, ok := c.values[key]; ok {
		return value
	}
	return c.Context.Value(key)
}

func TestRequestRoundTrip(t *testing.T) {
	want := Request{
		SingleConnection: true,
		AuthProtocol:     "MIT-MAGIC-COOKIE-1",
		AuthCookie:       "0123456789abcdef",
		ScreenNumber:     7,
	}

	got, err := ParseRequest(want.Marshal())
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if got != want {
		t.Fatalf("ParseRequest() = %#v, want %#v", got, want)
	}
}

func TestParseRequestRejectsInvalidPayload(t *testing.T) {
	if _, err := ParseRequest([]byte{1, 2, 3}); err == nil {
		t.Fatal("ParseRequest() accepted an invalid payload")
	}
}

func TestPlatformEnabledRequiresBooleanTrue(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]interface{}
		want bool
	}{
		{name: "missing metadata", meta: nil, want: false},
		{name: "missing field", meta: map[string]interface{}{}, want: false},
		{name: "false", meta: map[string]interface{}{PlatformMetaKey: false}, want: false},
		{name: "true", meta: map[string]interface{}{PlatformMetaKey: true}, want: true},
		{name: "string true", meta: map[string]interface{}{PlatformMetaKey: "true"}, want: false},
		{name: "numeric one", meta: map[string]interface{}{PlatformMetaKey: 1}, want: false},
		{name: "nil", meta: map[string]interface{}{PlatformMetaKey: nil}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := model.Platform{MetaData: tt.meta}
			if got := PlatformEnabled(platform); got != tt.want {
				t.Fatalf("PlatformEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestContext(t *testing.T) {
	ctx := &testValueContext{
		Context: context.Background(),
		values:  make(map[interface{}]interface{}),
	}
	want := Request{
		AuthProtocol: "MIT-MAGIC-COOKIE-1",
		AuthCookie:   "cookie",
	}

	if _, ok := RequestFromContext(ctx); ok {
		t.Fatal("RequestFromContext() found a request before SetRequest()")
	}
	SetRequest(ctx, want)
	got, ok := RequestFromContext(ctx)
	if !ok {
		t.Fatal("RequestFromContext() did not find the stored request")
	}
	if got != want {
		t.Fatalf("RequestFromContext() = %#v, want %#v", got, want)
	}
}
