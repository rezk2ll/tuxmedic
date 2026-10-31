package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	tests := []struct {
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{args: []string{"version"}, stdout: "tuxmedic "},
		{args: []string{"--version"}, stdout: "tuxmedic "},
		{args: []string{"list"}, stdout: "battery"},
		{args: []string{"help"}, stdout: "Usage:"},
		{args: []string{"frobnicate"}, code: 2, stderr: `unknown command "frobnicate"`},
		{args: []string{"check", "nosuchcheck"}, code: 2, stderr: `no check called "nosuchcheck"`},
		{args: []string{"--nope"}, code: 2, stderr: "Usage:"},
		{args: []string{"ask"}, code: 2, stderr: "ask what?"},
		{args: []string{"completion", "bash"}, stdout: "complete -F _tuxmedic tuxmedic"},
		{args: []string{"completion", "zsh"}, stdout: "#compdef tuxmedic"},
		{args: []string{"completion", "fish"}, stdout: "__fish_seen_subcommand_from check"},
		{args: []string{"completion", "tcsh"}, code: 2, stderr: "pick bash, zsh or fish"},
		{args: []string{"completion"}, code: 2, stderr: "needs a shell"},
		{args: []string{"man"}, stdout: ".SH CHECKS"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tt.args, &out, &errOut)
			if code != tt.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tt.code, errOut.String())
			}
			if !strings.Contains(out.String(), tt.stdout) {
				t.Errorf("stdout %q, want it to contain %q", out.String(), tt.stdout)
			}
			if !strings.Contains(errOut.String(), tt.stderr) {
				t.Errorf("stderr %q, want it to contain %q", errOut.String(), tt.stderr)
			}
		})
	}
}
