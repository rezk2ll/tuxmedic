package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

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
