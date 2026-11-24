package engine

import "net/http"

const aiRevision = 1
const aiMaxSQL = 128 << 10

type aiFrame struct {
	Type              string `json:"type"`
	Version           int    `json:"version"`
	ResourceSessionID string `json:"resource_session_id"`
	Data              any    `json:"data"`
}

func aiManifest(s *session) aiFrame {
	str := func(n int) map[string]any { return map[string]any{"type": "string", "maxLength": n} }
	requiredSQL := str(aiMaxSQL)
	requiredSQL["minLength"] = 1
	tool := func(name, title, description string, properties map[string]any, required []string, open bool) map[string]any {
		schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
		if len(required) > 0 {
			schema["required"] = required
		}
		return map[string]any{"name": name, "title": title, "description": description, "inputSchema": schema, "annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": open}}
	}
	read := tool("read_sql_context", "Read SQL editor context", "Read the verified SQL editor context, including the selected table when a table tab is open. Never reads business rows.", map[string]any{}, nil, false)
	search := tool("search_schema", "Find related tables", "Find bounded table candidates by table and column names/comments in the verified schema. Never reads business rows.", map[string]any{"query": str(256), "schema": str(256), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 10}}, []string{"query"}, false)
	inspect := tool("inspect_schema", "Inspect database schema", "In a table tab, omit query and tables to inspect only the selected table. Prefer this for questions about the current table or its structure. Inspect up to eight named tables with columns, keys and indexes. Use query=* only for explicit database-wide discovery. Never reads business rows.", map[string]any{"query": str(1024), "schema": str(1024), "tables": map[string]any{"type": "array", "minItems": 1, "maxItems": 8, "items": str(1024)}}, nil, true)
	validate := tool("validate_sql", "Validate SQL draft", "Check draft SQL syntax locally. The validationStatus is valid, invalid, or unverified when dialect validation is incomplete. A syntax check does not guarantee safety or permission. Never executes SQL.", map[string]any{"sql": requiredSQL}, []string{"sql"}, false)
	proposal := tool("propose_sql", "Propose SQL draft", "Prepare a SQL draft for explicit user review, including write statements. Never applies or executes SQL. Console drafts may contain native syntax Chen cannot validate.", map[string]any{"sql": requiredSQL, "explanation": str(4096)}, []string{"sql", "explanation"}, false)
	proposal["_meta"] = map[string]any{"com.jumpserver/finalResult": true}
	data := map[string]any{"version": 1, "resource_session_id": s.id, "profile": "sql", "revision": aiRevision, "context": map[string]any{"session_kind": "sql_editor", "supports_selected_table": true, "interaction_mode": "draft_only", "command_language": "sql", "dialect": s.grant.Config.Dialect, "protocol": s.grant.Config.Dialect, "asset_id": s.grant.AssetID, "asset_name": s.grant.AssetName, "database": s.grant.Config.Database}, "tools": []any{read, search, inspect, validate, proposal}}
	return aiFrame{"mcp.manifest", 1, s.id, data}
}
func (v *Server) aiManifestHTTP(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	if v.opts.SingleMode {
		fail(w, 404, "ai_unavailable", "AI requires a managed session")
		return
	}
	writeJSON(w, 200, aiManifest(s))
}
