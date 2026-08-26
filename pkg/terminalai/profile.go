package terminalai

import (
	"encoding/json"
	"strings"
)

var profileCommands = []string{
	"sh", "bash", "sudo", "ls", "find", "grep", "sed", "awk", "tar",
	"curl", "wget", "git", "ps", "df", "du", "ip", "ss", "lsof",
	"systemctl", "service", "journalctl", "docker", "podman", "kubectl",
	"python3", "node", "npm", "java", "go", "apt-get", "dnf", "yum",
	"apk", "pacman", "zypper",
}

type AssetProfile struct {
	Adapter           string            `json:"adapter,omitempty"`
	PlatformFamily    string            `json:"platformFamily,omitempty"`
	CommandLanguage   string            `json:"commandLanguage,omitempty"`
	SessionContext    SessionContext    `json:"sessionContext,omitempty"`
	OSName            string            `json:"osName"`
	OSID              string            `json:"osId"`
	VersionID         string            `json:"versionId"`
	Kernel            string            `json:"kernel"`
	Architecture      string            `json:"architecture"`
	Shell             string            `json:"shell"`
	AvailableCommands []string          `json:"availableCommands"`
	Capabilities      map[string]string `json:"capabilities,omitempty"`
	DetectionError    string            `json:"detectionError,omitempty"`
}

func AssetProfileProbeCommand() string {
	return `if [ -r /etc/os-release ]; then . /etc/os-release; fi; ` +
		`printf 'os_name=%s\nos_id=%s\nversion_id=%s\nkernel=%s\narchitecture=%s\nshell=%s\n' "${PRETTY_NAME:-${NAME:-unknown}}" "${ID:-unknown}" "${VERSION_ID:-unknown}" "$(uname -sr 2>/dev/null || uname -a 2>/dev/null || printf unknown)" "$(uname -m 2>/dev/null || printf unknown)" "${SHELL:-unknown}"; ` +
		`for koko_ai_cmd in ` + strings.Join(profileCommands, " ") + `; do if command -v "$koko_ai_cmd" >/dev/null 2>&1; then printf 'command.%s=1\n' "$koko_ai_cmd"; fi; done`
}

func ParseAssetProfile(output string) AssetProfile {
	profile := AssetProfile{
		OSName: "unknown", OSID: "unknown", VersionID: "unknown",
		Kernel: "unknown", Architecture: "unknown", Shell: "unknown",
	}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "os_name":
			profile.OSName = value
		case "os_id":
			profile.OSID = value
		case "version_id":
			profile.VersionID = value
		case "kernel":
			profile.Kernel = value
		case "architecture":
			profile.Architecture = value
		case "shell":
			profile.Shell = value
		default:
			if command, found := strings.CutPrefix(key, "command."); found && value == "1" {
				profile.AvailableCommands = append(profile.AvailableCommands, command)
			}
		}
	}
	return profile
}

func (p AssetProfile) String() string {
	value, _ := json.Marshal(p)
	return string(value)
}
