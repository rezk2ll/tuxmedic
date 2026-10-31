package checks

import (
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const (
	findHome   = "find /home/alex -mindepth 1 -maxdepth 1 -user 0"
	findDeeper = "find /home/alex/.config /home/alex/.cache /home/alex/.local/share -mindepth 1 -maxdepth 1 -user 0"
)

func permsHost() *doctor.Host {
	return &doctor.Host{ID: "debian", PackageMgr: "apt", User: "alex", Home: "/home/alex"}
}

func permsFake(groupsActive string, cmds map[string]string) *sys.Fake {
	if cmds == nil {
		cmds = map[string]string{}
	}
	for _, c := range []string{findHome, findDeeper} {
		if _, ok := cmds[c]; !ok {
			cmds[c] = ""
		}
	}
	return &sys.Fake{
		Files: map[string]string{
			"/proc/self/status": "Name:\ttuxmedic\nGroups:\t" + groupsActive + "\n",
			"/etc/group":        "root:x:0:\nadm:x:4:alex\nsudo:x:27:alex\ndocker:x:998:alex\ndialout:x:20:alex,sam\n",
		},
		Commands: cmds,
	}
}

func TestPermissions(t *testing.T) {
	t.Run("all active", func(t *testing.T) {
		r := run(t, Permissions, permsFake("4 20 27 998 1000", nil), permsHost())
		expect(t, r, doctor.OK, "group memberships are active")
	})
	t.Run("docker added after login", func(t *testing.T) {
		r := run(t, Permissions, permsFake("4 20 27 1000", nil), permsHost())
		expect(t, r, doctor.Warn, "You were added to the docker group, but it only works after you log out")
		if !strings.Contains(r.Findings[0].Explain, "running Docker without sudo") {
			t.Errorf("explain doesn't say what docker unlocks: %q", r.Findings[0].Explain)
		}
	})
	t.Run("two groups pending", func(t *testing.T) {
		r := run(t, Permissions, permsFake("4 27 1000", nil), permsHost())
		expect(t, r, doctor.Warn, "the groups docker and dialout")
	})
	t.Run("root owned files", func(t *testing.T) {
		r := run(t, Permissions, permsFake("4 20 27 998", map[string]string{
			findHome:   "/home/alex/.Xauthority\n",
			findDeeper: "/home/alex/.config/My App\n",
		}), permsHost())
		expect(t, r, doctor.Warn, "2 items in your home folder are owned by the administrator")
		fix := r.Findings[0].Fixes[0].Command
		if fix != "sudo chown -R alex: /home/alex/.Xauthority '/home/alex/.config/My App'" {
			t.Errorf("fix = %q", fix)
		}
	})
	t.Run("only a root owned cache", func(t *testing.T) {
		r := run(t, Permissions, permsFake("4 20 27 998", map[string]string{
			findDeeper: "/home/alex/.cache/scanner\n",
		}), permsHost())
		expect(t, r, doctor.Info, "1 item in your home folder is owned")
	})
	hostname := func(hosts, nss string, resolvable bool) *sys.Fake {
		f := permsFake("4 20 27 998 1000", nil)
		f.Files["/proc/sys/kernel/hostname"] = "studio-pc\n"
		f.Files["/etc/hosts"] = hosts
		f.Files["/etc/nsswitch.conf"] = "passwd: files\n" + nss
		f.Resolvable = map[string]bool{"studio-pc": resolvable}
		return f
	}
	t.Run("renamed, /etc/hosts still has the old name", func(t *testing.T) {
		f := hostname("127.0.0.1 localhost\n127.0.1.1 old-laptop # renamed\n", "hosts: files dns\n", false)
		r := run(t, Permissions, f, permsHost())
		expect(t, r, doctor.Warn, "can't find its own name (studio-pc), so sudo is slow")
		if c := r.Findings[0].Fixes[0].Command; c != `echo "127.0.1.1 studio-pc" | sudo tee -a /etc/hosts` {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("name in /etc/hosts with a domain", func(t *testing.T) {
		f := hostname("127.0.1.1 studio-pc.lan\n", "hosts: files dns\n", false)
		expect(t, run(t, Permissions, f, permsHost()), doctor.OK, "group memberships are active")
	})
	t.Run("myhostname answers it", func(t *testing.T) {
		f := hostname("127.0.0.1 localhost\n", "hosts: mymachines resolve [!UNAVAIL=return] files myhostname dns\n", false)
		expect(t, run(t, Permissions, f, permsHost()), doctor.OK, "group memberships are active")
	})
	t.Run("the local DNS server knows it", func(t *testing.T) {
		f := hostname("127.0.0.1 localhost\n", "hosts: files dns\n", true)
		expect(t, run(t, Permissions, f, permsHost()), doctor.OK, "group memberships are active")
	})
	t.Run("as root", func(t *testing.T) {
		f := permsFake("0", nil)
		f.Root = true
		expect(t, run(t, Permissions, f, permsHost()), doctor.Skipped, "Run without sudo")
	})
}
