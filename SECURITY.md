# Security

tuxmedic runs as your user and only reads. It reads files under `/proc`, `/sys`, `/etc` and `/run`, runs read-only commands such as `journalctl`, `pactl`, `busctl` and `systemctl --failed`, and opens two network connections to test your internet (a TCP connect to `1.1.1.1:443` and `9.9.9.9:443`, and DNS lookups of `debian.org` and `kernel.org`). It sends no data anywhere.

It changes nothing on its own. A fix only runs when you pick it in the interactive screen and press Enter, and it runs in your terminal where you see the command first. Fixes that need root call `sudo`, which asks for your password. tuxmedic never sees or stores it.

`tuxmedic report` and `tuxmedic check --json` remove host names, user names, home paths, MAC and IP addresses, email addresses, UUIDs and device serial numbers. Read the report before you post it anyway.

If you find a security issue, report it privately through [GitHub security advisories](https://github.com/rezk2ll/tuxmedic/security/advisories/new) instead of a public issue.

Only the latest release is supported.
