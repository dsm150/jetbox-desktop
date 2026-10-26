# Code signing policy

**Status:** we are applying for free code signing for open-source projects from
[SignPath Foundation](https://signpath.org/) (application in progress). Until it is approved, Windows releases
are **not signed**. After approval this page will say:

> Free code signing provided by [SignPath.io](https://about.signpath.io/), certificate by
> [SignPath Foundation](https://signpath.org/).

macOS releases are signed with an Apple Developer ID (Team ID `U2ZLMVKGQR`) and notarized by Apple; this is
separate from SignPath.

## What gets signed

Only files built by this repository's [GitHub Actions workflow](.github/workflows/windows.yml) from a version tag:

| File | What it is |
|---|---|
| `JetBox.exe` | the desktop app (this repository, `app/`) |
| `jetboxd.exe` | the JetBox service: NetBird client `v0.81.4` built from upstream source with [`daemon/patches`](daemon/patches) |
| `JetBox-Setup.exe` | the NSIS installer (`packaging/windows`) |

Third-party binaries are shipped unmodified and are **not** signed by us: `wintun.dll` (WireGuard LLC, already
signed by its vendor) and the Microsoft Edge WebView2 bootstrapper (signed by Microsoft).

## Team roles

* Committers and reviewers: [Alexander Makhotin](https://github.com/dsm150)
* Approvers: [Alexander Makhotin](https://github.com/dsm150)

Every release is approved manually. Multi-factor authentication is required for all team members on GitHub and
SignPath.

## Privacy policy

This program will not transfer any information to other networked systems unless specifically requested by the
user or the person installing or operating it.

To connect, the JetBox service talks to the JetBox server (`netbird.jet-net.net`) only after the user opens a
connection link and presses **Connect**. It sends what is needed to register the device and keep the
connection: the device name `JetBox-Windows` / `JetBox-Mac` (not the computer name), operating system and app
versions, the device's WireGuard public key, and its network addresses. No hardware serial numbers, MAC addresses
or traffic content are sent. **Unlink this computer** in the app's settings, or uninstalling the app, removes
the device from the server. Full policy: [jet-net.net/privacy](https://jet-net.net/privacy). Download page:
[jet-net.net/jetbox/computer](https://jet-net.net/jetbox/computer).
