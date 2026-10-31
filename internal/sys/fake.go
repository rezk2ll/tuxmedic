package sys

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Fake replays a machine from in-memory files and canned command output.
// Commands are keyed by their full command line, e.g. "wpctl status".
type Fake struct {
	Files    map[string]string
	Commands map[string]string
	// Failing commands return their output (if any in Commands) plus this error.
	Failing map[string]error
	Disks   map[string]DiskUsage
	Env     map[string]string
	Root    bool
	Clock   time.Time
	// Reachable lists addresses Dial succeeds on; Resolvable lists hosts Lookup resolves.
	Reachable  map[string]bool
	Resolvable map[string]bool
}

func (f *Fake) Dial(_ context.Context, addr string) error {
	if f.Reachable[addr] {
		return nil
	}
	return fmt.Errorf("dial tcp %s: connect: network is unreachable", addr)
}

func (f *Fake) Lookup(_ context.Context, host string) error {
	if f.Resolvable[host] {
		return nil
	}
	return fmt.Errorf("lookup %s: no such host", host)
}

func (f *Fake) ReadFile(path string) ([]byte, error) {
	if s, ok := f.Files[path]; ok {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("%s: %w", path, ErrNotFound)
}

func (f *Fake) Glob(pattern string) ([]string, error) {
	seen := map[string]bool{}
	for p := range f.Files {
		for c := p; c != "/" && c != "."; c = filepath.Dir(c) {
			if ok, _ := filepath.Match(pattern, c); ok {
				seen[c] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (f *Fake) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	out, ok := f.Commands[line]
	if err, failing := f.Failing[line]; failing {
		return []byte(out), err
	}
	if !ok {
		if !f.Has(name) {
			return nil, fmt.Errorf("%s: %w", name, ErrNotFound)
		}
		return nil, fmt.Errorf("fake: no output recorded for %q", line)
	}
	return []byte(out), nil
}

func (f *Fake) Has(command string) bool {
	for line := range f.Commands {
		if line == command || strings.HasPrefix(line, command+" ") {
			return true
		}
	}
	for line := range f.Failing {
		if line == command || strings.HasPrefix(line, command+" ") {
			return true
		}
	}
	return false
}

func (f *Fake) Exists(path string) bool {
	if _, ok := f.Files[path]; ok {
		return true
	}
	for p := range f.Files {
		if strings.HasPrefix(p, strings.TrimSuffix(path, "/")+"/") {
			return true
		}
	}
	return false
}

func (f *Fake) Statfs(path string) (DiskUsage, error) {
	if d, ok := f.Disks[path]; ok {
		return d, nil
	}
	return DiskUsage{}, fmt.Errorf("%s: %w", path, ErrNotFound)
}

func (f *Fake) IsRoot() bool             { return f.Root }
func (f *Fake) Getenv(key string) string { return f.Env[key] }

func (f *Fake) Now() time.Time {
	if f.Clock.IsZero() {
		return time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	}
	return f.Clock
}
