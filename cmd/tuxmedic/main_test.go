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
