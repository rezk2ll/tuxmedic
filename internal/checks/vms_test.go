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
		expect(t, run(t, VMs, fake("svm", nil), h), doctor.Skipped, "No virtual machine or container app")
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
	t.Run("default network stopped", func(t *testing.T) {
		f := fake("svm", []string{"virsh", "systemctl"}, "/dev/kvm", "")
		f.Commands[q] = "inactive\nactive\ninactive\ninactive\n"
		nets := " Name      State      Autostart   Persistent\n----------------------------------------------\n default   inactive   no          yes\n\n"
		f.Commands["virsh -c qemu:///system net-list --all"] = nets
		r := run(t, VMs, f, h)
		expect(t, r, doctor.Warn, "libvirt's default network is stopped")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo virsh net-start default && sudo virsh net-autostart default" {
			t.Errorf("fix = %q", c)
		}
		f.Commands["virsh -c qemu:///system net-list --all"] = strings.Replace(nets, "inactive", "active  ", 1)
		expect(t, run(t, VMs, f, h), doctor.OK, "AMD-V")
	})
	t.Run("inside a VM", func(t *testing.T) {
		expect(t, run(t, VMs, fake("", []string{"virsh"}), &doctor.Host{Virtual: "kvm"}), doctor.Skipped, "No virtual machine or container app")
	})

	podman := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Systemd: true, User: "sam"}
	t.Run("podman without an ID range", func(t *testing.T) {
		r := run(t, VMs, fake("vmx", []string{"podman"}, "/etc/subuid", "alex:100000:65536\n", "/etc/subgid", "alex:100000:65536\n"), podman)
		expect(t, r, doctor.Warn, "Podman can't run containers as you")
		if c := r.Findings[0].Fixes[0].Command; !strings.HasSuffix(c, "100000-165535 sam && podman system migrate") {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("podman ready", func(t *testing.T) {
		r := run(t, VMs, fake("vmx", []string{"podman"}, "/etc/subuid", "sam:100000:65536\n", "/etc/subgid", "sam:100000:65536\n"), podman)
		expect(t, r, doctor.OK, "Containers are set up to run")
	})
	dq := "systemctl is-active docker.service docker.socket"
	t.Run("docker service off", func(t *testing.T) {
		f := fake("vmx", []string{"docker", "systemctl"})
		f.Commands[dq] = "inactive\ninactive\n"
		expect(t, run(t, VMs, f, podman), doctor.Warn, "Docker is installed but its service isn't running")
	})
	t.Run("docker socket listening", func(t *testing.T) {
		f := fake("vmx", []string{"docker", "systemctl"})
		f.Commands[dq] = "inactive\nactive\n"
		expect(t, run(t, VMs, f, podman), doctor.OK, "Containers are set up to run")
	})
}
