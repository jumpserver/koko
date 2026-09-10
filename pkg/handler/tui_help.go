package handler

import "strings"

func (h *terminalUI) keyboardHelp(commands []tuiShortcut) string {
	var text strings.Builder
	for _, binding := range commands {
		if binding.label == "" {
			continue
		}
		text.WriteString(binding.label + "  " + binding.description + "\n\n")
	}
	return strings.TrimSpace(text.String())
}
