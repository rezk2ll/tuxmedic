package checks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func kernelLog(msgs ...string) string {
	var b strings.Builder
	for _, m := range msgs {
		line, _ := json.Marshal(map[string]string{"MESSAGE": m, "PRIORITY": "3", "_TRANSPORT": "kernel", "__REALTIME_TIMESTAMP": "1761900000000000"})
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestBluetooth(t *testing.T) {
	h := &doctor.Host{ID: "arch", PackageMgr: "pacman", Laptop: true}
	klog := "journalctl --no-pager -q -o json -b -k -p 0..4"
	fake := func(log string) *sys.Fake {
		return &sys.Fake{
			Files:    map[string]string{"/sys/class/bluetooth/hci0/address": "00:00:00:00:00:00\n"},
			Commands: map[string]string{klog: log, "bluetoothctl show": "Controller 00:00:00:00:00:00\n\tPowered: yes\n"},
		}
	}

	t.Run("ready", func(t *testing.T) {
		expect(t, run(t, Bluetooth, fake(kernelLog("usb 3-1: new device")), h), doctor.OK, "Bluetooth is on and ready")
	})
	t.Run("realtek firmware missing", func(t *testing.T) {
		r := run(t, Bluetooth, fake(kernelLog("Bluetooth: hci0: RTL: firmware file rtl_bt/rtl8852bu_fw.bin not found")), h)
		expect(t, r, doctor.Fail, "firmware didn't load")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo pacman -S linux-firmware" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("intel firmware file", func(t *testing.T) {
		r := run(t, Bluetooth, fake(kernelLog("Direct firmware load for intel/ibt-0040-0041.sfi failed with error -2")), h)
		expect(t, r, doctor.Fail, "firmware didn't load")
	})
	t.Run("wifi firmware isn't blamed on bluetooth", func(t *testing.T) {
		expect(t, run(t, Bluetooth, fake(kernelLog("Direct firmware load for iwlwifi-so-a0-gf-a0-89.ucode failed with error -2")), h), doctor.OK, "ready")
	})
	t.Run("chip hung", func(t *testing.T) {
		expect(t, run(t, Bluetooth, fake(kernelLog("Bluetooth: hci0: command 0xfc05 tx timeout")), h), doctor.Warn, "stopped answering")
	})
	t.Run("airplane mode wins", func(t *testing.T) {
		f := fake(kernelLog("Bluetooth: hci0: command 0xfc05 tx timeout"))
		f.Files["/sys/class/rfkill/rfkill1/type"] = "bluetooth\n"
		f.Files["/sys/class/rfkill/rfkill1/soft"] = "1\n"
		expect(t, run(t, Bluetooth, f, h), doctor.Fail, "turned off (airplane mode?)")
	})
}
