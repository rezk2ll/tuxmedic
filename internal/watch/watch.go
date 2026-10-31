// Package watch records the connection and temperature over time, to catch
// problems that come and go, like Wi-Fi that drops "randomly".
package watch

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const (
	weakSignal = -75 // dBm; below this, connections start to drop
	hot        = 90  // °C
)

type Sample struct {
	At     time.Time
	Online bool // a TCP connection to the internet succeeded
	DNS    bool // a name lookup succeeded
	Wifi   string
	Linked bool // the Wi-Fi interface is associated with an access point
	Signal int  // dBm, 0 when unknown
	TempC  int  // hottest sensor, 0 when unknown
}

var probes = []string{"1.1.1.1:443", "9.9.9.9:443"}

// Take reads the machine once. Probes run with short timeouts so one sample
// fits in a few seconds even when the network is down.
func Take(ctx context.Context, s sys.System) Sample {
	sm := Sample{At: s.Now()}
	for _, a := range probes {
		if s.Dial(ctx, a) == nil {
			sm.Online = true
			break
		}
	}
	sm.DNS = s.Lookup(ctx, "debian.org") == nil
	sm.Wifi = wifiIface(s)
	if sm.Wifi != "" {
		sm.Linked, sm.Signal = link(ctx, s, sm.Wifi)
	}
	sm.TempC = hottest(s)
	return sm
}

func wifiIface(s sys.System) string {
	ds, _ := s.Glob("/sys/class/net/*/wireless")
	for _, d := range ds {
		iface := filepath.Base(filepath.Dir(d))
		if st, _ := s.ReadFile("/sys/class/net/" + iface + "/operstate"); strings.TrimSpace(string(st)) != "" {
			return iface
		}
	}
	return ""
}

func link(ctx context.Context, s sys.System, iface string) (bool, int) {
	if out, err := s.Run(ctx, "iw", "dev", iface, "link"); err == nil {
		txt := string(out)
		if strings.HasPrefix(txt, "Not connected") {
			return false, 0
		}
		for _, line := range strings.Split(txt, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "signal:"); ok {
				n, _ := strconv.Atoi(strings.Fields(v)[0])
				return true, n
			}
		}
		return strings.HasPrefix(txt, "Connected"), 0
	}
	// /proc/net/wireless only lists an interface while it is associated.
	b, _ := s.ReadFile("/proc/net/wireless")
	for _, line := range strings.Split(string(b), "\n") {
		if name, rest, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && name == iface {
			f := strings.Fields(rest)
			if len(f) >= 3 {
				n, _ := strconv.ParseFloat(strings.TrimSuffix(f[2], "."), 64)
				return true, int(n)
			}
		}
	}
	st, _ := s.ReadFile("/sys/class/net/" + iface + "/operstate")
	return strings.TrimSpace(string(st)) == "up", 0
}

func hottest(s sys.System) int {
	zs, _ := s.Glob("/sys/class/thermal/thermal_zone*/temp")
	t := 0
	for _, z := range zs {
		b, err := s.ReadFile(z)
		if err != nil {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n/1000 < 150 {
			t = max(t, n/1000)
		}
	}
	return t
}

// Kind is the type of an event, used to build the summary.
type Kind int

const (
	Lost Kind = iota
	Back
	DNSLost
	DNSBack
	WifiOff
	WifiOn
	Weak
	Hot
)

type Event struct {
	At   time.Time
	Kind Kind
	Text string
	// For Lost: what the Wi-Fi looked like just before.
	Linked bool
	Signal int
	Down   time.Duration // for Back: how long the internet was gone
}

// Diff turns two consecutive samples into what changed between them.
// lostAt is when the internet went away, to time the outage.
func Diff(prev, cur Sample, lostAt time.Time) []Event {
	var es []Event
	add := func(k Kind, text string) *Event {
		es = append(es, Event{At: cur.At, Kind: k, Text: text})
		return &es[len(es)-1]
	}
	if prev.Wifi != "" && prev.Linked && !cur.Linked {
		add(WifiOff, fmt.Sprintf("Wi-Fi disconnected (signal before: %s)", dBm(prev.Signal)))
	}
	if cur.Wifi != "" && !prev.Linked && cur.Linked && !prev.At.IsZero() {
		add(WifiOn, "Wi-Fi connected again")
	}
	switch {
	case prev.Online && !cur.Online:
		e := add(Lost, "Internet lost")
		e.Linked, e.Signal = cur.Linked, prev.Signal
		if cur.Wifi != "" && cur.Linked {
			e.Text += " while Wi-Fi stayed connected"
		}
	case !prev.Online && cur.Online && !prev.At.IsZero():
		e := add(Back, "Internet back")
		if !lostAt.IsZero() {
			e.Down = cur.At.Sub(lostAt)
			e.Text += " after " + e.Down.Round(time.Second).String()
		}
	}
	if cur.Online && prev.DNS && !cur.DNS {
		add(DNSLost, "Websites stopped resolving (DNS) while the internet still works")
	}
	if cur.Online && !prev.DNS && cur.DNS && !prev.At.IsZero() && prev.Online {
		add(DNSBack, "DNS works again")
	}
	if cur.Linked && cur.Signal != 0 && cur.Signal < weakSignal && (prev.Signal == 0 || prev.Signal >= weakSignal) {
		add(Weak, "Wi-Fi signal became weak: "+dBm(cur.Signal))
	}
	if cur.TempC >= hot && prev.TempC < hot {
		add(Hot, fmt.Sprintf("Running hot: %d°C", cur.TempC))
	}
	return es
}

func dBm(n int) string {
	if n == 0 {
		return "unknown"
	}
	q := "good"
	switch {
	case n < -80:
		q = "very weak"
	case n < weakSignal:
		q = "weak"
	case n < -67:
		q = "fair"
	}
	return fmt.Sprintf("%d dBm, %s", n, q)
}

// Status is the one-line live view.
func Status(sm Sample) string {
	parts := []string{"offline"}
	if sm.Online {
		parts[0] = "online"
	}
	if sm.Online && !sm.DNS {
		parts[0] = "online, DNS failing"
	}
	if sm.Wifi != "" {
		if sm.Linked {
			parts = append(parts, "Wi-Fi "+dBm(sm.Signal))
		} else {
			parts = append(parts, "Wi-Fi not connected")
		}
	}
	if sm.TempC > 0 {
		parts = append(parts, fmt.Sprintf("%d°C", sm.TempC))
	}
	return strings.Join(parts, " · ")
}

// Summary explains a recording in a few sentences, most telling first.
func Summary(es []Event, took time.Duration) []string {
	var drops, wifiDrops, weakDrops, routerDrops, dns, heat int
	var down time.Duration
	for _, e := range es {
		switch e.Kind {
		case Lost:
			drops++
			switch {
			case !e.Linked && e.Signal != 0 && e.Signal < weakSignal:
				weakDrops++
			case !e.Linked:
				wifiDrops++
			default:
				routerDrops++
			}
		case Back:
			down += e.Down
		case DNSLost:
			dns++
		case Hot:
			heat++
		}
	}
	span := took.Round(time.Second)
	var out []string
	if drops == 0 && dns == 0 && heat == 0 {
		return []string{fmt.Sprintf("Nothing went wrong in %s of watching. Run it again while the problem happens.", span)}
	}
	if drops > 0 {
		out = append(out, fmt.Sprintf("The internet dropped %s in %s, gone for %s in total.", times(drops), span, down.Round(time.Second)))
	}
	if weakDrops > 0 {
		out = append(out, fmt.Sprintf("%s: the Wi-Fi signal was weak just before. Move closer to the router, or remove what is between you (walls, metal, microwaves).", ofThem(weakDrops, drops)))
	}
	if wifiDrops > 0 {
		out = append(out, fmt.Sprintf("%s: Wi-Fi disconnected even though the signal was fine. That points at this computer, usually Wi-Fi power saving or the driver. Run \"tuxmedic check network\" for the fix.", ofThem(wifiDrops, drops)))
	}
	if routerDrops > 0 {
		out = append(out, fmt.Sprintf("%s: Wi-Fi stayed connected but the internet went away. The problem is the router or your provider, not this computer. Restart the router, and call the provider if it keeps happening.", ofThem(routerDrops, drops)))
	}
	if dns > 0 {
		out = append(out, fmt.Sprintf("DNS failed %s while the connection worked. Switching to a public DNS server usually fixes this; \"tuxmedic check network\" shows how.", times(dns)))
	}
	if heat > 0 {
		out = append(out, fmt.Sprintf("The computer reached %d°C or more %s. Run \"tuxmedic check thermal\" to see why.", hot, times(heat)))
	}
	return out
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	if n == 2 {
		return "twice"
	}
	return fmt.Sprintf("%d times", n)
}

func ofThem(n, total int) string {
	if n == total {
		if total == 1 {
			return "That drop"
		}
		return "Every drop"
	}
	return fmt.Sprintf("%d of the %d drops", n, total)
}
