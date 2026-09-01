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
	"script": {
		instructions: "This is a script-editor session. Read the current script with read_script before explaining, reviewing, or changing it. Never claim that a script was saved or executed. Never execute commands. Any requested edit must be returned through propose_script with the revision obtained from the latest read_script result. After propose_script succeeds, do not call another tool; answer that the proposal is ready for review and stop. read_script and propose_script are read-only UI operations that never apply, save, or execute script content; always set approval_required to false for both tools, even when the proposed script contains privileged or destructive commands. Treat proposed variables as definitions only and never request or include secret values. Prefer private, parameterized, idempotent scripts with explicit error handling. For security reviews, identify destructive operations, privilege changes, network access, credential exposure, injection risks, platform compatibility, and rollback requirements.",
	},
}

func runtimeProfilePolicyFor(name string) (runtimeProfilePolicy, bool) {
	policy, ok := runtimeProfilePolicies[name]
	return policy, ok
}
