package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

func rep(id, name string, st doctor.Status) doctor.Report {
	return doctor.Report{CheckID: id, Name: name, Status: st}
}

func TestSaveLoadCompare(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tuxmedic", "last.json")
	t0 := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	if _, ok := Load(path); ok {
		t.Fatal("loaded a snapshot that was never saved")
	}
	first := []doctor.Report{rep("sound", "Sound", doctor.Fail), rep("printer", "Printers", doctor.OK), rep("disk", "Disks", doctor.Warn)}
	if err := Save(path, first, t0); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode: %v %v", fi.Mode(), err)
	}

	prev, ok := Load(path)
	if !ok {
		t.Fatal("couldn't load")
	}
	now := t0.Add(50 * time.Hour)
	second := []doctor.Report{rep("sound", "Sound", doctor.OK), rep("printer", "Printers", doctor.Warn), rep("disk", "Disks", doctor.Warn), rep("games", "Games", doctor.Fail)}
	want := "Since last time (2 days ago): fixed: Sound; new problems: Printers."
	if got := Compare(prev, second, now); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := Compare(prev, first, now); got != "" {
		t.Errorf("unchanged run said %q", got)
	}

	// A partial run keeps what the other checks said.
	if err := Save(path, []doctor.Report{rep("games", "Games", doctor.OK)}, now); err != nil {
		t.Fatal(err)
	}
	after, _ := Load(path)
	if after.Status["sound"] != doctor.Fail || after.Status["games"] != doctor.OK || !after.At.Equal(now) {
		t.Errorf("after partial save: %+v", after)
	}
}

func TestPath(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := Path(env(map[string]string{"XDG_STATE_HOME": "/s", "HOME": "/h"})); got != "/s/tuxmedic/last.json" {
		t.Errorf("got %q", got)
	}
	if got := Path(env(map[string]string{"HOME": "/h"})); got != "/h/.local/state/tuxmedic/last.json" {
		t.Errorf("got %q", got)
	}
	if got := Path(env(nil)); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Minute: "less than an hour ago", 90 * time.Minute: "an hour ago",
		5 * time.Hour: "5 hours ago", 72 * time.Hour: "3 days ago",
	} {
		if got := ago(d); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}
