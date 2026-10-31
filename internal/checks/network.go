package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Network = doctor.Check{
	ID:       "network",
	Name:     "Internet & Wi-Fi",
	Category: "Network",
	Question: "Why can't I get online, or why does my Wi-Fi keep dropping?",
	Run:      runNetwork,
}

// Probed only to tell "no internet" apart from "DNS is broken". Two providers,
// so one outage doesn't look like the user's fault.
var probeAddrs = []string{"1.1.1.1:443", "9.9.9.9:443"}
var probeHosts = []string{"debian.org", "kernel.org"}

type iface struct {
	name     string
	wireless bool
	up       bool
	carrier  bool
}

func interfaces(s sys.System) []iface {
	dirs, _ := s.Glob("/sys/class/net/*")
	var out []iface
	for _, d := range dirs {
		n := filepath.Base(d)
		if n == "lo" || isVirtualIface(n) {
			continue
		}
		state, _ := s.ReadFile(d + "/operstate")
		carrier, _ := s.ReadFile(d + "/carrier")
		out = append(out, iface{
			name:     n,
			wireless: s.Exists(d+"/wireless") || s.Exists(d+"/phy80211"),
			up:       strings.TrimSpace(string(state)) == "up",
			carrier:  strings.TrimSpace(string(carrier)) == "1",
		})
	}
	return out
}

func isVirtualIface(n string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vnet", "tun", "tap", "wg", "tailscale", "zt", "p2p-dev", "lxc", "lxd", "cni", "flannel", "vmnet", "vboxnet"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func runNetwork(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	var fs []doctor.Finding
	ifs := interfaces(s)
	hasWifi := false
	for _, i := range ifs {
		hasWifi = hasWifi || i.wireless
	}

	if f, blocked := rfkillWifi(s); blocked {
		fs = append(fs, f)
	}
	// Containers and VMs see a virtual network, never the laptop's Wi-Fi card.
	if h.Laptop && !hasWifi && !h.Container && h.Virtual == "" {
		f := doctor.Finding{
			Status: doctor.Fail,
			Title:  "No Wi-Fi adapter is working",
			Explain: "This laptop should have Wi-Fi, but Linux doesn't see a working wireless adapter. " +
				"Usually its driver or firmware is missing. The Drivers check shows which one.",
			Fixes: []doctor.Fix{
				installFix(h, "linux-firmware"),
				{Title: "Connect with a cable or phone USB tethering to download what's missing"},
			},
		}
		fs = append(fs, f)
	}

	online := false
	for _, a := range probeAddrs {
		if s.Dial(ctx, a) == nil {
			online = true
			break
		}
	}
	resolves := false
	for _, host := range probeHosts {
		if s.Lookup(ctx, host) == nil {
			resolves = true
			break
		}
	}
	nmState := nmConnectivity(ctx, s)

	switch {
	case online && resolves:
		f := doctor.Finding{Status: doctor.OK, Title: "You're online"}
		if w := wifiSignal(s); w != nil {
			if w.Status == doctor.OK {
				f.Title += ", " + strings.ToLower(w.Title[:1]) + w.Title[1:]
			} else {
				fs = append(fs, *w)
			}
		}
		fs = append(fs, f)
	case nmState == "portal":
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   "This network wants you to sign in first",
			Explain: "Hotels, airports and cafés often block the internet until you accept their terms on a web page.",
			Fixes:   []doctor.Fix{{Title: "Open a browser and visit any http:// site, the sign-in page should appear", URL: "http://neverssl.com"}},
		})
	case online && !resolves:
		fs = append(fs, doctor.Finding{
			Status: doctor.Fail,
			Title:  "You're connected, but website names don't work (DNS)",
			Explain: "The internet itself is reachable, but the service that turns names like debian.org into addresses isn't answering. " +
				"Websites fail to load even though the connection is fine.",
			Fixes: dnsFixes(s),
		})
	case !online && !hasDefaultRoute(s):
		f := doctor.Finding{
			Status:  doctor.Fail,
			Title:   "You're not connected to any network",
			Explain: "No network connection is active, so nothing can reach the internet.",
		}
		if hasWifi {
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Pick a Wi-Fi network from the network menu"})
			if s.Has("nmcli") {
				f.Fixes = append(f.Fixes, doctor.Fix{Title: "Or list networks from here", Command: "nmcli device wifi list"})
			}
		}
		fs = append(fs, f)
	default:
		fs = append(fs, doctor.Finding{
			Status: doctor.Fail,
			Title:  "You're connected to a network, but it has no internet",
			Explain: "Your computer is connected to the router, but the router isn't reaching the internet. " +
				"Other devices on the same network probably can't get online either.",
			Fixes: []doctor.Fix{
				{Title: "Restart the router or modem"},
				{Title: "Check whether other devices can get online"},
			},
		})
	}

	fs = append(fs, wifiDrops(ctx, s)...)
	return fs
}

func rfkillWifi(s sys.System) (doctor.Finding, bool) {
	dirs, _ := s.Glob("/sys/class/rfkill/rfkill*")
	for _, d := range dirs {
		t, _ := s.ReadFile(d + "/type")
		if strings.TrimSpace(string(t)) != "wlan" {
			continue
		}
		hard, _ := s.ReadFile(d + "/hard")
		soft, _ := s.ReadFile(d + "/soft")
		switch {
		case strings.TrimSpace(string(hard)) == "1":
			return doctor.Finding{
				Status:  doctor.Fail,
				Title:   "Wi-Fi is switched off by a hardware switch or key",
				Explain: "A physical switch, or a key like Fn+F2 or the airplane key, has turned the Wi-Fi radio off. Software can't turn it back on.",
				Fixes:   []doctor.Fix{{Title: "Press the airplane or Wi-Fi key, or flip the wireless switch on the side of the laptop"}},
			}, true
		case strings.TrimSpace(string(soft)) == "1":
			return doctor.Finding{
				Status:  doctor.Fail,
				Title:   "Wi-Fi is turned off (airplane mode?)",
				Explain: "The Wi-Fi radio is switched off in software, often by airplane mode.",
				Fixes:   []doctor.Fix{{Title: "Turn Wi-Fi back on", Command: "rfkill unblock wifi"}},
			}, true
		}
	}
	return doctor.Finding{}, false
}

func nmConnectivity(ctx context.Context, s sys.System) string {
	if !s.Has("nmcli") {
		return ""
	}
	out, err := s.Run(ctx, "nmcli", "-t", "-f", "CONNECTIVITY", "general")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func hasDefaultRoute(s sys.System) bool {
	if b, err := s.ReadFile("/proc/net/route"); err == nil {
		for _, line := range strings.Split(string(b), "\n")[1:] {
			if f := strings.Fields(line); len(f) > 2 && f[1] == "00000000" {
				return true
			}
		}
	}
	if b, err := s.ReadFile("/proc/net/ipv6_route"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(line); len(f) > 9 && f[0] == strings.Repeat("0", 32) && f[1] == "00" && f[9] != "lo" {
				return true
			}
		}
	}
	return false
}

func dnsFixes(s sys.System) []doctor.Fix {
	var fx []doctor.Fix
	if b, err := s.ReadFile("/etc/resolv.conf"); err == nil && strings.Contains(string(b), "127.0.0.53") {
		fx = append(fx, doctor.Fix{Title: "Restart the DNS service", Command: "sudo systemctl restart systemd-resolved", NeedsRoot: true})
	}
	if s.Has("nmcli") {
		fx = append(fx, doctor.Fix{Title: "Reconnect to the network", Command: "nmcli networking off && nmcli networking on"})
	}
	if s.Exists("/etc/resolv.conf") {
		if b, _ := s.ReadFile("/etc/resolv.conf"); !strings.Contains(string(b), "nameserver") {
			fx = append(fx, doctor.Fix{Title: "Your DNS settings file lists no DNS server; reconnecting usually rewrites it"})
		}
	} else {
		fx = append(fx, doctor.Fix{Title: "/etc/resolv.conf is missing; reconnecting usually recreates it"})
	}
	fx = append(fx, doctor.Fix{Title: "Turn off VPN apps you aren't using, they often take over DNS"})
	return fx
}

var wirelessLine = regexp.MustCompile(`^\s*(\S+):\s+\d+\s+([\d.]+)\s+(-?[\d.]+)`)

func wifiSignal(s sys.System) *doctor.Finding {
	b, err := s.ReadFile("/proc/net/wireless")
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines[min(2, len(lines)):] {
		m := wirelessLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		dbm, _ := strconv.ParseFloat(strings.TrimSuffix(m[3], "."), 64)
		if dbm >= 0 {
			continue
		}
		switch {
		case dbm < -78:
			return &doctor.Finding{
				Status:  doctor.Warn,
				Title:   fmt.Sprintf("Your Wi-Fi signal is weak (%.0f dBm)", dbm),
				Explain: "A weak signal makes Wi-Fi slow and drop out. Walls, distance and microwaves weaken it.",
				Fixes: []doctor.Fix{
					{Title: "Move closer to the router, or use the 5 GHz network nearby and 2.4 GHz far away"},
				},
			}
		case dbm < -67:
			return &doctor.Finding{Status: doctor.OK, Title: fmt.Sprintf("Wi-Fi signal is fair (%.0f dBm)", dbm)}
		default:
			return &doctor.Finding{Status: doctor.OK, Title: fmt.Sprintf("Wi-Fi signal is strong (%.0f dBm)", dbm)}
		}
	}
	return nil
}

var dropRE = regexp.MustCompile(`(?i)CTRL-EVENT-DISCONNECTED|deauthenticating from|disassociated from|Connection to AP .* lost|beacon loss|CTRL-EVENT-BEACON-LOSS`)

// wifiDrops counts disconnects from wpa_supplicant, iwd and the kernel in the
// current session; a handful a day is normal roaming, dozens are a problem.
func wifiDrops(ctx context.Context, s sys.System) []doctor.Finding {
	es, err := journal(ctx, s, "-b", "0", "--since", "-24h", "-p", "0..6")
	if err != nil {
		return nil
	}
	var lines []string
	for _, e := range es {
		if dropRE.MatchString(e.Message) {
			lines = append(lines, firstLineOf(e.Message))
		}
	}
	if len(lines) < 10 {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  fmt.Sprintf("Wi-Fi dropped %d times in the last 24 hours", len(lines)),
		Explain: "Frequent drops usually come from Wi-Fi power saving, a weak signal, or a router that switches channels. " +
			"Turning power saving off fixes it on many laptops.",
		Fixes: []doctor.Fix{
			{Title: "Turn off Wi-Fi power saving", Command: "printf '[connection]\\nwifi.powersave = 2\\n' | sudo tee /etc/NetworkManager/conf.d/wifi-powersave-off.conf && sudo systemctl restart NetworkManager", NeedsRoot: true},
		},
		Evidence: lastN(lines, 5),
	}}
}
