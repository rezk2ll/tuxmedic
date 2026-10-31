<div align="center">

# tuxmedic

Find out what's wrong with your Linux computer, and how to fix it.

[![CI](https://github.com/rezk2ll/tuxmedic/actions/workflows/ci.yml/badge.svg)](https://github.com/rezk2ll/tuxmedic/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/rezk2ll/tuxmedic)](https://github.com/rezk2ll/tuxmedic/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/rezk2ll/tuxmedic)](go.mod)
[![License: GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-blue)](LICENSE)

![tuxmedic overview](docs/overview.png)

</div>

The laptop froze last night, the Wi-Fi keeps dropping, the battery is stuck at 80%, there's no sound over HDMI. Linux logs the cause of most of these somewhere, but finding it means knowing which of a dozen tools to ask and how to read what they say.

tuxmedic asks them for you. It runs 27 checks, usually in under a second, and tells you what it found in plain words, with the exact command to fix it on your distro.

## What it checks

Each check answers a question people actually ask:

- Crashes & freezes: why did my computer crash, freeze or restart by itself? It reads the previous sessions' logs and names the cause: out of memory, graphics card hang, kernel panic, overheating. When the crashes began right after a kernel, graphics or desktop update, it says which one and how to go back.
- Internet & Wi-Fi: why can't I get online? It tells apart airplane mode, a router with no internet, broken DNS and hotel sign-in pages, and notices a connection that keeps dropping or an IPv6 setup that makes every site stall.
- Sound: why is there no sound? Muted output, the wrong default device, a monitor with no HDMI profile, a Bluetooth headset stuck in call quality, missing firmware.
- Camera & microphone: why don't they work in video calls? Another app holding the camera, the GNOME privacy switches, a muted or silent microphone, apps recording the speakers instead of the mic, Intel IPU laptop cameras that need extra drivers.
- Screen sharing & app sandbox: why is my shared screen black on Wayland? A missing or crashed desktop portal, no PipeWire, Flatpak apps with no Open and Save dialog, stuck snap updates and old snap versions eating disk space, editors and sync apps out of file watches ("Unable to watch for file changes").
- Screens & monitors: why does my second monitor stay black? A monitor switched off in settings, a cable that doesn't pass the monitor's details, a DisplayLink dock without its driver, a Thunderbolt dock waiting for approval, a laptop screen whose brightness can't be changed (with the kernel setting that fixes it, for your boot loader).
- Keyboard & typing: why do keys type late or not at all? Slow Keys or Sticky Keys switched on by holding Shift, and input methods for other languages (IBus, Fcitx) that apps expect but aren't running, or that run twice.
- Touchpad: why doesn't it move the pointer? A touchpad Linux doesn't see at all, or one the desktop switched off (by a key, or while a mouse is plugged in).
- Fingerprint reader: why can't I log in with my finger? fprintd missing, a reader Linux has no driver for (with its USB ID to look up), no finger saved yet, or a saved finger that sudo never asks for.
- USB drives & Windows disks: why won't it open, or why is it read-only? A missing NTFS or exFAT driver, an NTFS drive Windows Fast Startup left marked as in use, a stick that is plugged in but not mounted.
- Printers: why can't I print? A stopped printing service, a printer CUPS paused after an error, jobs stuck in the queue, a network printer that is ready but not added yet.
- Battery & charging: why isn't it charging? A charge limit set on purpose, a charger too weak for the laptop, a worn battery, fast drain.
- Disks & storage: is my disk full or failing? Free space and the cleanups that work on your distro, a disk switched to read-only, drive health (SMART) without root.
- Drivers & graphics: is some hardware missing a driver? NVIDIA with Secure Boot, Broadcom Wi-Fi, missing firmware.
- Video playback: why do videos stutter and the fans spin up? No VA-API driver for your Intel, AMD or NVIDIA graphics, so the processor decodes every frame.
- Games & Steam: why won't my games start? No Vulkan driver for Proton, the 32-bit driver Steam needs, Flatpak Steam without the controller rules.
- Recent changes: it worked last week, what changed? Reads the update history (apt, pacman, rpm) and names the kernel, graphics, firmware, sound or desktop update most likely behind a new problem, with the way back.
- Also: sleep and wake (including a lid that is set to do nothing), memory and swap, temperatures and fans, pending updates and stuck package managers, slow startup and failed services, the clock, languages that were never installed ("Setting locale failed"), emoji drawn as boxes, links, folders and PDFs whose default app was uninstalled, Bluetooth, groups that only work after you log out, and files in your home folder left owned by root.

![A check with its fixes and the log lines behind it](docs/detail.png)

## Install

Download the latest release and check it against the signed checksums:

```sh
curl -fsSL https://raw.githubusercontent.com/rezk2ll/tuxmedic/main/install.sh | sh
```

Or grab a package for your distro from the [releases page](https://github.com/rezk2ll/tuxmedic/releases/latest): `.deb`, `.rpm`, `.apk`, Arch `.pkg.tar.zst`, or a plain static binary for amd64, arm64 and riscv64.

With Go 1.26 or newer:

```sh
go install github.com/rezk2ll/tuxmedic/cmd/tuxmedic@latest
```

## Use

```sh
tuxmedic             # the interactive screen
tuxmedic ask "zoom can't hear me"   # only the checks your problem is about
tuxmedic watch       # record Wi-Fi drops and overheating while they happen
tuxmedic check       # print the results, for scripts and pipes
tuxmedic check sound battery
tuxmedic report      # a Markdown report to paste on a forum
tuxmedic check --json
```

In the interactive screen, press a number to open a check, then a number to run or copy one of its fixes. `e` shows the log lines a finding is based on, `/` lets you describe the problem in your own words and keeps the checks it is about, `w` watches for drops while you keep working, `ctrl+p` lists every command, and `?` shows all the keys. The mouse works too.

`tuxmedic watch` is for problems that come and go. Leave it running until the Wi-Fi drops again, then press Ctrl+C. It tells apart a weak signal, this computer dropping a good connection, and the router or provider losing the internet while the Wi-Fi stays up. `--for 30m` stops it on its own.

`tuxmedic check` exits with 1 when it finds a problem, so it works in scripts.

The packages and the install script add `man tuxmedic` and tab completion for bash, zsh and fish. With `go install`, add completion yourself, for example `tuxmedic completion bash > ~/.local/share/bash-completion/completions/tuxmedic`.

## Requirements

- Any Linux distro. CI installs and runs it on Ubuntu, Debian, Fedora, Arch, openSUSE, Alpine and Void. Checks that need systemd say so and step aside on systems without it. On image-based systems (Silverblue, Kinoite, Bazzite, Aeon, MicroOS) the fixes use rpm-ostree or transactional-update, and NixOS gets a pointer to the package instead of an install command.
- Any terminal, from 40×10 up. On the Linux console (`TERM=linux`) or with `--ascii`, it uses plain ASCII. When the output isn't a terminal, it prints plain text.
- No root. Run it as yourself. To see past crashes, your user needs to read the system log: many distros allow that already, otherwise tuxmedic tells you which group to join. `sudo tuxmedic` adds a few checks that need root, such as SMART on drives UDisks can't read.

## Privacy

tuxmedic only reads. It changes nothing unless you pick a fix and press Enter, and then it runs the command it showed you in your terminal. It sends nothing anywhere. The only network traffic is a connection test to 1.1.1.1 and 9.9.9.9 (and their IPv6 addresses, when your network has IPv6) and a DNS lookup of debian.org and kernel.org.

To tell you what changed since last time, tuxmedic keeps one small file, `~/.local/state/tuxmedic/last.json`, with the name and result of each check and nothing else. Delete it whenever you like.

`tuxmedic report` removes your host name, user name, home folder, MAC and IP addresses, emails, UUIDs and device serial numbers, so you can post it publicly. Read it before posting anyway.

## Build from source

```sh
git clone https://github.com/rezk2ll/tuxmedic
cd tuxmedic
go build ./cmd/tuxmedic
./tuxmedic
```

## How it works

Every check is a small function that reads `/proc`, `/sys`, the systemd journal, and tools such as `pactl`, `nmcli`, `busctl` and `systemctl` when they're installed. Checks read the machine through one interface, so the tests can replay a broken machine from fake files and command output. That's how the crash, battery, network and disk logic is tested without a broken laptop.

Checks run in parallel and the screen shows each result as it arrives. A check that hits something unexpected reports itself as skipped instead of taking the program down.

Why a terminal app and not a window: when the desktop is the thing that's broken, a terminal still works. It also works over SSH, in a text console, on any desktop, and on a friend's computer you're fixing for them.

## License

[GPL-3.0](LICENSE)

## Contributing

Found a problem tuxmedic missed, or a wrong diagnosis? [Open an issue](https://github.com/rezk2ll/tuxmedic/issues/new/choose) with the output of `tuxmedic report`. To add a check or fix one for your distro, see [CONTRIBUTING.md](CONTRIBUTING.md).
