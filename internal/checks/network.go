package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
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

var probeAddrs6 = []string{"[2606:4700:4700::1111]:443", "[2620:fe::fe]:443"}

// ipv6Route returns the interface with a public IPv6 address and an IPv6
// default route, or "". Unique local addresses (fc00::/7) don't reach the
// internet, and rejecting routes on lo aren't real ones.
func ipv6Route(s sys.System) string {
	public := map[string]bool{}
	for _, l := range strings.Split(readTrim(s, "/proc/net/if_inet6"), "\n") {
		f := strings.Fields(l)
		if len(f) == 6 && f[3] == "00" && !strings.HasPrefix(f[0], "fc") && !strings.HasPrefix(f[0], "fd") {
			public[f[5]] = true
		}
	}
	for _, l := range strings.Split(readTrim(s, "/proc/net/ipv6_route"), "\n") {
		f := strings.Fields(l)
		if len(f) == 10 && f[0] == strings.Repeat("0", 32) && f[1] == "00" && f[9] != "lo" && public[f[9]] {
			return f[9]
		}
	}
	return ""
}

// slowBand catches Wi-Fi on 2.4 GHz while the same network offers a strong
// 5 or 6 GHz signal. It reads NetworkManager's last scan and doesn't rescan.
// The network's name stays out of the finding, so shared reports don't
// carry it.
func slowBand(ctx context.Context, s sys.System, h *doctor.Host) *doctor.Finding {
	if h.Network != "NetworkManager" || !s.Has("nmcli") {
		return nil
	}
	out, err := s.Run(ctx, "nmcli", "-t", "-f", "IN-USE,SIGNAL,FREQ,SSID,DEVICE", "device", "wifi", "list", "--rescan", "no")
	if err != nil {
		return nil
	}
	type ap struct {
		inUse        bool
		signal, freq int
		ssid, dev    string
	}
	var aps []ap
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.ReplaceAll(l, `\:`, "\x00")
		f := strings.Split(l, ":")
		if len(f) != 5 {
			continue
		}
		sig, _ := strconv.Atoi(f[1])
		mhz, _ := strconv.Atoi(strings.TrimSuffix(f[2], " MHz"))
		aps = append(aps, ap{f[0] == "*", sig, mhz, strings.ReplaceAll(f[3], "\x00", ":"), f[4]})
	}
	for _, cur := range aps {
		if !cur.inUse || cur.freq >= 3000 || cur.ssid == "" {
			continue
		}
		for _, o := range aps {
			if o.ssid == cur.ssid && o.freq >= 5000 && o.signal >= 50 {
				return &doctor.Finding{
					Status: doctor.Info,
					Title:  "Wi-Fi is on the slow 2.4 GHz band, but your network also has a strong 5 GHz signal",
					Explain: fmt.Sprintf("2.4 GHz is often several times slower and more crowded. Your network's %d GHz signal is at %d%%, "+
						"so you'd likely get faster, steadier Wi-Fi there.", o.freq/1000, o.signal),
					Fixes: []doctor.Fix{
						{Title: "Reconnect so the laptop can pick the faster band", Command: "nmcli device disconnect " + cur.dev + " && nmcli device connect " + cur.dev},
						{Title: "If it keeps picking 2.4 GHz, give the 5 GHz network its own name in the router settings"},
					},
				}
			}
		}
	}
	return nil
}

// slowAdapters only do 100 Mb/s, or report a made-up speed (phone
// tethering), so a low speed says nothing about the cable.
var slowAdapters = map[string]bool{
	"smsc95xx": true, "asix": true, "dm9601": true, "mcs7830": true, "sr9700": true,
	"cdc_ether": true, "rndis_host": true, "ipheth": true, "cdc_ncm": true,
}

// slowEthernet catches a wired link that came up at 10 or 100 Mb/s. Gigabit
// needs all four wire pairs; with one damaged, the link silently drops to
// 100 Mb/s.
func slowEthernet(s sys.System, ifs []iface) []doctor.Finding {
	var fs []doctor.Finding
	for _, i := range ifs {
		d := "/sys/class/net/" + i.name
		if i.wireless || !i.carrier || !s.Exists(d+"/device") {
			continue
		}
		drv := parseKV(readTrim(s, d+"/device/uevent"))["DRIVER"]
		speed := readTrim(s, d+"/speed")
		half := readTrim(s, d+"/duplex") == "half"
		if slowAdapters[drv] || speed != "10" && speed != "100" && !half {
			continue
		}
		title := "The cable connection runs at only " + speed + " Mb/s"
		if half {
			title = "The cable connection runs in half duplex, which is slow and drops packets"
		}
		fs = append(fs, doctor.Finding{
			Status: doctor.Info,
			Title:  title,
			Explain: "Your network card can do 1000 Mb/s or more. A link this slow usually means a damaged or very old cable, " +
				"a loose plug, or an old router or switch port.",
			Fixes: []doctor.Fix{
				{Title: "Try another cable (Cat 5e or newer) and another port on the router"},
				{Title: "See what the card and the other end agreed on", Command: "ethtool " + i.name},
			},
			Evidence: []string{i.name + ": speed " + speed + " Mb/s, duplex " + readTrim(s, d+"/duplex") + ", driver " + drv},
		})
	}
	return fs
}

// brokenIPv6 catches a network that hands out IPv6 but doesn't route it.
// Apps try IPv6 first, so every site stalls for seconds before loading.
func brokenIPv6(ctx context.Context, s sys.System, h *doctor.Host) *doctor.Finding {
	dev := ipv6Route(s)
	if dev == "" {
		return nil
	}
	for _, a := range probeAddrs6 {
		if s.Dial(ctx, a) == nil {
			return nil
		}
	}
	f := &doctor.Finding{
		Status: doctor.Warn,
		Title:  "IPv6 is set up on " + dev + " but doesn't reach the internet",
		Explain: "Your network gives this computer an IPv6 address, but connections over it go nowhere. Browsers and apps try IPv6 first, " +
			"so sites can take several seconds to start loading, and some apps fail. It's usually the router or the provider.",
		Fixes: []doctor.Fix{{Title: "Restart the router"}},
	}
	if h.Network == "NetworkManager" {
		f.Fixes = append(f.Fixes, doctor.Fix{
			Title:   "If it keeps happening, turn IPv6 off for this network only",
			Command: "nmcli connection modify \"$(nmcli -g GENERAL.CONNECTION device show " + dev + ")\" ipv6.method disabled && nmcli device reapply " + dev,
		})
	}
	return f
}

// unsetWifiCountry catches Wi-Fi left on the "world" rules (country 00),
// which allow only the channels legal everywhere: networks on the other
// 5 GHz channels stay invisible. Adapters that manage their own rules
// (Intel) list a country per phy, which then wins.
func unsetWifiCountry(ctx context.Context, s sys.System, ifs []iface) []doctor.Finding {
	if !slices.ContainsFunc(ifs, func(i iface) bool { return i.wireless }) || !s.Has("iw") {
		return nil
	}
	out, err := s.Run(ctx, "iw", "reg", "get")
	if err != nil {
		return nil
	}
	global := ""
	for _, l := range strings.Split(string(out), "\n") {
		code, ok := strings.CutPrefix(strings.TrimSpace(l), "country ")
		if !ok {
			continue
		}
		code, _, _ = strings.Cut(code, ":")
		if global == "" {
			global = code
		} else if code != "00" {
			return nil
		}
	}
	if global != "00" {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  "Wi-Fi doesn't know which country it's in, so some 5 GHz networks are hidden",
		Explain: "Each country allows different Wi-Fi channels. With no country set, Linux uses only the few channels allowed everywhere, " +
			"so a router on another 5 GHz channel doesn't show up, or the connection falls back to 2.4 GHz.",
		Fixes: []doctor.Fix{
			{Title: "Set your country's two-letter code (this example is Germany) until the next restart", Command: "sudo iw reg set DE", NeedsRoot: true},
			{Title: "To keep it: add the cfg80211 ieee80211_regdom option for your country", URL: "https://wiki.archlinux.org/title/Network_configuration/Wireless#Respecting_the_regulatory_domain"},
		},
		Evidence: []string{"iw reg get: country 00"},
	}}
}

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
		if b := brokenIPv6(ctx, s, h); b != nil {
			fs = append(fs, *b)
		}
		if b := slowBand(ctx, s, h); b != nil {
			fs = append(fs, *b)
		}
		fs = append(fs, slowEthernet(s, ifs)...)
		fs = append(fs, unsetWifiCountry(ctx, s, ifs)...)
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
