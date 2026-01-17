package core

import "strings"

// ParseCommand splits input into command and arguments.
func ParseCommand(input string) (cmd, arg string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}

	parts := strings.SplitN(input, " ", 2)
	cmd = strings.ToLower(parts[0])
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	return cmd, arg
}
