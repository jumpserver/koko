package agent

type runtimeProfilePolicy struct {
	instructions string
}

var runtimeProfilePolicies = map[string]runtimeProfilePolicy{
	"terminal": {
		instructions: "This is a terminal session; execute commands only through the tools provided by the current session.",
	},
	"file": {
		instructions: "This is a file-management session; never emit shell commands unless a current session tool explicitly represents one. Always pass complete virtual absolute paths to file tools. Reuse the exact path field returned by list_directory. When naming files in an answer, preserve their complete virtual paths so later turns can reuse them. If only a relative name is known, resolve it against currentPath from the user interface context or call list_directory first. read_text must identify one file and must never use / as its path. To copy a text file, call read_text for the source and then save_text for the destination with the returned content and expected_version set to absent; never invent a copy tool.",
	},
}

func runtimeProfilePolicyFor(name string) (runtimeProfilePolicy, bool) {
	policy, ok := runtimeProfilePolicies[name]
	return policy, ok
}
