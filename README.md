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
| `app/` | desktop app (Wails v3): window + tray / menu bar icon; UI in `app/frontend` (React); fixes to Wails in [`app/wails/patches`](app/wails/patches), applied at build time by `app/wails/prepare.sh` |
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

## Replacing the connection

The device's identity in NetBird is its WireGuard key. When the user opens a link with a **different setup key**
while the computer is registered, it registers the computer under a **new identity** (for a link of another JetBox or
account it asks first; a new link of the same JetBox connects without asking): a new
service profile (`jetbox-<unixtime>`, new WireGuard key) is configured and logged in with the new key, and only
then becomes the active one. If the login fails or is cancelled, the previous profile is made active again (its
config is never touched) and the new one is removed. The previous profile and its peer are left
untouched — the account (jet-net.net) removes the old peer. The same link opened again keeps the current identity.

**Limitation — several Windows users.** Service profiles belong to the Windows user who created them, while the
service (and the active profile) is one per computer. If a second Windows user replaces the connection, the active
profile becomes theirs, and the first user's app then shows and controls that connection: the app does not switch
another user's profile back (it only logs the mismatch with `box.json`). The app does not promise anything about this
case.

**Interrupted replacement.** `replace.json` next to `box.json` is written before the new profile is created and
removed once the new connection is saved (or rolled back). If it is still there — the app was closed or the computer
lost power midway — the app finishes the rollback at start (and before connecting): the profile from `box.json` is made
active again and the half-registered new profile is removed; `replace.json` is deleted only once that profile is
gone. Protection is not turned on by itself. A `box.json` whose service profile no longer exists (reinstall) is
treated as no connection.

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

**Замена подключения.** Ссылка с другим setup key при живой регистрации (другой JetBox или абонент — после
подтверждения, новая ссылка того же JetBox — без вопроса) — компьютер встаёт
под новой личностью: новый профиль службы с новым ключом WireGuard; отказ входа — откат на прежний профиль.
Прежний пир не трогаем — его убирает Кабинет. **Ограничение:** профили службы привязаны к пользователю Windows,
а служба одна на компьютер — замена вторым пользователем делает активным его профиль, и первый пользователь
увидит его подключение: чужой профиль приложение назад не переключает, только пишет расхождение в журнал.
Прерванную замену (осталась replace.json) приложение доводит откатом при запуске; box.json, чьего профиля в
службе нет (переустановка), считается отсутствующим.
