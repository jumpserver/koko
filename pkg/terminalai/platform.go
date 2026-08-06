package terminalai

import "strings"

type platformCommand struct {
	Family   string
	Language string
	Shell    bool
}

func resolvePlatformCommand(context SessionContext) platformCommand {
	identity := strings.ToLower(strings.Join([]string{
		context.PlatformCategory,
		context.PlatformType,
		context.PlatformName,
		context.BaseOS,
	}, " "))
	switch {
	case containsPlatform(identity, "cisco", "ios-xe", "ios xe"):
		return platformCommand{
			Family:   "cisco-ios",
			Language: "Cisco IOS/IOS-XE network CLI command in the current mode",
		}
	case containsPlatform(identity, "huawei", "vrp"):
		return platformCommand{
			Family:   "huawei-vrp",
			Language: "Huawei VRP network CLI command in the current view",
		}
	case containsPlatform(identity, "h3c", "comware"):
		return platformCommand{
			Family:   "h3c-comware",
			Language: "H3C Comware network CLI command in the current view",
		}
	case containsPlatform(identity, "juniper", "junos"):
		return platformCommand{
			Family:   "juniper-junos",
			Language: "Juniper Junos CLI command in the current operational or configuration mode",
		}
	case containsPlatform(identity, "windows"):
		return platformCommand{
			Family: "windows",
			Language: "Windows terminal command matching the active PowerShell " +
				"or cmd.exe prompt",
		}
	case containsPlatform(identity, "network", "switch", "router", "firewall"):
		return platformCommand{
			Family:   "network-device",
			Language: "network device CLI command in the current mode",
		}
	case containsPlatform(identity, "linux"):
		return platformCommand{
			Family: "linux", Language: "POSIX shell command", Shell: true,
		}
	case containsPlatform(identity, "unix", "aix", "bsd", "solaris"):
		return platformCommand{
			Family: "unix", Language: "POSIX shell command", Shell: true,
		}
	default:
		return platformCommand{
			Family: "generic-terminal", Language: "terminal input for the active platform",
		}
	}
}

func containsPlatform(identity string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(identity, value) {
			return true
		}
	}
	return false
}
