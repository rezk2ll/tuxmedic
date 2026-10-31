package checks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

type entry struct {
	Time     time.Time
	Priority int
	Ident    string
	Message  string
	Kernel   bool
}

type boot struct {
	Index int
	ID    string
	First time.Time
	Last  time.Time
}

// journal reads the systemd journal through journalctl's JSON output, which is
// stable across systemd versions, unlike the text formats.
func journal(ctx context.Context, s sys.System, args ...string) ([]entry, error) {
	out, err := s.Run(ctx, "journalctl", append([]string{"--no-pager", "-q", "-o", "json"}, args...)...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseJournal(out), nil
}

func parseJournal(out []byte) []entry {
	var es []entry
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var raw map[string]json.RawMessage
		if json.Unmarshal(sc.Bytes(), &raw) != nil {
			continue
		}
		e := entry{
			Message: field(raw["MESSAGE"]),
			Ident:   field(raw["SYSLOG_IDENTIFIER"]),
		}
		if e.Ident == "" {
			e.Ident = field(raw["_COMM"])
		}
		e.Kernel = field(raw["_TRANSPORT"]) == "kernel"
		if p, err := strconv.Atoi(field(raw["PRIORITY"])); err == nil {
			e.Priority = p
		} else {
			e.Priority = 6
		}
		if us, err := strconv.ParseInt(field(raw["__REALTIME_TIMESTAMP"]), 10, 64); err == nil {
			e.Time = time.UnixMicro(us)
		}
		es = append(es, e)
	}
	return es
}

// field decodes a journal field. journalctl writes non-UTF-8 values as arrays
// of bytes and repeated fields as arrays of strings.
func field(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b []byte
	var ints []int
	if json.Unmarshal(raw, &ints) == nil {
		b = make([]byte, len(ints))
		for i, v := range ints {
			b[i] = byte(v)
		}
		return strings.ToValidUTF8(string(b), "?")
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil && len(ss) > 0 {
		return ss[0]
	}
	return ""
}

func listBoots(ctx context.Context, s sys.System) ([]boot, error) {
	out, err := s.Run(ctx, "journalctl", "--no-pager", "--list-boots", "-o", "json")
	if err == nil {
		var raw []struct {
			Index int    `json:"index"`
			ID    string `json:"boot_id"`
			First int64  `json:"first_entry"`
			Last  int64  `json:"last_entry"`
		}
		if json.Unmarshal(out, &raw) == nil {
			bs := make([]boot, len(raw))
			for i, r := range raw {
				bs[i] = boot{Index: r.Index, ID: r.ID, First: time.UnixMicro(r.First), Last: time.UnixMicro(r.Last)}
			}
			return bs, nil
		}
	}
	// systemd older than 251 has no JSON boot list.
	out, err = s.Run(ctx, "journalctl", "--no-pager", "--list-boots")
	if err != nil {
		return nil, err
	}
	return parseBootsText(out), nil
}

func parseBootsText(out []byte) []boot {
	var bs []boot
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		idx, err := strconv.Atoi(f[0])
		if err != nil || len(f[1]) != 32 {
			continue
		}
		bs = append(bs, boot{Index: idx, ID: f[1]})
	}
	return bs
}

func when(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "at an unknown time"
	}
	t = t.Local()
	y, m, d := now.Local().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	switch {
	case !t.Before(today):
		return "today at " + t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "yesterday at " + t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -6)):
		return t.Format("Monday at 15:04")
	}
	return t.Format("Mon 2 Jan at 15:04")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// parseKV reads KEY=value lines, the format of os-release and `timedatectl show`.
func parseKV(s string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && !strings.HasPrefix(k, "#") {
			kv[k] = strings.Trim(v, `"'`)
		}
	}
	return kv
}
