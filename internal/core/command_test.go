package core

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
		wantArg string
	}{
		{"connect", "connect user pass", "connect", "user pass"},
		{"say", "say hello world", "say", "hello world"},
		{"look", "look", "look", ""},
		{"pose", "pose waves", "pose", "waves"},
		{"quit", "quit", "quit", ""},
		{"empty", "", "", ""},
		{"whitespace", "  say  hello  ", "say", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, arg := ParseCommand(tt.input)
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if arg != tt.wantArg {
				t.Errorf("arg = %q, want %q", arg, tt.wantArg)
			}
		})
	}
}
