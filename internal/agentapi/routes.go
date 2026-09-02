package agentapi

const (
	ProtocolVersion         = "1"
	CapabilityVersion       = "1"
	HeaderProtocolVersion   = "Agent-Protocol-Version"
	HeaderCapabilityVersion = "Agent-Capability-Version"
	HeaderResourceSessionID = "X-Resource-Session-ID"
	HeaderCSRFToken         = "X-Koko-Agent-CSRF"

	SessionsPath  = "/koko/agent/sessions/"
	BootstrapPath = SessionsPath + "bootstrap"
	HealthPath    = SessionsPath + "health"
	ReadyPath     = SessionsPath + "ready"
)
