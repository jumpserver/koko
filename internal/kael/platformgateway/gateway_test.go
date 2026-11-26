package platformgateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

func TestResolveRequestRequiresExactSnapshotDefinition(t *testing.T) {
	version := &registry{Hash: "version-1", LoadedAt: time.Now(), Operations: map[string]operation{"assets_assets_list": {ID: "assets_assets_list", Method: "GET", Path: "/api/v1/assets/assets/"}}}
	gateway := &Gateway{config: Config{AllowedMethods: map[string]bool{"GET": true}}, current: version, versions: map[string]*registry{version.Hash: version}}
	principal := domain.Principal{SubjectID: "user-1", OrganizationID: "org-1", Permissions: []string{"assets.view_asset"}}
	registration := registrationsFor(version, "platform.asset")[0]
	request := ports.CapabilityRequest{Principal: principal, Profile: "platform.asset", Registration: registration, Arguments: json.RawMessage(`{"query":"assets","progress":"search","action":"inspect"}`)}
	if _, _, _, err := gateway.resolveRequest(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Registration.DefinitionDigest = "tampered"
	if _, _, _, err := gateway.resolveRequest(context.Background(), request); err == nil {
		t.Fatal("tampered capability definition was accepted")
	}
}

func TestBuildRequestUsesOpenAPIQuerySerialization(t *testing.T) {
	operation := operation{
		Method: "GET",
		Path:   "/api/v1/assets/assets/",
		QueryParams: []parameter{
			{Name: "tags", Style: "form", Schema: map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			{Name: "filter", Style: "deepObject", Schema: map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}},
		},
	}
	path, query, _, err := buildRequest(operation, map[string]any{"query_params": map[string]any{"tags": []any{"linux", "prod"}, "filter": map[string]any{"name": "web"}}})
	if err != nil {
		t.Fatal(err)
	}
	if path != operation.Path || query != "filter%5Bname%5D=web&tags=linux%2Cprod" {
		t.Fatalf("unexpected request binding: %s?%s", path, query)
	}
}

func TestSensitiveFieldsAndTextAreBlockedOrRedacted(t *testing.T) {
	if !containsSensitive(map[string]any{"database_password": "value"}) {
		t.Fatal("derived sensitive key was accepted")
	}
	if containsSensitive(map[string]any{"password_provided": true}) {
		t.Fatal("safe presence marker was rejected")
	}
	value := sanitize(map[string]any{"authorization_header": "Bearer abcdefghijklmnop", "message": "token=abcdefghijklmnop"}, 0).(map[string]any)
	if value["authorization_header"] != "[REDACTED]" || value["message"] == "token=abcdefghijklmnop" {
		t.Fatal("sensitive response was not redacted")
	}
}
