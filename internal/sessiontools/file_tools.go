package sessiontools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type mcpToolHandler struct {
	name     string
	executor FileExecutor
}

type FileToolCapabilities struct {
	ReadText bool
	SaveText bool
	Mkdir    bool
	Rename   bool
	Delete   bool
}

type mcpPathArguments struct {
	Path string `json:"path"`
}

type mcpListArguments struct {
	Path  string `json:"path"`
	Limit int    `json:"limit,omitempty"`
}

type mcpReadArguments struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type mcpSaveArguments struct {
	Path            string `json:"path"`
	Content         string `json:"content"`
	ExpectedVersion string `json:"expected_version"`
}

type mcpRenameArguments struct {
	Path            string `json:"path"`
	DestinationPath string `json:"destination_path"`
	ExpectedVersion string `json:"expected_version"`
}

type mcpDeleteArguments struct {
	Path            string `json:"path"`
	ExpectedVersion string `json:"expected_version"`
	Recursive       bool   `json:"recursive,omitempty"`
}

func NewFileToolHandlers(
	executor FileExecutor,
	capabilities FileToolCapabilities,
) ([]MCPToolHandler, error) {
	if executor == nil {
		return nil, errors.New("file MCP executor is required")
	}
	names := []string{ToolListDirectory, ToolStat}
	if capabilities.ReadText {
		names = append(names, ToolReadText)
	}
	if capabilities.SaveText {
		names = append(names, ToolSaveText)
	}
	if capabilities.Mkdir {
		names = append(names, ToolMkdir)
	}
	if capabilities.Rename {
		names = append(names, ToolRename)
	}
	if capabilities.Delete {
		names = append(names, ToolDelete)
	}
	handlers := make([]MCPToolHandler, 0, len(names))
	for _, name := range names {
		handlers = append(handlers, &mcpToolHandler{name: name, executor: executor})
	}
	return handlers, nil
}

func (h *mcpToolHandler) Definition() MCPToolDefinition {
	stringProperty := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	properties := map[string]any{
		"path": map[string]any{
			"type": "string", "minLength": 1, "pattern": "^/",
			"description": "Virtual absolute path inside the active SFTP resource; use / for its root",
		},
	}
	required := []string{"path"}
	description := "Inspect a path in the active SFTP resource"
	annotations := map[string]any{"readOnlyHint": true}
	switch h.name {
	case ToolListDirectory:
		description = "List bounded entries in one virtual directory through the active SFTP session"
		properties["limit"] = map[string]any{
			"type": "integer", "minimum": 1, "maximum": MaxDirectoryEntries,
			"description": "Maximum entries to return; omit to use the bounded server maximum",
		}
	case ToolStat:
		description = "Read current path metadata and version"
	case ToolReadText:
		// File contents cross the resource/model trust boundary. openWorldHint
		// keeps this read-only operation subject to the runtime's automatic approval.
		annotations["openWorldHint"] = true
		description = "Read a bounded UTF-8 text file for AI analysis through the active SFTP session"
		properties["path"] = map[string]any{
			"type": "string", "minLength": 2, "pattern": "^/.+",
			"description": "Complete virtual absolute path of one file; use the path returned by list_directory, never /",
		}
		properties["max_bytes"] = map[string]any{
			"type": "integer", "minimum": 1, "maximum": MaxTextBytes,
			"description": "Maximum UTF-8 bytes to read; omit to use the bounded server maximum",
		}
	case ToolSaveText:
		annotations["readOnlyHint"] = false
		annotations["destructiveHint"] = true
		description = "Save UTF-8 text with a mandatory version precondition"
		properties["content"] = stringProperty("Complete UTF-8 file content")
		properties["expected_version"] = stringProperty(
			"Version returned by read_text, or absent for create-only",
		)
		required = append(required, "content", "expected_version")
	case ToolMkdir:
		annotations["readOnlyHint"] = false
		annotations["destructiveHint"] = true
		description = "Create a directory only when the target is absent"
	case ToolRename:
		annotations["readOnlyHint"] = false
		annotations["destructiveHint"] = true
		description = "Rename within one directory with a mandatory version precondition"
		properties["destination_path"] = stringProperty(
			"Absolute destination in the same directory, or a single basename",
		)
		properties["expected_version"] = stringProperty("Version returned by stat")
		required = append(required, "destination_path", "expected_version")
	case ToolDelete:
		annotations["readOnlyHint"] = false
		annotations["destructiveHint"] = true
		description = "Delete a path with a mandatory version precondition"
		properties["expected_version"] = stringProperty("Version returned by stat")
		properties["recursive"] = map[string]any{
			"type":        "boolean",
			"description": "Must be true when deleting a directory; ignored for a file",
		}
		required = append(required, "expected_version")
	}
	return MCPToolDefinition{
		Name: h.name, Title: fileToolTitle(h.name),
		Description: description, Annotations: annotations,
		OutputSchema: fileToolOutputSchema(h.name),
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": properties, "required": required,
		},
	}
}

func fileToolTitle(name string) string {
	switch name {
	case ToolListDirectory:
		return "List directory"
	case ToolStat:
		return "Inspect path"
	case ToolReadText:
		return "Read text file"
	case ToolSaveText:
		return "Save text file"
	case ToolMkdir:
		return "Create directory"
	case ToolRename:
		return "Rename path"
	case ToolDelete:
		return "Delete path"
	default:
		return name
	}
}

func (h *mcpToolHandler) Call(
	ctx context.Context,
	arguments json.RawMessage,
) (any, error) {
	switch h.name {
	case ToolListDirectory:
		var args mcpListArguments
		if err := decodeMCPArguments(arguments, &args); err != nil {
			return nil, err
		}
		action, err := prepareFileAction(fileAction{Tool: h.name, Path: args.Path})
		if err != nil {
			return nil, err
		}
		if args.Limit <= 0 {
			args.Limit = MaxDirectoryEntries
		}
		if args.Limit > MaxDirectoryEntries {
			return nil, fmt.Errorf("directory limit exceeds %d", MaxDirectoryEntries)
		}
		return h.executor.ListDirectory(ctx, action.Path, args.Limit)
	case ToolStat:
		var args mcpPathArguments
		if err := decodeMCPArguments(arguments, &args); err != nil {
			return nil, err
		}
		action, err := prepareFileAction(fileAction{Tool: h.name, Path: args.Path})
		if err != nil {
			return nil, err
		}
		entry, err := h.executor.Stat(ctx, action.Path)
		if os.IsNotExist(err) {
			return FileEntry{
				Name: pathBase(action.Path), Path: action.Path,
				Exists: false, Version: ExpectedVersionAbsent,
			}, nil
		}
		return entry, err
	case ToolReadText:
		var args mcpReadArguments
		if err := decodeMCPArguments(arguments, &args); err != nil {
			return nil, err
		}
		action, err := prepareFileAction(fileAction{Tool: h.name, Path: args.Path})
		if err != nil {
			return nil, err
		}
		if args.MaxBytes <= 0 {
			args.MaxBytes = MaxTextBytes
		}
		if args.MaxBytes > MaxTextBytes {
			return nil, fmt.Errorf("text limit exceeds %d", MaxTextBytes)
		}
		result, err := h.executor.ReadText(ctx, action.Path, args.MaxBytes)
		if os.IsNotExist(err) {
			return TextResult{
				Path: action.Path, Exists: false, Version: ExpectedVersionAbsent,
			}, nil
		}
		return result, err
	case ToolSaveText:
		var args mcpSaveArguments
		if err := decodeMCPArguments(arguments, &args); err != nil {
			return nil, err
		}
		if strings.TrimSpace(args.ExpectedVersion) == "" {
			return nil, errors.New("expected_version is required")
		}
		action, err := prepareFileAction(fileAction{
			Tool: h.name, Path: args.Path, Content: args.Content,
			ExpectedVersion: args.ExpectedVersion,
		})
		if err != nil {
			return nil, err
		}
		if err = h.checkSavePrecondition(ctx, action); err != nil {
			return nil, err
		}
		return h.executor.SaveText(
			ctx, action.Path, action.Content, action.ExpectedVersion,
		)
	case ToolMkdir:
		var args mcpPathArguments
		if err := decodeMCPArguments(arguments, &args); err != nil {
			return nil, err
		}
		action, err := prepareFileAction(fileAction{Tool: h.name, Path: args.Path})
		if err != nil {
			return nil, err
		}
		if _, err = h.executor.Stat(ctx, action.Path); err == nil {
			return nil, errors.New("mkdir target already exists")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if err = h.executor.Mkdir(ctx, action.Path); err != nil {
			return nil, err
		}
		return map[string]any{"path": action.Path, "created": true}, nil
	case ToolRename:
		var args mcpRenameArguments
		if err := decodeMCPArguments(arguments, &args); err != nil {
			return nil, err
		}
		if strings.TrimSpace(args.ExpectedVersion) == "" ||
			args.ExpectedVersion == ExpectedVersionAbsent {
			return nil, errors.New("a current expected_version is required")
		}
		action, err := prepareFileAction(fileAction{
			Tool: h.name, Path: args.Path,
			DestinationPath: args.DestinationPath,
			ExpectedVersion: args.ExpectedVersion,
		})
		if err != nil {
			return nil, err
		}
		if _, err = h.executor.Stat(ctx, action.DestinationPath); err == nil {
			return nil, errors.New("rename destination already exists")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if err = h.checkVersion(ctx, action.Path, action.ExpectedVersion); err != nil {
			return nil, err
		}
		if err = h.executor.Rename(
			ctx, action.Path, action.DestinationPath, action.ExpectedVersion,
		); err != nil {
			return nil, err
		}
		return map[string]any{
			"path": action.Path, "destination_path": action.DestinationPath,
		}, nil
	case ToolDelete:
		var args mcpDeleteArguments
		if err := decodeMCPArguments(arguments, &args); err != nil {
			return nil, err
		}
		if strings.TrimSpace(args.ExpectedVersion) == "" ||
			args.ExpectedVersion == ExpectedVersionAbsent {
			return nil, errors.New("a current expected_version is required")
		}
		action, err := prepareFileAction(fileAction{
			Tool: h.name, Path: args.Path, Recursive: args.Recursive,
			ExpectedVersion: args.ExpectedVersion,
		})
		if err != nil {
			return nil, err
		}
		entry, err := h.executor.Stat(ctx, action.Path)
		if err != nil {
			return nil, err
		}
		if entry.Version != action.ExpectedVersion {
			return nil, errors.New("remote path changed before delete")
		}
		if entry.IsDir && !action.Recursive {
			return nil, errors.New("recursive=true is required to delete a directory")
		}
		if err = h.checkVersion(ctx, action.Path, action.ExpectedVersion); err != nil {
			return nil, err
		}
		if err = h.executor.Delete(
			ctx, action.Path, action.ExpectedVersion, action.Recursive,
		); err != nil {
			return nil, err
		}
		return map[string]any{"path": action.Path, "deleted": true}, nil
	default:
		return nil, fmt.Errorf("unsupported file MCP tool %q", h.name)
	}
}

func (h *mcpToolHandler) checkSavePrecondition(
	ctx context.Context,
	action fileAction,
) error {
	if action.ExpectedVersion == ExpectedVersionAbsent {
		_, err := h.executor.Stat(ctx, action.Path)
		switch {
		case err == nil:
			return errors.New("remote file already exists")
		case os.IsNotExist(err):
			return nil
		default:
			return err
		}
	}
	current, err := h.executor.ReadText(ctx, action.Path, MaxTextBytes)
	if err != nil {
		return err
	}
	if current.Truncated {
		return errors.New("refusing to edit a file whose content was truncated")
	}
	if current.Version != action.ExpectedVersion {
		return errors.New("remote file changed before save")
	}
	return nil
}

func (h *mcpToolHandler) checkVersion(
	ctx context.Context,
	path, expected string,
) error {
	entry, err := h.executor.Stat(ctx, path)
	if err != nil {
		return err
	}
	if entry.Version != expected {
		return errors.New("remote path changed before mutation")
	}
	return nil
}

func decodeMCPArguments(value json.RawMessage, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode file tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("file tool arguments have trailing data")
	}
	return nil
}

func pathBase(path string) string {
	path = strings.TrimRight(path, "/")
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}
