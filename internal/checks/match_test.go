package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

func TestEveryCheckHasKeywords(t *testing.T) {
	for _, c := range All() {
		if len(c.Keywords) == 0 {
			t.Errorf("%s has no keywords", c.ID)
		}
	}
}

func TestMatchQuestions(t *testing.T) {
	tests := []struct{ q, want string }{
		{"no sound in zoom", "camera"},
		{"my wi-fi keeps dropping", "network"},
		{"laptop won't charge past 80%", "battery"},
		{"second monitor not detected", "display"},
		{"computer froze last night", "crash"},
		{"fan is really loud", "thermal"},
		{"airpods won't pair", "bluetooth"},
		{"screen sharing is black on wayland", "apps"},
		{"docker permission denied", "permissions"},
		{"time is wrong after windows", "clock"},
		{"laptop doesn't wake up after closing the lid", "sleep"},
		{"nvidia driver", "drivers"},
		{"disk full", "disk"},
		{"apt is locked", "updates"},
		{"takes forever to boot", "boot"},
		{"headphones no sound", "sound"},
	}
	all := All()
	for _, tc := range tests {
		got := doctor.Match(all, tc.q)
		if len(got) == 0 || got[0].ID != tc.want {
			ids := make([]string, len(got))
			for i, c := range got {
				ids[i] = c.ID
			}
			t.Errorf("%q: got %v, want %s first", tc.q, ids, tc.want)
		}
	}
}

func TestMatchNothing(t *testing.T) {
	if got := doctor.Match(All(), "why is my"); len(got) != 0 {
		t.Errorf("filler words matched %d checks", len(got))
	}
}
