package sessiontools

func commandOutputSchema() map[string]any {
	return objectOutputSchema(
		map[string]any{
			"command":          map[string]any{"type": "string"},
			"execution":        map[string]any{"type": "string", "enum": []string{MCPExecutionPTY, MCPExecutionBackground}},
			"output":           map[string]any{"type": "string"},
			"output_truncated": map[string]any{"type": "boolean"},
			"exit_code":        map[string]any{"type": "integer"},
			"command_acl": objectOutputSchema(map[string]any{
				"action":     map[string]any{"type": "string"},
				"acl_id":     map[string]any{"type": "string"},
				"item_id":    map[string]any{"type": "string"},
				"name":       map[string]any{"type": "string"},
				"matched":    map[string]any{"type": "string"},
				"detail_url": map[string]any{"type": "string"},
				"reviewers":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"processor":  map[string]any{"type": "string"},
				"reviewed":   map[string]any{"type": "boolean"},
			}, "action"),
		},
		"command", "execution", "output",
	)
}

func terminalContextOutputSchema() map[string]any {
	return objectOutputSchema(map[string]any{
		"resource_session_id": map[string]any{"type": "string"},
		"terminal_id":         map[string]any{"type": "integer"},
		"context": objectOutputSchema(map[string]any{
			"protocol":          map[string]any{"type": "string"},
			"connection_method": map[string]any{"type": "string"},
			"asset_id":          map[string]any{"type": "string"},
			"asset_name":        map[string]any{"type": "string"},
			"asset_address":     map[string]any{"type": "string"},
			"platform_id":       map[string]any{"type": "integer"},
			"platform_category": map[string]any{"type": "string"},
			"platform_type":     map[string]any{"type": "string"},
			"platform_name":     map[string]any{"type": "string"},
			"base_os":           map[string]any{"type": "string"},
			"charset":           map[string]any{"type": "string"},
			"database":          map[string]any{"type": "string"},
		}, "protocol"),
	}, "resource_session_id", "terminal_id", "context")
}

func terminalSnapshotOutputSchema() map[string]any {
	return objectOutputSchema(map[string]any{
		"content":   map[string]any{"type": "string"},
		"max_bytes": map[string]any{"type": "integer", "minimum": 1},
	}, "content", "max_bytes")
}

func databaseSchemaOutputSchema() map[string]any {
	column := objectOutputSchema(map[string]any{
		"name":     map[string]any{"type": "string"},
		"type":     map[string]any{"type": "string"},
		"nullable": map[string]any{"type": "boolean"},
		"default":  map[string]any{"type": []string{"string", "null"}},
	}, "name", "type", "nullable", "default")
	table := objectOutputSchema(map[string]any{
		"database": map[string]any{"type": "string"},
		"schema":   map[string]any{"type": "string"},
		"table":    map[string]any{"type": "string"},
		"columns":  map[string]any{"type": "array", "items": column},
	}, "database", "table", "columns")
	return objectOutputSchema(map[string]any{
		"database":  map[string]any{"type": "string"},
		"matches":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"tables":    map[string]any{"type": "array", "items": table},
		"truncated": map[string]any{"type": "boolean"},
	}, "database", "tables")
}

func fileToolOutputSchema(name string) map[string]any {
	entry := fileEntryOutputSchema()
	switch name {
	case ToolListDirectory:
		return objectOutputSchema(map[string]any{
			"path":      map[string]any{"type": "string"},
			"entries":   map[string]any{"type": "array", "items": entry},
			"truncated": map[string]any{"type": "boolean"},
		}, "path", "entries")
	case ToolStat, ToolSaveText:
		return entry
	case ToolReadText:
		return objectOutputSchema(map[string]any{
			"path":      map[string]any{"type": "string"},
			"exists":    map[string]any{"type": "boolean"},
			"content":   map[string]any{"type": "string"},
			"version":   map[string]any{"type": "string"},
			"truncated": map[string]any{"type": "boolean"},
		}, "path", "exists", "content", "version")
	case ToolMkdir:
		return objectOutputSchema(map[string]any{
			"path": map[string]any{"type": "string"}, "created": map[string]any{"type": "boolean"},
		}, "path", "created")
	case ToolRename:
		return objectOutputSchema(map[string]any{
			"path": map[string]any{"type": "string"}, "destination_path": map[string]any{"type": "string"},
		}, "path", "destination_path")
	case ToolDelete:
		return objectOutputSchema(map[string]any{
			"path": map[string]any{"type": "string"}, "deleted": map[string]any{"type": "boolean"},
		}, "path", "deleted")
	default:
		return objectOutputSchema(nil)
	}
}

func fileEntryOutputSchema() map[string]any {
	return objectOutputSchema(map[string]any{
		"name":    map[string]any{"type": "string"},
		"path":    map[string]any{"type": "string"},
		"exists":  map[string]any{"type": "boolean"},
		"size":    map[string]any{"type": "integer"},
		"perm":    map[string]any{"type": "string"},
		"modTime": map[string]any{"type": "integer"},
		"isDir":   map[string]any{"type": "boolean"},
		"version": map[string]any{"type": "string"},
	}, "name", "path", "exists", "size", "perm", "modTime", "isDir", "version")
}

func objectOutputSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	result := map[string]any{
		"type": "object", "additionalProperties": false, "properties": properties,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}
