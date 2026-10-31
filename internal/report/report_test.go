package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

var host = &doctor.Host{
	Distro: "Fedora Linux 43", ID: "fedora", Kernel: "6.17.4-300.fc43.x86_64",
	Hostname: "zephyr-laptop", User: "alexq", Home: "/home/alexq",
}

func TestRedact(t *testing.T) {
	r := NewRedactor(host)
	tests := []struct{ in, want string }{
		{"/home/alexq/.config/pulse failed", "~/.config/pulse failed"},
		{"zephyr-laptop kernel: oops", "<hostname> kernel: oops"},
		{"session opened for user alexq", "session opened for user <user>"},
		{"wlp2s0: associated with 3c:7c:3f:12:ab:9e", "wlp2s0: associated with <mac>"},
		{"DHCP lease 192.168.1.37 from 192.168.1.1", "DHCP lease <ip> from <ip>"},
		{"probe 1.1.1.1:443 and 127.0.0.53", "probe 1.1.1.1:443 and 127.0.0.53"},
		{"addr fe80::1c2b:3aff:fe4d:5e6f", "addr fe80::<ipv6>"},
		{"mail alex.q@example.org", "mail <email>"},
		{"UUID=3f1e2d4c-5b6a-4798-8a9b-0c1d2e3f4a5b not found", "UUID=<id> not found"},
		{"alsa_output.usb-Acme_Headset_202105190004-00.analog", "alsa_output.usb-Acme_Headset_<serial>.analog"},
	}
	for _, tt := range tests {
		if got := r.String(tt.in); got != tt.want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", tt.in, got, tt.want)
		}
	}
}

func sample() []doctor.Report {
	return []doctor.Report{
		{CheckID: "disk", Name: "Disks & storage", Status: doctor.OK, Findings: []doctor.Finding{{Status: doctor.OK, Title: "Enough free space"}}},
		{CheckID: "sound", Name: "Sound", Status: doctor.Fail, Findings: []doctor.Finding{{
			Status: doctor.Fail, Title: "Sound is muted", Explain: "The output is muted.",
			Fixes:    []doctor.Fix{{Title: "Unmute", Command: "wpctl set-mute @DEFAULT_AUDIO_SINK@ 0"}},
			Evidence: []string{"zephyr-laptop pipewire[1234]: /home/alexq/.local/state lock"},
		}}},
	}
}

func TestMarkdownLeaksNothing(t *testing.T) {
	var b bytes.Buffer
	Markdown(&b, "v1.0.0", host, sample(), NewRedactor(host))
	out := b.String()
	for _, secret := range []string{"zephyr-laptop", "alexq"} {
		if strings.Contains(out, secret) {
			t.Errorf("report contains %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "1 problem found") || !strings.Contains(out, "Sound is muted") {
		t.Errorf("report misses the finding:\n%s", out)
	}
	if strings.Index(out, "Sound") > strings.Index(out, "Disks") {
		t.Error("worst check should come first")
	}
}

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, "v1.0.0", host, sample(), NewRedactor(host)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "alexq") || strings.Contains(b.String(), "zephyr") {
		t.Errorf("json leaks personal details:\n%s", b.String())
	}
	var v struct {
		Checks []struct {
			CheckID string
			Status  string
		}
	}
	if err := json.Unmarshal(b.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Checks) != 2 || v.Checks[0].Status != "problem" {
		t.Errorf("unexpected checks: %+v", v.Checks)
	}
}

func TestPlainShowsFixCommands(t *testing.T) {
	var b bytes.Buffer
	Plain(&b, host, sample(), ASCII, false)
	out := b.String()
	for _, want := range []string{"[x] Sound", "$ wpctl set-mute", "[ok] Disks & storage: Enough free space"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("plain output must not contain escape codes")
	}
}
