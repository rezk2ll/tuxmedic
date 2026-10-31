package checks

import (
	"context"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestUnmanagedNIC(t *testing.T) {
	h := &doctor.Host{Desktop: "GNOME", Network: "NetworkManager"}
	devs := "lo:loopback:connected (externally)\ndocker0:bridge:unmanaged\nveth1a2b:ethernet:unmanaged\n" +
		"wlp2s0:wifi:disconnected\nenp3s0:ethernet:unmanaged\n"
	fake := &sys.Fake{
		Files: files("/sys/class/net/wlp2s0/device/uevent", "", "/sys/class/net/enp3s0/device/uevent", "",
			"/etc/network/interfaces", "source /etc/network/interfaces.d/*\nauto lo\niface lo inet loopback\nallow-hotplug enp3s0\niface enp3s0 inet dhcp\n"),
		Commands: map[string]string{"nmcli -t -f DEVICE,TYPE,STATE device": devs},
	}
	fs := unmanagedNIC(context.Background(), fake, h)
	if len(fs) != 1 || fs[0].Title != "The cable adapter enp3s0 is set to be ignored by the network settings" || len(fs[0].Fixes) != 2 {
		t.Fatalf("findings = %+v", fs)
	}
	delete(fake.Files, "/etc/network/interfaces")
	if fs := unmanagedNIC(context.Background(), fake, h); len(fs) != 1 || len(fs[0].Fixes) != 1 {
		t.Errorf("without interfaces file: %+v", fs)
	}
	if fs := unmanagedNIC(context.Background(), fake, &doctor.Host{Network: "NetworkManager"}); len(fs) != 0 {
		t.Errorf("server: %+v", fs)
	}
}

func TestUnsetWifiCountry(t *testing.T) {
	const d = "/sys/class/net/wlp2s0/"
	fake := func(reg string) *sys.Fake {
		return &sys.Fake{
			Files:      files(d+"operstate", "up", d+"carrier", "1", d+"wireless/x", ""),
			Commands:   map[string]string{"iw reg get": reg},
			Reachable:  map[string]bool{probeAddrs[0]: true},
			Resolvable: map[string]bool{"debian.org": true},
		}
	}
	h := &doctor.Host{PackageMgr: "apt"}
	world := "global\ncountry 00: DFS-UNSET\n\t(2402 - 2472 @ 40), (6, 20), (N/A)\n"

	r := run(t, Network, fake(world), h)
	expect(t, r, doctor.Info, "Wi-Fi doesn't know which country it's in")
	if c := r.Findings[0].Fixes[0].Command; c != "sudo iw reg set DE" {
		t.Errorf("fix = %q", c)
	}
	expect(t, run(t, Network, fake(world+"\nphy#0 (self-managed)\ncountry US: DFS-FCC\n"), h), doctor.OK, "You're online")
	expect(t, run(t, Network, fake("global\ncountry DE: DFS-ETSI\n"), h), doctor.OK, "You're online")
}

func TestSlowEthernet(t *testing.T) {
	const d = "/sys/class/net/enp3s0/"
	fake := func(driver, speed, duplex string) *sys.Fake {
		return &sys.Fake{
			Files: files(d+"operstate", "up", d+"carrier", "1", d+"speed", speed, d+"duplex", duplex,
				d+"device/uevent", "DRIVER="+driver+"\n"),
			Reachable:  map[string]bool{probeAddrs[0]: true},
			Resolvable: map[string]bool{"debian.org": true},
		}
	}
	h := &doctor.Host{PackageMgr: "apt"}

	r := run(t, Network, fake("r8169", "100", "full"), h)
	expect(t, r, doctor.Info, "The cable connection runs at only 100 Mb/s")
	if r.Findings[0].Fixes[1].Command != "ethtool enp3s0" {
		t.Errorf("fixes = %+v", r.Findings[0].Fixes)
	}
	expect(t, run(t, Network, fake("e1000e", "1000", "half"), h), doctor.Info, "half duplex")
	expect(t, run(t, Network, fake("e1000e", "1000", "full"), h), doctor.OK, "You're online")
	expect(t, run(t, Network, fake("smsc95xx", "100", "full"), h), doctor.OK, "You're online")
}
