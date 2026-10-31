package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func run(t *testing.T, c doctor.Check, f *sys.Fake, h *doctor.Host) doctor.Report {
	t.Helper()
	if h == nil {
		h = &doctor.Host{Distro: "Debian GNU/Linux 13", ID: "debian", PackageMgr: "apt", Systemd: true}
	}
	return doctor.RunOne(context.Background(), c, f, h)
}

// expect asserts the report's overall status and that some finding title
// contains want.
func expect(t *testing.T, r doctor.Report, st doctor.Status, want string) {
	t.Helper()
	var titles []string
	for _, f := range r.Findings {
		if strings.HasPrefix(f.Title, "This check crashed") {
			t.Fatalf("%s crashed: %v", r.CheckID, f.Evidence)
		}
		titles = append(titles, f.Title)
	}
	if r.Status != st {
		t.Errorf("%s: status %v, want %v; findings: %q", r.CheckID, r.Status, st, titles)
	}
	for _, ti := range titles {
		if strings.Contains(ti, want) {
			return
		}
	}
	t.Errorf("%s: no finding contains %q; findings: %q", r.CheckID, want, titles)
}

func files(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestBattery(t *testing.T) {
	const bat, ac = "/sys/class/power_supply/BAT0/", "/sys/class/power_supply/AC/"
	laptop := &doctor.Host{Laptop: true, PackageMgr: "apt"}
	tests := []struct {
		name  string
		files map[string]string
		host  *doctor.Host
		st    doctor.Status
		want  string
	}{
		{
			name:  "plugged in but not charging, no limit set",
			files: files(bat+"type", "Battery", bat+"status", "Not charging", bat+"capacity", "61", ac+"type", "Mains", ac+"online", "1"),
			st:    doctor.Fail, want: "isn't charging (61%)",
		},
		{
			name: "paused at a charge limit is not a fault",
			files: files(bat+"type", "Battery", bat+"status", "Not charging", bat+"capacity", "79",
				bat+"charge_control_end_threshold", "80", ac+"type", "Mains", ac+"online", "1"),
			st: doctor.Info, want: "paused at 79%",
		},
		{
			name: "plugged in and still draining",
			files: files(bat+"type", "Battery", bat+"status", "Discharging", bat+"capacity", "40",
				ac+"type", "USB", ac+"online", "1"),
			st: doctor.Fail, want: "still draining",
		},
		{
			name: "worn battery",
			files: files(bat+"type", "Battery", bat+"status", "Discharging", bat+"capacity", "70",
				bat+"energy_full", "30000000", bat+"energy_full_design", "50000000", ac+"type", "Mains", ac+"online", "0"),
			st: doctor.Warn, want: "60%",
		},
		{
			name: "fast drain on battery",
			files: files(bat+"type", "Battery", bat+"status", "Discharging", bat+"capacity", "70",
				bat+"power_now", "32000000", bat+"energy_now", "40000000"),
			st: doctor.Warn, want: "draining fast: 32 W",
		},
		{
			name:  "unknown status unplugged does not crash",
			files: files(bat+"type", "Battery", bat+"status", "Not charging", bat+"capacity", "55"),
			st:    doctor.OK, want: "55%",
		},
		{
			name:  "a wireless mouse battery is ignored",
			files: files("/sys/class/power_supply/hidpp_battery_0/type", "Battery", "/sys/class/power_supply/hidpp_battery_0/scope", "Device"),
			host:  &doctor.Host{}, st: doctor.Skipped, want: "No battery",
		},
		{
			name:  "laptop with no battery detected",
			files: files(ac+"type", "Mains", ac+"online", "1"),
			host:  laptop, st: doctor.Fail, want: "battery isn't detected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expect(t, run(t, Battery, &sys.Fake{Files: tt.files}, tt.host), tt.st, tt.want)
		})
	}
}

func meminfoFile(totalKB, availKB, swapKB, swapFreeKB int) string {
	return strings.Join([]string{
		"MemTotal:       " + itoa(totalKB) + " kB",
		"MemAvailable:   " + itoa(availKB) + " kB",
		"SwapTotal:      " + itoa(swapKB) + " kB",
		"SwapFree:       " + itoa(swapFreeKB) + " kB",
	}, "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestMemory(t *testing.T) {
	const gb = 1 << 20
	t.Run("nearly full", func(t *testing.T) {
		f := &sys.Fake{Files: files("/proc/meminfo", meminfoFile(8*gb, gb/4, 2*gb, gb))}
		expect(t, run(t, Memory, f, nil), doctor.Fail, "Memory is nearly full")
	})
	t.Run("high pressure even with free memory", func(t *testing.T) {
		f := &sys.Fake{Files: files(
			"/proc/meminfo", meminfoFile(16*gb, 8*gb, 2*gb, 2*gb),
			"/proc/pressure/memory", "some avg10=30.00 avg60=25.00 avg300=14.20 total=1\nfull avg10=1 avg60=1 avg300=1 total=1",
		)}
		expect(t, run(t, Memory, f, nil), doctor.Fail, "nearly full")
	})
	t.Run("no swap on 8 GB", func(t *testing.T) {
		f := &sys.Fake{Files: files("/proc/meminfo", meminfoFile(8*gb, 6*gb, 0, 0))}
		expect(t, run(t, Memory, f, nil), doctor.Warn, "no swap or zram")
	})
	t.Run("healthy", func(t *testing.T) {
		f := &sys.Fake{Files: files("/proc/meminfo", meminfoFile(32*gb, 20*gb, 8*gb, 8*gb))}
		expect(t, run(t, Memory, f, nil), doctor.OK, "Plenty of free memory")
	})
}

func TestDisk(t *testing.T) {
	const gib = 1 << 30
	mounts := "/dev/nvme0n1p2 / ext4 rw,relatime 0 0\n/dev/nvme0n1p1 /boot/efi vfat rw 0 0\ntmpfs /run tmpfs rw 0 0\n"
	t.Run("main disk full", func(t *testing.T) {
		f := &sys.Fake{
			Files: files("/proc/self/mounts", mounts),
			Disks: map[string]sys.DiskUsage{"/": {Total: 250 * gib, Avail: gib / 2, Files: 1000, FilesFree: 500}},
		}
		expect(t, run(t, Disk, f, nil), doctor.Fail, "full")
	})
	t.Run("read-only root", func(t *testing.T) {
		f := &sys.Fake{
			Files: files("/proc/self/mounts", strings.Replace(mounts, "rw,relatime", "ro,relatime", 1)),
			Disks: map[string]sys.DiskUsage{"/": {Total: 250 * gib, Avail: 100 * gib, Files: 1000, FilesFree: 500}},
		}
		expect(t, run(t, Disk, f, nil), doctor.Fail, "read-only")
	})
	t.Run("out of inodes", func(t *testing.T) {
		f := &sys.Fake{
			Files: files("/proc/self/mounts", mounts),
			Disks: map[string]sys.DiskUsage{"/": {Total: 250 * gib, Avail: 100 * gib, Files: 1000, FilesFree: 10}},
		}
		expect(t, run(t, Disk, f, nil), doctor.Fail, "file slots")
	})
}

func TestNetwork(t *testing.T) {
	wifi := files(
		"/sys/class/net/wlp2s0/operstate", "up", "/sys/class/net/wlp2s0/carrier", "1", "/sys/class/net/wlp2s0/wireless/x", "",
		"/sys/class/net/lo/operstate", "unknown",
		"/proc/net/route", "Iface\tDestination\tGateway\nwlp2s0\t00000000\t0101A8C0\n",
	)
	laptop := &doctor.Host{Laptop: true, PackageMgr: "dnf"}
	t.Run("online", func(t *testing.T) {
		f := &sys.Fake{Files: wifi, Reachable: map[string]bool{"1.1.1.1:443": true}, Resolvable: map[string]bool{"debian.org": true}}
		expect(t, run(t, Network, f, laptop), doctor.OK, "You're online")
	})
	v6 := func(extra ...string) map[string]string {
		m := map[string]string{
			// A documentation-range address and a link-local one on the same card.
			"/proc/net/if_inet6": "20010db8000000000000000000000042 03 40 00 00     wlp2s0\n" +
				"fe800000000000000000000000000042 03 40 20 80     wlp2s0\n",
			"/proc/net/ipv6_route": "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000001 00000000 00000003   wlp2s0\n",
		}
		for k, v := range wifi {
			m[k] = v
		}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	nm := &doctor.Host{Laptop: true, PackageMgr: "dnf", Network: "NetworkManager"}
	t.Run("ipv6 handed out but not routed", func(t *testing.T) {
		f := &sys.Fake{Files: v6(), Reachable: map[string]bool{probeAddrs[0]: true}, Resolvable: map[string]bool{"debian.org": true}}
		r := run(t, Network, f, nm)
		expect(t, r, doctor.Warn, "IPv6 is set up on wlp2s0 but doesn't reach the internet")
		if c := r.Findings[0].Fixes[1].Command; !strings.Contains(c, "ipv6.method disabled && nmcli device reapply wlp2s0") {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("ipv6 works", func(t *testing.T) {
		f := &sys.Fake{Files: v6(), Reachable: map[string]bool{probeAddrs[0]: true, probeAddrs6[1]: true}, Resolvable: map[string]bool{"debian.org": true}}
		expect(t, run(t, Network, f, nm), doctor.OK, "You're online")
	})
	t.Run("only a unique local ipv6 address", func(t *testing.T) {
		f := &sys.Fake{
			Files:      v6("/proc/net/if_inet6", "fd000000000000000000000000000042 03 40 00 00     wlp2s0\n"),
			Reachable:  map[string]bool{probeAddrs[0]: true},
			Resolvable: map[string]bool{"debian.org": true},
		}
		expect(t, run(t, Network, f, nm), doctor.OK, "You're online")
	})
	scan := "nmcli -t -f IN-USE,SIGNAL,FREQ,SSID,DEVICE device wifi list --rescan no"
	online := func(list string) *sys.Fake {
		return &sys.Fake{Files: wifi, Commands: map[string]string{scan: list}, Reachable: map[string]bool{probeAddrs[0]: true}, Resolvable: map[string]bool{"debian.org": true}}
	}
	t.Run("on 2.4 GHz with a strong 5 GHz nearby", func(t *testing.T) {
		r := run(t, Network, online("*:64:2437 MHz:Cafe\\:Guest:wlp2s0\n :71:5500 MHz:Cafe\\:Guest:wlp2s0\n :90:5180 MHz:Other:wlp2s0\n"), nm)
		expect(t, r, doctor.Info, "slow 2.4 GHz band")
		f := r.Findings[0]
		if strings.Contains(f.Title+f.Explain, "Cafe") {
			t.Errorf("the network name leaked into the finding: %+v", f)
		}
		if !strings.Contains(f.Explain, "5 GHz signal is at 71%") {
			t.Errorf("explain = %q", f.Explain)
		}
	})
	t.Run("5 GHz too weak to be worth it", func(t *testing.T) {
		expect(t, run(t, Network, online("*:64:2437 MHz:Home:wlp2s0\n :30:5500 MHz:Home:wlp2s0\n"), nm), doctor.OK, "You're online")
	})
	t.Run("already on 5 GHz", func(t *testing.T) {
		expect(t, run(t, Network, online(" :64:2437 MHz:Home:wlp2s0\n*:71:5500 MHz:Home:wlp2s0\n"), nm), doctor.OK, "You're online")
	})
	t.Run("dns broken", func(t *testing.T) {
		f := &sys.Fake{Files: wifi, Reachable: map[string]bool{"9.9.9.9:443": true}}
		expect(t, run(t, Network, f, laptop), doctor.Fail, "DNS")
	})
	t.Run("captive portal", func(t *testing.T) {
		f := &sys.Fake{Files: wifi, Commands: map[string]string{"nmcli -t -f CONNECTIVITY general": "portal\n"}}
		expect(t, run(t, Network, f, laptop), doctor.Warn, "sign in")
	})
	t.Run("router has no internet", func(t *testing.T) {
		expect(t, run(t, Network, &sys.Fake{Files: wifi}, laptop), doctor.Fail, "no internet")
	})
	t.Run("airplane mode", func(t *testing.T) {
		fs := files("/sys/class/rfkill/rfkill0/type", "wlan", "/sys/class/rfkill/rfkill0/hard", "0", "/sys/class/rfkill/rfkill0/soft", "1")
		expect(t, run(t, Network, &sys.Fake{Files: fs}, laptop), doctor.Fail, "airplane mode")
	})
}

func TestUpdates(t *testing.T) {
	h := &doctor.Host{ID: "ubuntu", IDLike: []string{"debian"}, PackageMgr: "apt", Kernel: "6.8.0-40-generic"}
	t.Run("running kernel was removed", func(t *testing.T) {
		f := &sys.Fake{Files: files("/usr/lib/modules/6.8.0-45-generic/modules.dep", "")}
		expect(t, run(t, Updates, f, h), doctor.Warn, "restart to finish")
	})
	t.Run("interrupted dpkg", func(t *testing.T) {
		f := &sys.Fake{Files: files(
			"/usr/lib/modules/6.8.0-40-generic/modules.dep", "",
			"/var/lib/dpkg/updates/0001", "Package: foo",
		)}
		r := run(t, Updates, f, h)
		if r.Status < doctor.Warn {
			t.Fatalf("status %v, want a warning or worse", r.Status)
		}
		if !hasCommand(r, "dpkg --configure -a") {
			t.Errorf("want a dpkg --configure -a fix, got %+v", r.Findings)
		}
	})
}

func hasCommand(r doctor.Report, sub string) bool {
	for _, f := range r.Findings {
		for _, x := range f.Fixes {
			if strings.Contains(x.Command, sub) {
				return true
			}
		}
	}
	return false
}

func TestCrashGroupsSameCause(t *testing.T) {
	boots := `[{"index":-2,"boot_id":"aaaa","first_entry":1768000000000000,"last_entry":1768100000000000},` +
		`{"index":-1,"boot_id":"bbbb","first_entry":1768200000000000,"last_entry":1768300000000000},` +
		`{"index":0,"boot_id":"cccc","first_entry":1768400000000000,"last_entry":1768450000000000}]`
	oom := func(ts string) string {
		return `{"__REALTIME_TIMESTAMP":"` + ts + `","PRIORITY":"3","SYSLOG_IDENTIFIER":"kernel","MESSAGE":"Out of memory: Killed process 4242 (firefox)"}` + "\n"
	}
	f := &sys.Fake{Commands: map[string]string{
		"journalctl --no-pager --list-boots -o json":      boots,
		"journalctl --no-pager -q -o json -b aaaa -n 300": oom("1768100000000000"),
		"journalctl --no-pager -q -o json -b bbbb -n 300": oom("1768300000000000"),
	}}
	r := run(t, Crash, f, nil)
	expect(t, r, doctor.Fail, "crashed 2 times")
	n := 0
	for _, fd := range r.Findings {
		if strings.Contains(fd.Title, "crashed") && fd.Status == doctor.Fail {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want the two out-of-memory crashes folded into 1 finding, got %d", n)
	}
}

func TestEveryCheckSurvivesAnEmptyMachine(t *testing.T) {
	// A container or minimal VM: no /sys, no tools. Nothing may crash, and
	// nothing may claim a hardware fault.
	for _, c := range All() {
		t.Run(c.ID, func(t *testing.T) {
			r := run(t, c, &sys.Fake{}, &doctor.Host{Container: true})
			for _, f := range r.Findings {
				if strings.HasPrefix(f.Title, "This check crashed") {
					t.Fatalf("crashed: %v", f.Evidence)
				}
			}
		})
	}
}
