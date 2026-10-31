package watch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestTake(t *testing.T) {
	f := &sys.Fake{
		Clock: time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC),
		Files: map[string]string{
			"/sys/class/net/wlan0/wireless/x":       "",
			"/sys/class/net/wlan0/operstate":        "up\n",
			"/sys/class/thermal/thermal_zone0/temp": "61000\n",
			"/sys/class/thermal/thermal_zone1/temp": "200000\n",
		},
		Commands:   map[string]string{"iw dev wlan0 link": "Connected to 00:00:00:00:00:00 (on wlan0)\n\tsignal: -71 dBm\n"},
		Reachable:  map[string]bool{"9.9.9.9:443": true},
		Resolvable: map[string]bool{"debian.org": true},
	}
	got := Take(context.Background(), f)
	want := Sample{At: f.Clock, Online: true, DNS: true, Wifi: "wlan0", Linked: true, Signal: -71, TempC: 61}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if s := Status(got); s != "online · Wi-Fi -71 dBm, fair · 61°C" {
		t.Errorf("status %q", s)
	}
}

func TestTakeProcFallback(t *testing.T) {
	f := &sys.Fake{Files: map[string]string{
		"/sys/class/net/wlp2s0/wireless/x": "",
		"/sys/class/net/wlp2s0/operstate":  "up\n",
		"/proc/net/wireless":               "Inter-| sta-|   Quality\n face | tus | link level noise\nwlp2s0: 0000   40.  -79.  -256        0      0      0      0      0        0\n",
	}}
	got := Take(context.Background(), f)
	if !got.Linked || got.Signal != -79 || got.Online {
		t.Errorf("got %+v", got)
	}
}

// replay feeds samples through Diff the way Run does and returns the events.
func replay(samples ...Sample) []Event {
	var prev Sample
	var lostAt time.Time
	var es []Event
	t0 := time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)
	for i, cur := range samples {
		cur.At = t0.Add(time.Duration(i) * 3 * time.Second)
		cur.Wifi = "wlan0"
		for _, e := range Diff(prev, cur, lostAt) {
			switch e.Kind {
			case Lost:
				lostAt = e.At
			case Back:
				lostAt = time.Time{}
			}
			es = append(es, e)
		}
		prev = cur
	}
	return es
}

var (
	good    = Sample{Online: true, DNS: true, Linked: true, Signal: -55}
	weak    = Sample{Online: true, DNS: true, Linked: true, Signal: -82}
	noWifi  = Sample{}
	noInet  = Sample{DNS: false, Linked: true, Signal: -55}
	noDNS   = Sample{Online: true, Linked: true, Signal: -55}
	tooWarm = Sample{Online: true, DNS: true, Linked: true, Signal: -55, TempC: 95}
)

func TestSummaryTellsCausesApart(t *testing.T) {
	tests := []struct {
		name    string
		samples []Sample
		want    []string
	}{
		{"calm", []Sample{good, good, good}, []string{"Nothing went wrong"}},
		{"weak signal", []Sample{good, weak, noWifi, good}, []string{"dropped once", "gone for 3s", "That drop: the Wi-Fi signal was weak"}},
		{"wifi drops with good signal", []Sample{good, noWifi, good, noWifi, noWifi, good}, []string{"dropped twice", "gone for 9s", "Every drop: Wi-Fi disconnected even though the signal was fine"}},
		{"router", []Sample{good, noInet, good}, []string{"Wi-Fi stayed connected but the internet went away", "router or your provider"}},
		{"mixed", []Sample{good, noInet, good, noWifi, good}, []string{"1 of the 2 drops: Wi-Fi disconnected", "1 of the 2 drops: Wi-Fi stayed connected"}},
		{"dns", []Sample{good, noDNS, good}, []string{"DNS failed once"}},
		{"heat", []Sample{good, tooWarm, good, tooWarm}, []string{"90°C or more twice"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			es := replay(tc.samples...)
			got := strings.Join(Summary(es, time.Minute), "\n")
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("summary lacks %q:\n%s\nevents: %+v", w, got, es)
				}
			}
		})
	}
}

func TestEventsRead(t *testing.T) {
	es := replay(good, weak, noWifi, good)
	var texts []string
	for _, e := range es {
		texts = append(texts, e.Text)
	}
	got := strings.Join(texts, "\n")
	for _, w := range []string{
		"Wi-Fi signal became weak: -82 dBm, very weak",
		"Wi-Fi disconnected (signal before: -82 dBm, very weak)",
		"Internet lost",
		"Wi-Fi connected again",
		"Internet back after 3s",
	} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestRunStopsAndSummarizes(t *testing.T) {
	var out strings.Builder
	f := &sys.Fake{Reachable: map[string]bool{"1.1.1.1:443": true}, Resolvable: map[string]bool{"debian.org": true}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	es := Run(ctx, f, &out, 10*time.Millisecond, 0, false)
	if len(es) != 0 || !strings.Contains(out.String(), "Nothing went wrong") {
		t.Errorf("events %+v, output:\n%s", es, out.String())
	}
}
