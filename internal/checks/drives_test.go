package checks

import (
	"errors"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const lsblkCmd = "lsblk -J -o NAME,PATH,FSTYPE,MOUNTPOINT,TRAN,HOTPLUG,LABEL,SIZE,TYPE"

func lsblk(children string) string {
	return `{"blockdevices":[
	 {"name":"nvme0n1","path":"/dev/nvme0n1","fstype":null,"mountpoint":null,"tran":"nvme","hotplug":false,"label":null,"size":"512G","type":"disk","children":[
	   {"name":"nvme0n1p2","path":"/dev/nvme0n1p2","fstype":"ext4","mountpoint":"/","tran":null,"hotplug":false,"label":null,"size":"500G","type":"part"}]},
	 {"name":"sda","path":"/dev/sda","fstype":null,"mountpoint":null,"tran":"usb","hotplug":"1","label":null,"size":"32G","type":"disk","children":[` + children + `]}]}`
}

func TestDrives(t *testing.T) {
	h := &doctor.Host{ID: "debian", PackageMgr: "apt", Kernel: "6.12.0"}
	withNTFS := map[string]string{"/proc/filesystems": "nodev\tsysfs\n\tvfat\n\tntfs3\n"}

	t.Run("mounted", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{lsblkCmd: lsblk(
			`{"name":"sda1","fstype":"vfat","mountpoint":"/media/u/STICK","label":"STICK","size":"32G","type":"part"}`)}}
		expect(t, run(t, Drives, f, h), doctor.OK, `USB drives open: "STICK"`)
	})
	t.Run("phone without mtp support", func(t *testing.T) {
		const d = "/sys/bus/usb/devices/3-1"
		phone := files(d+"/manufacturer", "Acme", d+"/product", "Phone 9", d+":1.0/interface", "MTP")
		none := map[string]string{lsblkCmd: `{"blockdevices":[]}`}
		f := &sys.Fake{Files: phone, Commands: none}
		gnome := &doctor.Host{ID: "debian", PackageMgr: "apt", Desktop: "GNOME"}
		r := run(t, Drives, f, gnome)
		expect(t, r, doctor.Warn, "Acme Phone 9 is plugged in but doesn't show up in the file manager")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo apt install gvfs-backends" {
			t.Errorf("fix = %q", c)
		}
		f.Files["/usr/libexec/gvfsd-mtp"] = ""
		expect(t, run(t, Drives, f, gnome), doctor.OK, "No USB drive is plugged in")

		kde := &doctor.Host{ID: "arch", PackageMgr: "pacman", Desktop: "KDE"}
		f = &sys.Fake{Files: phone, Commands: none}
		if c := run(t, Drives, f, kde).Findings[0].Fixes[0].Command; c != "sudo pacman -S kio-extras" {
			t.Errorf("kde fix = %q", c)
		}
		f.Files["/usr/lib/qt6/plugins/kf6/kio/kio_mtp.so"] = ""
		expect(t, run(t, Drives, f, kde), doctor.OK, "No USB drive is plugged in")
	})
	t.Run("linux drive owned by root", func(t *testing.T) {
		ext4 := lsblk(`{"name":"sda1","path":"/dev/sda1","fstype":"ext4","mountpoint":"/media/u/My Backup","label":"My Backup","size":"32G","type":"part"}`)
		f := &sys.Fake{Commands: map[string]string{lsblkCmd: ext4}, Failing: map[string]error{"test -w /media/u/My Backup": errors.New("exit status 1")}}
		r := run(t, Drives, f, h)
		expect(t, r, doctor.Warn, `You can open the drive "My Backup" but not save to it`)
		if got := r.Findings[0].Fixes[0].Command; got != `sudo chown "$USER": '/media/u/My Backup'` {
			t.Errorf("fix = %q", got)
		}
		f = &sys.Fake{Commands: map[string]string{lsblkCmd: ext4, "test -w /media/u/My Backup": ""}}
		expect(t, run(t, Drives, f, h), doctor.OK, `USB drives open: "My Backup"`)
	})
	t.Run("ntfs read-only", func(t *testing.T) {
		f := &sys.Fake{
			Files: map[string]string{
				"/proc/filesystems": withNTFS["/proc/filesystems"],
				"/proc/mounts":      "/dev/sda1 /media/u/My\\040Files ntfs3 ro,nosuid,nodev 0 0\n",
			},
			Commands: map[string]string{lsblkCmd: lsblk(
				`{"name":"sda1","path":"/dev/sda1","fstype":"ntfs","mountpoint":"/media/u/My Files","label":"My Files","size":"32G","type":"part"}`)},
		}
		r := run(t, Drives, f, h)
		expect(t, r, doctor.Warn, `The drive "My Files" opened read-only`)
		if got := r.Findings[0].Fixes[2].Command; got != "sudo umount /dev/sda1 && sudo ntfsfix -d /dev/sda1" {
			t.Errorf("fix = %q", got)
		}
	})
	t.Run("ntfs without driver", func(t *testing.T) {
		f := &sys.Fake{
			Files:    map[string]string{"/proc/filesystems": "\tvfat\n"},
			Commands: map[string]string{lsblkCmd: lsblk(`{"name":"sda1","fstype":"ntfs","label":"BACKUP","size":"32G","type":"part"}`)},
		}
		r := run(t, Drives, f, h)
		expect(t, r, doctor.Fail, `Linux can't open the drive "BACKUP": it uses NTFS`)
		if got := r.Findings[0].Fixes[0].Command; got != "sudo apt install ntfs-3g" {
			t.Errorf("fix = %q", got)
		}
	})
	t.Run("exfat module on disk counts", func(t *testing.T) {
		f := &sys.Fake{
			Files: map[string]string{
				"/proc/filesystems": "\tvfat\n",
				"/lib/modules/6.12.0/kernel/fs/exfat/exfat.ko.zst": "",
			},
			Commands: map[string]string{lsblkCmd: lsblk(`{"name":"sda1","fstype":"exfat","label":"CAM","size":"32G","type":"part"}`)},
		}
		expect(t, run(t, Drives, f, h), doctor.Info, `The drive "CAM" is plugged in but not opened`)
	})
	t.Run("windows partition read-only on the internal disk", func(t *testing.T) {
		f := &sys.Fake{
			Files: map[string]string{
				"/proc/filesystems": withNTFS["/proc/filesystems"],
				"/proc/mounts":      "/dev/nvme0n1p3 /mnt/windows fuseblk ro,relatime 0 0\n",
			},
			Commands: map[string]string{lsblkCmd: `{"blockdevices":[{"name":"nvme0n1","tran":"nvme","hotplug":"0","size":"1T","type":"disk","children":[
				{"name":"nvme0n1p3","fstype":"ntfs","mountpoint":"/mnt/windows","label":"Windows","size":"400G","type":"part"}]}]}`},
		}
		expect(t, run(t, Drives, f, h), doctor.Warn, `The drive "Windows" opened read-only`)
	})
	t.Run("blank card", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{lsblkCmd: `{"blockdevices":[{"name":"sdb","tran":"usb","hotplug":true,"size":"64G","type":"disk"}]}`}}
		expect(t, run(t, Drives, f, h), doctor.Info, "The 64G drive /dev/sdb has nothing Linux can read on it")
	})
	t.Run("nothing plugged in", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{lsblkCmd: `{"blockdevices":[]}`}}
		expect(t, run(t, Drives, f, h), doctor.OK, "No USB drive is plugged in")
	})
	t.Run("no lsblk", func(t *testing.T) {
		expect(t, run(t, Drives, &sys.Fake{}, h), doctor.Skipped, "lsblk is missing")
	})
}
