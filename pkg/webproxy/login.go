package webproxy

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type webLoginStep struct {
	Step    int    `json:"step"`
	Command string `json:"command"`
	Target  string `json:"target,omitempty"`
	Value   string `json:"value,omitempty"`
	Origin  string `json:"origin,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type webLoginConfig struct {
	Autofill            string         `json:"autofill"`
	UsernameSelector    string         `json:"username_selector,omitempty"`
	PasswordSelector    string         `json:"password_selector,omitempty"`
	SubmitSelector      string         `json:"submit_selector,omitempty"`
	SuccessSelector     string         `json:"success_selector,omitempty"`
	InteractiveSelector string         `json:"interactive_selector,omitempty"`
	Script              []webLoginStep `json:"script,omitempty"`
}

func exactOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "\\%*?# \t\r\n") || len(raw) > 512 || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("invalid Web origin: use an HTTP/HTTPS origin without paths or wildcards")
	}
	for _, c := range parsed.Hostname() {
		if c > 127 || c < 33 {
			return "", errors.New("Web origins must use ASCII host names")
		}
	}
	if strings.HasSuffix(parsed.Hostname(), ".") || parsed.Port() == "0" {
		return "", errors.New("invalid Web origin")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", errors.New("invalid Web origin port")
		}
	}
	if strings.Contains(parsed.Hostname(), ":") {
		port := parsed.Port()
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil {
			return "", errors.New("invalid Web origin host")
		}
		parsed.Host = "[" + ip.String() + "]"
		if port != "" {
			parsed.Host = net.JoinHostPort(ip.String(), port)
		}
		return normalizedOrigin(parsed.String())
	}
	return normalizedOrigin(raw)
}

func loginConfig(token webConnectToken, origin string) (webLoginConfig, error) {
	config := token.config
	if config.Autofill == "" {
		// Keep the basic SDK model useful to callers constructing tokens locally.
		data, _ := json.Marshal(token.Asset.SpecInfo)
		if err := json.Unmarshal(data, &config); err != nil {
			return config, err
		}
	}
	if config.Autofill == "" {
		if protocol, ok := token.Platform.GetProtocolSetting(token.Protocol); ok {
			data, err := json.Marshal(protocol.Setting)
			if err != nil {
				return config, err
			}
			if err = json.Unmarshal(data, &config); err != nil {
				return config, err
			}
		}
	}
	if config.Autofill != "script" {
		return config, nil
	}
	if len(config.Script) == 0 || len(config.Script) > 128 {
		return config, errors.New("Web script must contain 1 to 128 steps")
	}
	sort.SliceStable(config.Script, func(i, j int) bool { return config.Script[i].Step < config.Script[j].Step })
	for i := range config.Script {
		step := &config.Script[i]
		if step.Step < 1 || (i > 0 && step.Step == config.Script[i-1].Step) || len(step.Target) > 1024 || len(step.Value) > 4096 || step.Timeout < 0 || step.Timeout > 180 {
			return config, errors.New("invalid Web script step")
		}
		if step.Origin == "" {
			step.Origin = origin
		}
		value, err := exactOrigin(step.Origin)
		if err != nil {
			return config, errors.New("invalid script origin")
		}
		step.Origin = value
		usesCredentials := strings.Contains(step.Value, "{USERNAME}") || strings.Contains(step.Value, "{SECRET}")
		if usesCredentials && step.Command != "type" {
			return config, errors.New("script credentials may only be used by type commands")
		}
		switch step.Command {
		case "type", "click", "button", "check", "code", "interactive", "success":
			if !validCredentialSelector(step.Target) {
				return config, errors.New("invalid script selector")
			}
			if step.Command == "success" && i != len(config.Script)-1 {
				return config, errors.New("success must be the final script step")
			}
		case "open":
			raw := step.Value
			if raw == "" {
				raw = step.Target
			}
			target, err := url.Parse(raw)
			if err != nil || target.User != nil {
				return config, errors.New("invalid script URL")
			}
			base, _ := url.Parse(value)
			if _, err := normalizedOrigin(base.ResolveReference(target).String()); err != nil {
				return config, errors.New("invalid script URL")
			}
		case "sleep":
			seconds, err := strconv.Atoi(step.Target)
			if err != nil || seconds < 0 || seconds > 30 {
				return config, errors.New("script sleep must be 0 to 30 seconds")
			}
		default:
			return config, errors.New("unsupported Web Proxy script command (frames are not supported)")
		}
	}
	return config, nil
}

// Credential destinations come from the trusted asset script, not from page redirects.
func scriptCredentialOrigins(config webLoginConfig, origin string) []string {
	if config.Autofill != "script" {
		return []string{origin}
	}
	var origins []string
	for _, step := range config.Script {
		if step.Command == "type" && (strings.Contains(step.Value, "{USERNAME}") || strings.Contains(step.Value, "{SECRET}")) && !slices.Contains(origins, step.Origin) {
			origins = append(origins, step.Origin)
		}
	}
	return origins
}
