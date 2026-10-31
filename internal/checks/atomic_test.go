package checks

import (
	"slices"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestFixesOnImageBasedSystems(t *testing.T) {
	ostree := &doctor.Host{ID: "bazzite", IDLike: []string{"fedora"}, PackageMgr: "rpm-ostree"}
	aeon := &doctor.Host{ID: "opensuse-aeon", PackageMgr: "transactional-update"}

	if got := installFix(ostree, "sof-firmware").Command; got != "sudo rpm-ostree install alsa-sof-firmware" {
		t.Errorf("rpm-ostree install = %q", got)
	}
	if got := installFix(aeon, "linux-firmware").Command; got != "sudo transactional-update pkg install kernel-firmware" {
		t.Errorf("transactional-update install = %q", got)
	}
	if fix, ok := downgradeFix(ostree, pkgChange{pkg: "mesa-dri-drivers", from: "25.1", to: "25.2"}); !ok || fix.Command != "sudo rpm-ostree rollback" {
		t.Errorf("rollback = %+v", fix)
	}
	var cleanup []string
	for _, f := range cleanupFixes(ostree) {
		cleanup = append(cleanup, f.Command)
	}
	if !slices.Contains(cleanup, "sudo rpm-ostree cleanup --base --repomd") || !slices.Contains(cleanup, "flatpak uninstall --unused") {
		t.Errorf("cleanup = %q", cleanup)
	}
}

func TestStagedUpdate(t *testing.T) {
	h := &doctor.Host{ID: "silverblue", PackageMgr: "rpm-ostree"}
	staged := `{"deployments":[{"booted":false,"staged":true,"version":"43.20261030.0"},{"booted":true,"version":"43.20261024.0"}]}`
	booted := `{"deployments":[{"booted":true,"version":"43.20261030.0"},{"booted":false,"version":"43.20261024.0"}]}`

	f := &sys.Fake{Commands: map[string]string{"rpm-ostree status --json": staged}}
	expect(t, run(t, Updates, f, h), doctor.Info, "An update is ready: restart to use it (43.20261030.0)")

	f = &sys.Fake{Commands: map[string]string{"rpm-ostree status --json": booted}}
	expect(t, run(t, Updates, f, h), doctor.OK, "No pending restart")
}
