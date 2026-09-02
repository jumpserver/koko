package sessiontools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	ToolListDirectory = "list_directory"
	ToolStat          = "stat"
	ToolReadText      = "read_text"
	ToolSaveText      = "save_text"
	ToolMkdir         = "mkdir"
	ToolRename        = "rename"
	ToolDelete        = "delete"

	MaxDirectoryEntries   = 200
	MaxTextBytes          = 32 * 1024
	ExpectedVersionAbsent = "absent"
)

type FileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Size    int64  `json:"size"`
	Perm    string `json:"perm"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
	Version string `json:"version"`
}

type DirectoryResult struct {
	Path      string      `json:"path"`
	Entries   []FileEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

type TextResult struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Content   string `json:"content"`
	Version   string `json:"version"`
	Truncated bool   `json:"truncated,omitempty"`
}

// FileExecutor is bound to the already authorized SFTP resource. It must not
// open an untracked connection or bypass the active session's permissions.
type FileExecutor interface {
	ListDirectory(context.Context, string, int) (DirectoryResult, error)
	Stat(context.Context, string) (FileEntry, error)
	ReadText(context.Context, string, int64) (TextResult, error)
	SaveText(context.Context, string, string, string) (FileEntry, error)
	Mkdir(context.Context, string) error
	Rename(context.Context, string, string, string) error
	Delete(context.Context, string, string, bool) error
}

type fileAction struct {
	Tool            string
	Path            string
	DestinationPath string
	Content         string
	ExpectedVersion string
	Recursive       bool
}

func prepareFileAction(action fileAction) (fileAction, error) {
	path, err := cleanToolPath(action.Path)
	if err != nil {
		return action, err
	}
	if isFileWriteTool(action.Tool) && (path == "." || path == "/") {
		return action, fmt.Errorf("refusing to modify the file root")
	}
	action.Path = path
	if action.Tool == ToolRename {
		destination := strings.TrimSpace(action.DestinationPath)
		if filepath.Base(destination) == destination {
			destination = filepath.Join(filepath.Dir(path), destination)
		}
		destination, err = cleanToolPath(destination)
		if err != nil {
			return action, fmt.Errorf("invalid rename destination: %w", err)
		}
		if filepath.Clean(filepath.Dir(destination)) != filepath.Clean(filepath.Dir(path)) ||
			destination == path {
			return action, fmt.Errorf("rename destination must be a different name in the same directory")
		}
		action.DestinationPath = destination
	}
	if action.Tool == ToolSaveText {
		if len(action.Content) > MaxTextBytes {
			return action, fmt.Errorf("file content exceeds the tool text limit")
		}
		if !utf8.ValidString(action.Content) || strings.IndexByte(action.Content, 0) >= 0 {
			return action, fmt.Errorf("save_text accepts UTF-8 text only")
		}
	}
	return action, nil
}

func cleanToolPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("file path is required")
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("file path must be absolute")
	}
	for _, part := range strings.Split(filepath.ToSlash(value), "/") {
		if part == ".." {
			return "", fmt.Errorf("parent path segments are not allowed")
		}
	}
	return filepath.Clean(value), nil
}

func isFileWriteTool(tool string) bool {
	switch tool {
	case ToolSaveText, ToolMkdir, ToolRename, ToolDelete:
		return true
	default:
		return false
	}
}
