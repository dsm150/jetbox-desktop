# JetBox for Windows and macOS

JetBox connects a computer to the user's own **JetBox** home gateway with one button. The user opens a
connection link (`jetbox://connect#…`) from their account at [jet-net.net](https://jet-net.net), presses
**Connect**, and the computer joins their private network over WireGuard; internet traffic can be routed through
the home gateway. The app shows one shield button (protected / off), the connection to home and a tray (menu bar)
icon.

JetBox is a thin client on top of the open-source [NetBird](https://github.com/netbirdio/netbird) client
service (BSD-3-Clause). The service is built **from upstream source** (tag `v0.81.4`) with a small set of patches
in [`daemon/patches`](daemon/patches): JetBox branding and side-by-side install with the official NetBird client,
no upstream auto-update, no hardware identifiers sent to the server.

| Directory | What |
|---|---|
| `core/` | shared Go code: connection link parsing, service client (gRPC), connect flow, "ping to home", other-VPN check |
| `app/` | desktop app (Wails v3): window + tray / menu bar icon; UI in `app/frontend` (React) |
| `daemon/` | the JetBox service: upstream NetBird tag + patches ([`daemon/JETBOX-BUILD.md`](daemon/JETBOX-BUILD.md)) |
| `packaging/windows` | NSIS installer `JetBox-Setup.exe` |
| `packaging/macos` | `.pkg` installer (signing, notarization) |
| `.github/workflows` | CI: Windows installer built from a tag |

## What the Windows installer changes

The installer says this on its first page, before anything is installed:

* installs the JetBox app (`%ProgramFiles%\JetBox`) and starts it at sign-in (tray icon only);
* installs the **JetBox** Windows service (runs as LocalSystem) and its data folder `%ProgramData%\JetBox`;
* the service creates the **JetBox** VPN network adapter (Wintun driver) and a Windows Firewall rule;
* registers the `jetbox://` link handler;
* installs Microsoft Edge WebView2 if it is missing.

Uninstall (*Settings → Apps → JetBox*, or `Uninstall.exe /S`) unlinks the computer from JetBox and removes all of
the above: the service, the adapter, firewall and DNS (NRPT) rules, the link handler, program files and data.

## Build

Requirements: Go 1.26+, Node.js + pnpm, NSIS 3 (Windows installer), Xcode (macOS app).

```sh
sh test.sh                                     # frontend, go vet/test (core, app), service tests
SKIP_DAEMON=1 sh test.sh                       # quick, without service tests
app/build.sh windows 1.0.2 4                   # build-out/windows-amd64/JetBox.exe
sh daemon/scripts/build.sh windows             # build-out/windows-amd64/jetboxd.exe (upstream tag + patches)
packaging/windows/build.sh 1.0.2 4             # build-out/windows-amd64/JetBox-Setup.exe
```

Without a local `makensis`, `NSIS_REMOTE=user@windows-host` builds the installer on a Windows machine over ssh
(`NSIS_SSH_OPTS` for ssh/scp options). Releases are built by [GitHub Actions](.github/workflows/windows.yml)
from a version tag.

## License

BSD-3-Clause — [`LICENSE`](LICENSE). Third-party components — [`NOTICE`](NOTICE).
Code signing — [`CODE_SIGNING_POLICY.md`](CODE_SIGNING_POLICY.md).

---

## По-русски

JetBox подключает компьютер к вашему JetBox одной кнопкой: ссылка `jetbox://connect#…` из личного кабинета на
[jet-net.net](https://jet-net.net) → «Подключить» → «Под защитой». Работает поверх службы на основе открытого
клиента NetBird (BSD-3-Clause), собранной из исходников тега `v0.81.4` с патчами из `daemon/patches`.
Как собрать — раздел **Build** выше; лицензия — BSD-3-Clause.
