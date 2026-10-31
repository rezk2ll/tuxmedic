// Package sys is the only way checks read the machine. Keeping every file read
// and command behind one interface lets the same check run on any distro and
// lets tests replay a machine from fixtures.
package sys

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// ErrNotFound means the command or file does not exist on this machine. Checks
// treat it as "this distro does not have that piece", never as a failure.
var ErrNotFound = errors.New("not found")

type DiskUsage struct {
	Total, Free, Avail uint64
	Files, FilesFree   uint64
}

type System interface {
	ReadFile(path string) ([]byte, error)
	Glob(pattern string) ([]string, error)
	Readlink(path string) (string, error)
	// Run executes a command with the C locale so its output parses the same on
	// every system language.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	Has(command string) bool
	Exists(path string) bool
	Statfs(path string) (DiskUsage, error)
	IsRoot() bool
	Getenv(key string) string
	Now() time.Time
	// Dial and Lookup are the only network access tuxmedic has: a TCP connect
	// and a DNS lookup, to tell "no internet" apart from "DNS is broken".
	Dial(ctx context.Context, addr string) error
	Lookup(ctx context.Context, host string) error
}

type Host struct{}

func (Host) ReadFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, ErrNotFound)
	}
	return b, err
}

func (Host) Glob(pattern string) ([]string, error) { return filepath.Glob(pattern) }

func (Host) Readlink(path string) (string, error) { return os.Readlink(path) }

func (Host) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "SYSTEMD_COLORS=0", "SYSTEMD_PAGER=", "NO_COLOR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
			return out, fmt.Errorf("%s: %w: %s", name, err, firstLine(msg))
		}
		return out, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

func (Host) Has(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

func (Host) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (Host) Statfs(path string) (DiskUsage, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return DiskUsage{}, err
	}
	bs := uint64(s.Bsize)
	return DiskUsage{
		Total: s.Blocks * bs, Free: s.Bfree * bs, Avail: s.Bavail * bs,
		Files: s.Files, FilesFree: s.Ffree,
	}, nil
}

func (Host) IsRoot() bool             { return os.Geteuid() == 0 }
func (Host) Getenv(key string) string { return os.Getenv(key) }
func (Host) Now() time.Time           { return time.Now() }

func (Host) Dial(ctx context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err == nil {
		c.Close()
	}
	return err
}

func (Host) Lookup(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := net.DefaultResolver.LookupHost(ctx, host)
	return err
}

func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
