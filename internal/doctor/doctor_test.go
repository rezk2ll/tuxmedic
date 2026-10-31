package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestRunOneRecoversFromPanic(t *testing.T) {
	c := Check{ID: "boom", Name: "Boom", Run: func(context.Context, sys.System, *Host) []Finding {
		panic("boom")
	}}
	r := RunOne(context.Background(), c, &sys.Fake{}, &Host{})
	if r.Status != Skipped || len(r.Findings) != 1 || !strings.HasPrefix(r.Findings[0].Title, "This check crashed") {
		t.Fatalf("want a skipped report explaining the crash, got %+v", r)
	}
}

func TestRunOneSortsWorstFirst(t *testing.T) {
	c := Check{ID: "x", Run: func(context.Context, sys.System, *Host) []Finding {
		return []Finding{{Status: OK, Title: "fine"}, {Status: Fail, Title: "broken"}, {Status: Warn, Title: "meh"}}
	}}
	r := RunOne(context.Background(), c, &sys.Fake{}, &Host{})
	if r.Status != Fail || r.Headline() != "broken" || r.Findings[2].Title != "fine" {
		t.Fatalf("unexpected order: %+v", r.Findings)
	}
}

func TestRunAllReturnsEveryCheck(t *testing.T) {
	var cs []Check
	for i := range 10 {
		cs = append(cs, Check{ID: string(rune('a' + i)), Run: func(context.Context, sys.System, *Host) []Finding {
			return []Finding{{Status: OK, Title: "ok"}}
		}})
	}
	out := make(chan Report)
	go RunAll(context.Background(), cs, &sys.Fake{}, &Host{}, out)
	seen := map[string]bool{}
	for r := range out {
		seen[r.CheckID] = true
	}
	if len(seen) != 10 {
		t.Fatalf("got %d reports, want 10", len(seen))
	}
}

func TestProbe(t *testing.T) {
	f := &sys.Fake{
		Files: map[string]string{
			"/etc/os-release":                       "NAME=\"Linux Mint\"\nPRETTY_NAME=\"Linux Mint 22.2\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\n",
			"/proc/sys/kernel/osrelease":            "6.14.0-33-generic\n",
			"/sys/class/dmi/id/chassis_type":        "10\n",
			"/run/systemd/system/x":                 "",
			"/sys/class/power_supply/BAT0/type":     "Battery",
			"/sys/firmware/efi/efivars/placeholder": "",
		},
		Commands: map[string]string{"apt --version": "apt 2.8.3"},
		Env:      map[string]string{"XDG_CURRENT_DESKTOP": "X-Cinnamon", "XDG_SESSION_TYPE": "x11"},
	}
	h := Probe(context.Background(), f)
	if h.Distro != "Linux Mint 22.2" || h.ID != "linuxmint" || !h.Like("debian") {
		t.Errorf("distro: %+v", h)
	}
	if !h.Laptop || !h.Systemd || h.PackageMgr != "apt" || h.Session != "x11" {
		t.Errorf("host: %+v", h)
	}
}
