// Package checks holds every diagnosis tuxmedic can run.
package checks

import (
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

func All() []doctor.Check {
	cs := []doctor.Check{
		Crash, Network, Sound, Camera, Display, Keyboard, Touchpad, Fingerprint, Printer, Drives, Battery, Disk, Memory, Thermal, Video, Games,
		Drivers, Sleep, Bluetooth, Updates, Changes, Boot, Apps, VMs, Defaults, Keyring, Permissions, Locale, Clock,
	}
	for i := range cs {
		cs[i].Keywords = strings.Fields(keywords[cs[i].ID])
	}
	return cs
}

// keywords are the words people use for each problem, taken from how they ask
// on forums: apps and brands ("zoom", "airpods", "steam") as much as symptoms.
var keywords = map[string]string{
	"crash": "crash crashed crashes freeze frozen freezes hang hangs lockup reboot rebooted restart restarted " +
		"shutdown shuts off panic oom killed closes closed died",
	"network": "wifi wireless internet online offline network ethernet cable dns connection connect connected " +
		"disconnect disconnects dropping drops router modem hotspot vpn airplane captive hotel signal websites",
	"sound": "sound audio speaker speakers headphone headphones headset jack volume mute muted hdmi dummy " +
		"pipewire pulseaudio music youtube crackling quiet silent hear",
	"camera": "camera webcam cam mic microphone video zoom teams meet skype discord call calls record recording " +
		"voice hear",
	"display": "monitor monitors screen screens display displays external second hdmi displayport usbc dock " +
		"thunderbolt projector tv resolution black blank detected flicker flickering brightness backlight dim dark",
	"fingerprint": "fingerprint finger reader sensor fprintd biometric unlock login password howdy",
	"keyboard":    "keyboard keys key typing type typed slow late delay sticky shift lag input method ibus fcitx chinese japanese korean pinyin language layout",
	"touchpad":    "touchpad trackpad pointer cursor click clicking tap scroll scrolling gestures",
	"printer":     "printer printers print printing printed scanner cups paper job jobs queue hp epson canon brother",
	"drives": "usb stick flash pendrive thumb external drive drives sd card reader ntfs exfat fat32 readonly " +
		"mount mounting windows partition dualboot",
	"battery": "battery charge charging charger plugged power drain draining percent 80 health wear capacity",
	"disk":    "disk disks storage space full ssd drive hdd nvme smart readonly partition cleanup clean free",
	"memory":  "memory ram swap slow sluggish lag laggy tabs heavy freezing",
	"video": "video videos youtube netflix twitch stutter stuttering choppy lag lagging playback 4k vaapi " +
		"hardware acceleration decoding fans cpu",
	"games": "game games gaming steam proton vulkan controller gamepad joystick xbox playstation dualsense " +
		"wine lutris heroic",
	"thermal": "hot heat heating overheat overheating temperature temperatures temp fan fans loud noisy noise " +
		"throttling throttle cpu slow sluggish laggy speed ghz mhz frequency turbo boost",
	"drivers": "driver drivers nvidia amd radeon intel graphics gpu nouveau firmware secure secureboot broadcom " +
		"missing games gaming steam",
	"sleep":     "sleep suspend suspended wake waking wakeup resume lid hibernate standby",
	"bluetooth": "bluetooth pair pairing paired airpods earbuds buds controller gamepad mouse keyboard speaker",
	"updates": "update updates updating upgrade upgrading install installing package packages apt dnf pacman " +
		"zypper lock locked dpkg held software store",
	"changes": "changed change since yesterday week worked stopped broke broken after update updated upgrade " +
		"kernel suddenly",
	"boot": "boot booting startup start starting slow login services service failed",
	"apps": "flatpak snap screenshare screensharing share sharing portal wayland obs dialog open save sandbox " +
		"screenshot vscode editor watch watching inotify dropbox syncthing syncing",
	"vms": "vm vms virtual machine machines virtualbox vbox kvm qemu libvirt virt-manager boxes vmware " +
		"vt-x vtx amd-v svm virtualization virtualisation bios",
	"permissions": "permission permissions denied group groups docker sudo root arduino serial dialout owner " +
		"ownership loop login password wrong incorrect locked lockout faillock",
	"keyring": "keyring keyrings kwallet wallet unlock password passwords asks prompt seahorse autologin " +
		"automatic login",
	"defaults": "default app apps browser link links click clicking open opens opening pdf folder folders " +
		"wrong association xdg",
	"locale": "locale language languages lang utf8 utf-8 unicode accents accented characters letters question marks " +
		"garbled perl warning setting failed",
	"clock": "clock time date timezone dual dualboot windows ntp sync wrong hour",
}
