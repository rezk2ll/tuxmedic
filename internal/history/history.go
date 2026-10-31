// Package history remembers how each check ended last time, so tuxmedic can
// say what got worse or was fixed since. Only check IDs and statuses are kept.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

type Snapshot struct {
	At     time.Time                `json:"at"`
	Status map[string]doctor.Status `json:"status"`
}

// Path is $XDG_STATE_HOME/tuxmedic/last.json, or ~/.local/state/tuxmedic/last.json.
func Path(getenv func(string) string) string {
	dir := getenv("XDG_STATE_HOME")
	if dir == "" {
		home := getenv("HOME")
		if home == "" {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "tuxmedic", "last.json")
}

func Load(path string) (Snapshot, bool) {
	var s Snapshot
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &s) != nil || s.Status == nil {
		return Snapshot{}, false
	}
	return s, true
}

// Save records the reports on top of the last snapshot, so running a few
// checks keeps what the others said.
func Save(path string, rs []doctor.Report, now time.Time) error {
	if path == "" {
		return nil
	}
	s, ok := Load(path)
	if !ok {
		s.Status = map[string]doctor.Status{}
	}
	s.At = now
	for _, r := range rs {
		s.Status[r.CheckID] = r.Status
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Compare says what changed since prev, or "" when nothing did.
func Compare(prev Snapshot, rs []doctor.Report, now time.Time) string {
	var fixed, worse []string
	for _, r := range rs {
		before, ok := prev.Status[r.CheckID]
		switch {
		case !ok:
		case before >= doctor.Warn && r.Status < doctor.Warn:
			fixed = append(fixed, r.Name)
		case r.Status >= doctor.Warn && r.Status > before:
			worse = append(worse, r.Name)
		}
	}
	if len(fixed)+len(worse) == 0 {
		return ""
	}
	var parts []string
	if len(fixed) > 0 {
		parts = append(parts, "fixed: "+strings.Join(fixed, ", "))
	}
	if len(worse) > 0 {
		parts = append(parts, "new problems: "+strings.Join(worse, ", "))
	}
	return fmt.Sprintf("Since last time (%s): %s.", ago(now.Sub(prev.At)), strings.Join(parts, "; "))
}

func ago(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "less than an hour ago"
	case d < 48*time.Hour:
		if h := int(d.Hours()); h > 1 {
			return fmt.Sprintf("%d hours ago", h)
		}
		return "an hour ago"
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}
