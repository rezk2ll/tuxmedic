package checks

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var VMs = doctor.Check{
	ID:       "vms",
	Name:     "Virtual machines",
	Category: "System",
	Question: "Why won't my virtual machines start?",
	Run:      runVMs,
}

var vmTools = []string{"VBoxManage", "virt-manager", "gnome-boxes", "qemu-system-x86_64", "virsh", "vmware"}

func runVMs(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Virtual != "" || h.Container {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "This is a virtual machine itself"}}
	}
	var tools []string
	for _, t := range vmTools {
		if s.Has(t) {
			tools = append(tools, t)
		}
	}
	if len(tools) == 0 {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No virtual machine app is installed"}}
	}
	var fs []doctor.Finding
	flag := hasVirtFlag(s)
	if flag == "" || flag == "unknown" && !s.Exists("/dev/kvm") {
		fs = append(fs, doctor.Finding{
			Status: doctor.Fail,
			Title:  "Hardware virtualization is switched off, so virtual machines run very slowly or not at all",
			Explain: "The processor can run virtual machines at full speed, but the setting is off in the computer's firmware (BIOS/UEFI). " +
				"It's usually called Intel VT-x, Intel Virtualization Technology, AMD-V or SVM Mode.",
			Fixes: []doctor.Fix{
				{Title: "Restart into the firmware settings, turn on virtualization, save and restart", Command: "systemctl reboot --firmware-setup"},
			},
			Evidence: []string{"processor flags: no vmx or svm", "apps: " + strings.Join(tools, ", ")},
		})
	}
	if f, ok := vboxVsKVM(s, h); ok {
		fs = append(fs, f)
	}
	if f, ok := libvirtOff(ctx, s, h); ok {
		fs = append(fs, f)
	}
	if len(fs) == 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Hardware virtualization is on (" + flag + ")"})
	}
	return fs
}

// hasVirtFlag reports the CPU's virtualization flag. The kernel hides it
// when the firmware has the feature switched off.
func hasVirtFlag(s sys.System) string {
	for _, l := range strings.Split(readTrim(s, "/proc/cpuinfo"), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok || strings.TrimSpace(k) != "flags" {
			continue
		}
		for _, f := range strings.Fields(v) {
			switch f {
			case "vmx":
				return "Intel VT-x"
			case "svm":
				return "AMD-V"
			}
		}
		return ""
	}
	return "unknown"
}

var libvirtUnits = []string{"libvirtd.service", "libvirtd.socket", "virtqemud.service", "virtqemud.socket"}

// libvirtOff catches virt-manager and GNOME Boxes' system connection with
// no libvirt service to talk to: "Unable to connect to libvirt qemu:///system".
func libvirtOff(ctx context.Context, s sys.System, h *doctor.Host) (doctor.Finding, bool) {
	if !h.Systemd || !s.Has("virt-manager") && !s.Has("virsh") || !s.Has("systemctl") {
		return doctor.Finding{}, false
	}
	out, _ := s.Run(ctx, "systemctl", append([]string{"is-active"}, libvirtUnits...)...)
	states := strings.Fields(string(out))
	if len(states) == 0 {
		return doctor.Finding{}, false
	}
	for _, st := range states {
		if st == "active" || st == "activating" {
			return doctor.Finding{}, false
		}
	}
	unit := "libvirtd.socket"
	if s.Exists("/usr/lib/systemd/system/virtqemud.socket") && !s.Exists("/usr/lib/systemd/system/libvirtd.socket") {
		unit = "virtqemud.socket"
	}
	if !s.Exists("/usr/lib/systemd/system/"+unit) && !s.Exists("/lib/systemd/system/"+unit) {
		return doctor.Finding{
			Status:  doctor.Warn,
			Title:   "virt-manager has no libvirt service to connect to",
			Explain: "virt-manager is a window onto libvirt, which runs the machines. libvirt isn't installed, so it shows \"Unable to connect to libvirt\".",
			Fixes:   []doctor.Fix{installFix(h, "libvirt")},
		}, true
	}
	return doctor.Finding{
		Status: doctor.Warn,
		Title:  "The libvirt service is off, so virt-manager can't connect",
		Explain: "virt-manager is a window onto libvirt, which runs the machines. Some distros install libvirt without starting it, " +
			"so virt-manager shows \"Unable to connect to libvirt qemu:///system\".",
		Fixes:    []doctor.Fix{{Title: "Start libvirt now and at every startup", Command: "sudo systemctl enable --now " + unit, NeedsRoot: true}},
		Evidence: []string{strings.Join(libvirtUnits, ", ") + ": not running"},
	}, true
}

// vboxVsKVM catches VirtualBox failing because KVM already holds the
// processor's virtualization. Since Linux 6.12 KVM takes it as soon as its
// module loads, not only while a KVM machine runs.
func vboxVsKVM(s sys.System, h *doctor.Host) (doctor.Finding, bool) {
	if !s.Has("VBoxManage") || h.Home == "" || !s.Exists("/sys/module/kvm_intel") && !s.Exists("/sys/module/kvm_amd") {
		return doctor.Finding{}, false
	}
	logs, _ := s.Glob(filepath.Join(h.Home, "VirtualBox VMs", "*", "Logs", "VBox.log"))
	for _, l := range logs {
		b := readTrim(s, l)
		code := ""
		for _, c := range []string{"VERR_VMX_IN_VMX_ROOT_MODE", "VERR_SVM_IN_USE"} {
			if strings.Contains(b, c) {
				code = c
			}
		}
		if code == "" {
			continue
		}
		mod := "kvm_intel"
		if s.Exists("/sys/module/kvm_amd") {
			mod = "kvm_amd"
		}
		return doctor.Finding{
			Status: doctor.Fail,
			Title:  "VirtualBox can't start machines while KVM holds the processor's virtualization",
			Explain: "Only one of them can use it at a time. Recent kernels let KVM take it as soon as it loads, even when no KVM machine is running, " +
				"so VirtualBox stops with VERR_VMX_IN_VMX_ROOT_MODE or VERR_SVM_IN_USE.",
			Fixes: []doctor.Fix{
				kernelArgFix(s, h, "kvm.enable_virt_at_load=0"),
				{Title: "Or unload KVM until the next restart", Command: "sudo modprobe -r " + mod, NeedsRoot: true},
			},
			Evidence: []string{filepath.Base(filepath.Dir(filepath.Dir(l))) + ": " + code + " in VBox.log"},
		}, true
	}
	return doctor.Finding{}, false
}
