package checks

import (
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestVMs(t *testing.T) {
	h := &doctor.Host{ID: "arch", PackageMgr: "pacman", Systemd: true, Home: "/home/sam"}
	fake := func(flags string, cmds []string, files ...string) *sys.Fake {
		f := &sys.Fake{
			Files:    map[string]string{"/proc/cpuinfo": "processor\t: 0\nflags\t\t: fpu sse2 " + flags + "\n"},
			Commands: map[string]string{"grub-mkconfig": ""},
		}
		for _, c := range cmds {
			f.Commands[c] = ""
		}
		for i := 0; i+1 < len(files); i += 2 {
			f.Files[files[i]] = files[i+1]
		}
		return f
	}

	t.Run("no VM app", func(t *testing.T) {
		expect(t, run(t, VMs, fake("svm", nil), h), doctor.Skipped, "No virtual machine app")
	})
	t.Run("switched off in firmware", func(t *testing.T) {
		expect(t, run(t, VMs, fake("", []string{"virt-manager"}), h), doctor.Fail, "Hardware virtualization is switched off")
	})
	t.Run("on", func(t *testing.T) {
		expect(t, run(t, VMs, fake("vmx", []string{"virt-manager"}, "/dev/kvm", ""), h), doctor.OK, "on (Intel VT-x)")
	})
	t.Run("virtualbox blocked by kvm", func(t *testing.T) {
		r := run(t, VMs, fake("svm", []string{"VBoxManage"},
			"/sys/module/kvm_amd/parameters/nested", "1",
			"/home/sam/VirtualBox VMs/Test VM/Logs/VBox.log", "00:00:01.2 Power up failed (vrc=VERR_SVM_IN_USE, rc=E_FAIL)\n"), h)
		expect(t, r, doctor.Fail, "VirtualBox can't start machines while KVM holds")
		fx := r.Findings[0].Fixes
		if !strings.Contains(fx[0].Command, "kvm.enable_virt_at_load=0") || fx[1].Command != "sudo modprobe -r kvm_amd" {
			t.Errorf("fixes = %+v", fx)
		}
		if r.Findings[0].Evidence[0] != "Test VM: VERR_SVM_IN_USE in VBox.log" {
			t.Errorf("evidence = %v", r.Findings[0].Evidence)
		}
	})
	q := "systemctl is-active libvirtd.service libvirtd.socket virtqemud.service virtqemud.socket"
	t.Run("libvirt installed but off", func(t *testing.T) {
		f := fake("svm", []string{"virt-manager", "systemctl"}, "/dev/kvm", "", "/usr/lib/systemd/system/libvirtd.socket", "")
		f.Commands[q] = "inactive\ninactive\ninactive\ninactive\n"
		r := run(t, VMs, f, h)
		expect(t, r, doctor.Warn, "The libvirt service is off")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo systemctl enable --now libvirtd.socket" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("libvirt not installed", func(t *testing.T) {
		f := fake("svm", []string{"virt-manager", "systemctl"}, "/dev/kvm", "")
		f.Commands[q] = "inactive\ninactive\ninactive\ninactive\n"
		r := run(t, VMs, f, &doctor.Host{ID: "debian", PackageMgr: "apt", Systemd: true})
		if c := r.Findings[0].Fixes[0].Command; c != "sudo apt install libvirt-daemon-system" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("libvirt socket listening", func(t *testing.T) {
		f := fake("svm", []string{"virt-manager", "systemctl"}, "/dev/kvm", "")
		f.Commands[q] = "inactive\nactive\ninactive\ninactive\n"
		expect(t, run(t, VMs, f, h), doctor.OK, "AMD-V")
	})
	t.Run("inside a VM", func(t *testing.T) {
		expect(t, run(t, VMs, fake("", []string{"virsh"}), &doctor.Host{Virtual: "kvm"}), doctor.Skipped, "virtual machine itself")
	})
}
