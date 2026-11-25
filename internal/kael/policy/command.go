package policy

import (
	"encoding/json"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/kael/domain"
	"mvdan.cc/sh/v3/syntax"
)

const (
	CommandPolicyMetaKey = "com.jumpserver/commandPolicy"
	ShellReadOnlyPolicy  = "shell-readonly-v1"
)

// InvocationPolicy refines an opted-in terminal registration using the complete
// command, never a model-supplied risk label or a tool name. Other calls retain
// their registered policy, including when syntax or arguments are unknown.
func InvocationPolicy(registration domain.Registration, arguments json.RawMessage) (string, bool) {
	annotations := registration.Annotations()
	risk := registration.Risk
	if risk == "" {
		risk, _ = RegistrationPolicy(annotations)
	}
	if registration.BindingKind == "panel" && registration.Namespace == "luna.terminal" &&
		annotations.CommandPolicy == ShellReadOnlyPolicy && !annotations.Destructive && !annotations.FinalResult {
		var args struct {
			Command        string `json:"command"`
			Execution      string `json:"execution"`
			TimeoutSeconds int    `json:"timeout_seconds"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(arguments)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&args) == nil && json.Valid(arguments) && readOnlyShell(args.Command) {
			return "read", false
		}
	}
	return risk, registration.RequiresConfirmation
}

func readOnlyShell(command string) bool {
	if len(command) == 0 || len(command) > 64*1024 || !utf8.ValidString(command) || strings.IndexFunc(command, unicode.IsControl) >= 0 {
		return false
	}
	// Interactive shells differ in comment and history expansion behavior.
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true)).Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) == 0 {
		return false
	}
	safe := true
	syntax.Walk(file, func(node syntax.Node) bool {
		if !safe {
			return false
		}
		switch value := node.(type) {
		case nil, *syntax.File:
		case *syntax.Stmt:
			safe = value.Cmd != nil && !value.Background && !value.Coprocess && !value.Disown
		case *syntax.BinaryCmd:
			safe = value.Op == syntax.Pipe || value.Op == syntax.PipeAll || value.Op == syntax.AndStmt || value.Op == syntax.OrStmt
		case *syntax.CallExpr:
			safe = readOnlyCall(value)
			return false // All words are checked by readOnlyCall.
		case *syntax.Redirect:
			target, glob, ok := literalWord(value.Word)
			fd := ""
			if value.N != nil {
				fd = value.N.Value
			}
			safe = ok && !glob && value.Hdoc == nil && (fd == "" || fd == "1" || fd == "2") &&
				(value.Op == syntax.RdrOut && target == "/dev/null" || value.Op == syntax.DplOut && (target == "1" || target == "2"))
			return false
		default:
			safe = false
		}
		return safe
	})
	return safe
}

// literalWord only accepts literal text and ordinary quotes. Expansions, escape
// processing and unknown shell constructs require approval; nothing is evaluated.
func literalWord(word *syntax.Word) (string, bool, bool) {
	if word == nil {
		return "", false, false
	}
	var result strings.Builder
	glob := false
	for _, part := range word.Parts {
		switch value := part.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(value.Value, "\\{}~!") {
				return "", false, false
			}
			glob = glob || strings.ContainsAny(value.Value, "*?[")
			result.WriteString(value.Value)
		case *syntax.SglQuoted:
			if value.Dollar {
				return "", false, false
			}
			result.WriteString(value.Value)
		case *syntax.DblQuoted:
			if value.Dollar {
				return "", false, false
			}
			for _, quoted := range value.Parts {
				literal, ok := quoted.(*syntax.Lit)
				if !ok || strings.ContainsAny(literal.Value, "\\!") {
					return "", false, false
				}
				result.WriteString(literal.Value)
			}
		default:
			return "", false, false
		}
	}
	return result.String(), glob, true
}

func readOnlyCall(call *syntax.CallExpr) bool {
	if len(call.Assigns) != 0 || len(call.Args) == 0 {
		return false
	}
	args := make([]string, len(call.Args))
	anyGlob := false
	for i, word := range call.Args {
		text, glob, ok := literalWord(word)
		if !ok || i == 0 && glob {
			return false
		}
		args[i], anyGlob = text, anyGlob || glob
	}
	name := args[0]
	if strings.Contains(name, "/") {
		// A program in the working directory is not a trusted system utility.
		switch path.Dir(name) {
		case "/bin", "/usr/bin", "/sbin", "/usr/sbin":
			if path.Clean(name) != name {
				return false
			}
			name = path.Base(name)
		default:
			return false
		}
	}
	switch name {
	case "du", "df", "ls", "pwd", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep",
		"id", "whoami", "uname", "uptime", "free", "ps", "stat", "readlink", "realpath", "lscpu", "lsmod":
		return true
	case "sort":
		// Glob expansion can inject options such as --compress-program or -o.
		return !anyGlob && readOnlySort(args[1:])
	case "uniq":
		// uniq's second filename is an output file; accept pipeline filters only.
		return !anyGlob && onlyFlags(args[1:], "cdiuz", "--count", "--repeated", "--ignore-case", "--unique", "--zero-terminated")
	case "hostname":
		return !anyGlob && onlyFlags(args[1:], "aAdfisI", "--alias", "--all-fqdns", "--domain", "--fqdn", "--long", "--ip-address", "--all-ip-addresses", "--short")
	}
	return false
}

func onlyFlags(args []string, short string, long ...string) bool {
	for _, arg := range args {
		matched := false
		for _, option := range long {
			matched = matched || arg == option
		}
		if matched {
			continue
		}
		if !strings.HasPrefix(arg, "-") || len(arg) < 2 || strings.Trim(arg[1:], short) != "" {
			return false
		}
	}
	return true
}

func readOnlySort(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return true
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == "-k" || arg == "-t" || arg == "--key" || arg == "--field-separator" {
			i++
			if i >= len(args) {
				return false
			}
			continue
		}
		if strings.HasPrefix(arg, "-k") || strings.HasPrefix(arg, "-t") || strings.HasPrefix(arg, "--key=") || strings.HasPrefix(arg, "--field-separator=") {
			continue
		}
		if !onlyFlags([]string{arg}, "bcCdfghimMnrRsuVz", "--reverse", "--numeric-sort", "--human-numeric-sort", "--general-numeric-sort", "--month-sort", "--version-sort", "--ignore-leading-blanks", "--ignore-case", "--dictionary-order", "--ignore-nonprinting", "--stable", "--unique", "--zero-terminated") {
			return false
		}
	}
	return true
}
