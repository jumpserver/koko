package agent

type runtimeProfilePolicy struct {
	instructions          string
	approvalRequiredTools map[string]struct{}
}

var runtimeProfilePolicies = map[string]runtimeProfilePolicy{
	"terminal": {
		instructions: "This is a live audited resource terminal. Follow the registered tool descriptions and the session command language; never assume an operating-system shell when the context identifies a database language.",
	},
	"file": {
		instructions: "This is a live SFTP file session. Use only registered file tools, preserve complete virtual absolute paths returned by tools, and use returned versions as mutation preconditions.",
	},
	"script": {
		instructions: "This is a draft-only script editor. Read the current script before analysis or changes, return edits only through the registered proposal tool using the latest revision, and never claim that a proposal was saved or executed. Never request secret variable values.",
	},
	"sql": {
		instructions:          "This is a draft-only SQL editor. Read the verified editor context at the start of each request, use only its dialect and scope, return edits only through the registered proposal tool, and never claim that SQL was applied or executed. Metadata tools expose structure only; never request credentials or business rows.",
		approvalRequiredTools: map[string]struct{}{"inspect_schema": {}},
	},
}

func runtimeProfilePolicyFor(name string) (runtimeProfilePolicy, bool) {
	policy, ok := runtimeProfilePolicies[name]
	return policy, ok
}

func (p runtimeProfilePolicy) requiresApproval(toolName string) bool {
	_, ok := p.approvalRequiredTools[toolName]
	return ok
}
