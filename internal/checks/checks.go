// Package checks holds every diagnosis tuxmedic can run.
package checks

import (
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

func All() []doctor.Check {
	cs := []doctor.Check{
		Crash, Network, Sound, Camera, Display, Touchpad, Printer, Battery, Disk, Memory, Thermal,
		Drivers, Sleep, Bluetooth, Updates, Changes, Boot, Apps, Permissions, Clock,
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
		"thunderbolt projector tv resolution black blank detected flicker flickering",
	"touchpad": "touchpad trackpad pointer cursor click clicking tap scroll scrolling gestures",
	"printer":  "printer printers print printing printed scanner cups paper job jobs queue hp epson canon brother",
	"battery":  "battery charge charging charger plugged power drain draining percent 80 health wear capacity",
	"disk":     "disk disks storage space full ssd drive hdd nvme smart readonly partition cleanup clean free",
	"memory":   "memory ram swap slow sluggish lag laggy tabs heavy freezing",
	"thermal": "hot heat heating overheat overheating temperature temperatures temp fan fans loud noisy noise " +
		"throttling throttle cpu",
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
		"screenshot",
	"permissions": "permission permissions denied group groups docker sudo root arduino serial dialout owner " +
		"ownership loop login",
	"clock": "clock time date timezone dual dualboot windows ntp sync wrong hour",
}
