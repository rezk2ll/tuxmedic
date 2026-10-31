package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Display = doctor.Check{
	ID:       "display",
	Name:     "Screens & monitors",
	Category: "Devices",
	Question: "Why isn't my second monitor detected, or why does it stay black?",
	Run:      runDisplay,
}

type connector struct {
	card, port      string // "card1", "HDMI-A-1"
	connected, on   bool
	mode, name, gpu string
}

func (c connector) builtin() bool {
	return strings.HasPrefix(c.port, "eDP") || strings.HasPrefix(c.port, "LVDS") || strings.HasPrefix(c.port, "DSI")
}

func (c connector) label() string {
	if c.builtin() {
		return "the built-in screen"
	}
	port := portName(c.port)
	if c.name != "" {
		return fmt.Sprintf("%s (%s)", c.name, port)
	}
	return "the monitor on " + port
}

func portName(p string) string {
	switch {
	case strings.HasPrefix(p, "HDMI"):
		return "HDMI"
	case strings.HasPrefix(p, "DP"):
		return "DisplayPort or USB-C"
	case strings.HasPrefix(p, "DVI"):
		return "DVI"
	case strings.HasPrefix(p, "VGA"):
		return "VGA"
	}
	return p
}

func connectors(s sys.System) []connector {
	dirs, _ := s.Glob("/sys/class/drm/card*-*")
	var out []connector
	for _, d := range dirs {
		card, port, ok := strings.Cut(filepath.Base(d), "-")
		if !ok || strings.HasPrefix(port, "Writeback") {
			continue
		}
		c := connector{card: card, port: port}
		c.connected = readTrim(s, d+"/status") == "connected"
		c.on = readTrim(s, d+"/enabled") == "enabled"
		c.mode, _, _ = strings.Cut(readTrim(s, d+"/modes"), "\n")
		if b, err := s.ReadFile(d + "/edid"); err == nil {
			c.name = edidName(b)
		}
		if drv, err := s.ReadFile("/sys/class/drm/" + card + "/device/uevent"); err == nil {
			c.gpu = parseKV(string(drv))["DRIVER"]
		}
		out = append(out, c)
	}
	return out
}

// edidName reads the monitor name descriptor (tag 0xFC) from an EDID block.
func edidName(b []byte) string {
	if len(b) < 128 {
		return ""
	}
	for off := 54; off+18 <= 126; off += 18 {
		d := b[off : off+18]
		if d[0] == 0 && d[1] == 0 && d[2] == 0 && d[3] == 0xFC {
			name, _, _ := strings.Cut(string(d[5:]), "\n")
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func runDisplay(_ context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	conns := connectors(s)
	var fs []doctor.Finding
	if len(conns) == 0 {
		if h.Container || h.Virtual != "" {
			return []doctor.Finding{{Status: doctor.Skipped, Title: "No screens to check in a virtual machine or container"}}
		}
		fs = append(fs, doctor.Finding{
			Status:  doctor.Info,
			Title:   "No graphics outputs found",
			Explain: "Linux doesn't list any screen connectors. Either this is a server without graphics, or the graphics driver isn't loaded. The Drivers check shows which one.",
		})
	}

	var active []string
	for _, c := range conns {
		switch {
		case c.connected && c.mode == "" && !c.builtin():
			fs = append(fs, doctor.Finding{
				Status: doctor.Fail,
				Title:  "A monitor is plugged into " + portName(c.port) + ", but it can't tell the computer what it supports",
				Explain: "The monitor is detected, but it didn't send its list of resolutions (its EDID). A loose or cheap cable, an adapter, or a dock is the usual cause. " +
					"Without that list the desktop won't turn the monitor on.",
				Fixes: []doctor.Fix{
					{Title: "Plug the cable in firmly at both ends, or try another cable"},
					{Title: "Connect the monitor directly instead of through an adapter or dock"},
					{Title: "Switch the monitor off and on again while it is plugged in"},
				},
			})
		case c.connected && !c.on && !c.builtin() && c.gpu != "nvidia":
			fs = append(fs, doctor.Finding{
				Status: doctor.Warn,
				Title:  "Your monitor " + c.label() + " is plugged in but switched off",
				Explain: "The computer sees the monitor and knows its resolutions, but the desktop isn't using it. " +
					"This happens when it was turned off in the display settings, or the desktop missed it being plugged in.",
				Fixes: []doctor.Fix{{Title: "Open your display settings and turn the monitor on, or press Super+P to switch screen modes"}},
			})
		case c.connected && c.on:
			a := c.label()
			if c.mode != "" {
				a += " at " + c.mode
			}
			active = append(active, a)
		}
	}

	fs = append(fs, displayLink(s)...)
	fs = append(fs, thunderbolt(s)...)

	if len(active) > 0 {
		fs = append(fs, doctor.Finding{
			Status: doctor.OK,
			Title:  "Screens in use: " + strings.Join(active, ", "),
		})
	}
	return fs
}

// displayLink spots docks that drive monitors over plain USB. They need the
// evdi kernel module and the vendor's driver, which no distro ships by default.
func displayLink(s sys.System) []doctor.Finding {
	vendors, _ := s.Glob("/sys/bus/usb/devices/*/idVendor")
	for _, v := range vendors {
		if readTrim(s, v) != "17e9" {
			continue
		}
		if s.Exists("/sys/module/evdi") {
			return nil
		}
		product := readTrim(s, filepath.Dir(v)+"/product")
		if product == "" {
			product = "A DisplayLink dock or adapter"
		}
		return []doctor.Finding{{
			Status: doctor.Fail,
			Title:  product + " is plugged in, but its driver isn't installed",
			Explain: "This dock sends the picture over USB with DisplayLink technology. It needs an extra driver from the manufacturer, " +
				"so monitors connected to it stay black until it is installed. Monitors on the laptop's own HDMI or USB-C ports aren't affected.",
			Fixes: []doctor.Fix{{Title: "Install the DisplayLink driver for your distro", URL: "https://www.synaptics.com/products/displaylink-graphics/downloads"}},
		}}
	}
	return nil
}

func thunderbolt(s sys.System) []doctor.Finding {
	devs, _ := s.Glob("/sys/bus/thunderbolt/devices/*/authorized")
	var fs []doctor.Finding
	for _, a := range devs {
		if readTrim(s, a) != "0" {
			continue
		}
		dir := filepath.Dir(a)
		name := strings.TrimSpace(readTrim(s, dir+"/vendor_name") + " " + readTrim(s, dir+"/device_name"))
		if name == "" {
			name = "A Thunderbolt device"
		}
		f := doctor.Finding{
			Status:  doctor.Warn,
			Title:   name + " is connected but not allowed yet",
			Explain: "For security, Thunderbolt docks and devices must be approved before they work. Until then, monitors, network and USB ports on the dock stay dead.",
			Fixes:   []doctor.Fix{{Title: "Approve it in Settings, under Privacy or Thunderbolt"}},
		}
		if id := readTrim(s, dir+"/unique_id"); id != "" && s.Has("boltctl") {
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Or approve it from here, and remember it", Command: "boltctl enroll " + id})
		}
		fs = append(fs, f)
	}
	return fs
}
