<div align="center">

# tuxmedic

Find out what's wrong with your Linux computer, and how to fix it.

[![CI](https://github.com/rezk2ll/tuxmedic/actions/workflows/ci.yml/badge.svg)](https://github.com/rezk2ll/tuxmedic/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/rezk2ll/tuxmedic)](https://github.com/rezk2ll/tuxmedic/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/rezk2ll/tuxmedic)](go.mod)
[![License: GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-blue)](LICENSE)

![tuxmedic overview](docs/overview.png)

</div>

The Wi-Fi keeps dropping, there's no sound over HDMI, the laptop froze last night. Linux usually logged why, but finding it means knowing which tool to ask.

tuxmedic asks them for you. It runs 30 checks, usually in under a second, and tells you what it found in plain words, with the command that fixes it on your distro.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/rezk2ll/tuxmedic/main/install.sh | sh
```

The script checks the download against signed checksums. There are also `.deb`, `.rpm`, `.apk` and Arch packages, and static binaries for amd64, arm64 and riscv64, on the [releases page](https://github.com/rezk2ll/tuxmedic/releases/latest). With Go 1.26 or newer: `go install github.com/rezk2ll/tuxmedic/cmd/tuxmedic@latest`.

## Use

```sh
tuxmedic                          # the interactive screen
tuxmedic ask "zoom can't hear me" # only the checks your problem is about
tuxmedic watch                    # catch Wi-Fi drops and overheating as they happen
tuxmedic check sound battery      # plain output, exits 1 on a problem
tuxmedic report                   # Markdown to paste on a forum, personal data removed
```

In the interactive screen, press a number to open a check and run one of its fixes. Press `?` for every key.

## What it checks

- Crashes and freezes, and the update that started them
- Internet and Wi-Fi: no address, broken DNS, hotel sign-in pages, a connection that keeps dropping
- Sound, camera and microphone
- Screens, touchpad, keyboard, fingerprint reader, Bluetooth, printers, USB drives
- Battery, charging, temperatures, sleep and wake
- Disks, memory, updates, drivers, video playback
- Games and Steam, virtual machines and containers, Flatpak and snap apps
- Notifications, the clock, fonts, permissions

## Requirements

- Any distro. CI runs it on Ubuntu, Debian, Fedora, Arch, openSUSE, Alpine and Void.
- Any terminal from 40×10 up, including the Linux console.
- No root. `sudo tuxmedic` adds a few checks that need it.

## Privacy

tuxmedic only reads. It changes nothing unless you pick a fix and press Enter, and it sends nothing anywhere. Its only network traffic is a connection test to 1.1.1.1 and 9.9.9.9, a DNS lookup of debian.org and kernel.org, and, when a proxy is set, a connection to that proxy.

It keeps one file, `~/.local/state/tuxmedic/last.json`, with each check's result, to tell you what changed since last time.

## License

[GPL-3.0](LICENSE). To report a wrong diagnosis or add a check, see [CONTRIBUTING.md](CONTRIBUTING.md).
