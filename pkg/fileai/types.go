package fileai

import (
	"context"

	"github.com/jumpserver/koko/pkg/terminalai"
	"github.com/jumpserver/koko/pkg/terminalai/provider"
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

type ChatMessage = terminalai.ChatMessage
type ChatPart = terminalai.ChatPart

type Entry struct {
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
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated,omitempty"`
}

type TextResult struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Content   string `json:"content"`
	Version   string `json:"version"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Executor is the connection-bound file operation boundary. Implementations
// must preserve the authorization and audit behavior of the active SFTP
// session rather than opening a second, untracked connection.
type Executor interface {
	ListDirectory(context.Context, string, int) (DirectoryResult, error)
	Stat(context.Context, string) (Entry, error)
	ReadText(context.Context, string, int64) (TextResult, error)
	SaveText(context.Context, string, string, string) (Entry, error)
	Mkdir(context.Context, string) error
	Rename(context.Context, string, string) error
	Delete(context.Context, string) error
}

type SessionOptions struct {
	Config   terminalai.Config
	Executor Executor
	Language string
	Emit     func(ChatMessage)
}

type Session interface {
	Handle(ChatMessage) error
	ProviderInfo() provider.ProviderInfo
	Cancel()
	Close()
}

type Action struct {
	ID              string `json:"id,omitempty"`
	Tool            string `json:"tool"`
	Path            string `json:"path"`
	DestinationPath string `json:"destinationPath"`
	Content         string `json:"content"`
	ExpectedVersion string `json:"expectedVersion"`
	Recursive       bool   `json:"recursive"`
	Rationale       string `json:"rationale"`
}

type Decision struct {
	Kind    string   `json:"kind"`
	Answer  string   `json:"answer"`
	Summary string   `json:"summary"`
	Plan    []string `json:"plan"`
	Action  Action   `json:"action"`
}

type ActionResult struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Path    string `json:"path"`
	Outcome string `json:"outcome"`
	Summary string `json:"summary"`
	Error   string `json:"error,omitempty"`
	Details any    `json:"details,omitempty"`
}
