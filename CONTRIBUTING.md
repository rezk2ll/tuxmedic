# Contributing

Bug reports, new checks and fixes for distros we got wrong are welcome. For anything bigger, open an issue first so we can agree on the approach before you write it.

## Build and try your change

You need Go 1.26 or newer.

```sh
go run ./cmd/tuxmedic          # the interactive screen
go run ./cmd/tuxmedic check    # plain output
go test ./...
```

CI also runs `go vet`, `staticcheck` and `golangci-lint`, and runs the binary inside Fedora, Arch, Debian, Ubuntu, Alpine, openSUSE and Void containers.

## Adding or changing a check

A check lives in `internal/checks` and reads the machine only through `sys.System`. That interface lets the tests replay a machine from fake files and command output, so every check can be tested on any computer.

- Write the question a user would ask, not the subsystem name: "Why is there no sound?", not "Audio".
- A finding says what is wrong in plain words, why it matters, and what to do. Give the exact command for the user's package manager when there is one.
- Never claim a fault you can't see. If the data isn't there (no permission, no systemd, a container), report the check as skipped and say why.
- Add a test with a synthetic fixture. Never paste logs, host names, serial numbers or addresses from a real machine.

## Pull requests

- One change per pull request, with a commit message that says why.
- For a visible change in the interactive screen, attach a screenshot.
- If you fixed something for a specific distro, say which one and how you tested it.

## Releases

The maintainer pushes a `v<version>` tag. CI builds static binaries and `.deb`, `.rpm`, `.apk` and Arch packages for amd64 and arm64, signs the checksums, and attaches everything to the GitHub release.

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).
