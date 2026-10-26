# Установщик JetBox для Mac (`.pkg`)

Всё сразу (служба, приложение, .pkg, все тесты, эталон в `releases/`): `sh packaging/macos/release.sh <X.Y.Z> [сборка]`.
Эталоны выложенных установщиков — `JetBox_Desktop/releases/` (вне git, `SHA256SUMS` рядом; имя
`JetBox-<версия>-b<сборка>.pkg`); пробные `--unsigned`/`--no-notarize` пишутся как `JetBox-<версия>-unsigned.pkg` /
`-not-notarized.pkg` и релизные файлы не затирают. По шагам:

```sh
sh daemon/scripts/build.sh darwin             # служба → build-out/darwin-universal/jetboxd
# приложение → build-out/darwin-universal/JetBox.app (app/build.sh)
sh packaging/macos/build-pkg.sh               # подпись + нотаризация → build-out/macos/JetBox-<версия>.pkg, JetBox.pkg
sh packaging/macos/test.sh                    # состав, launchd, подписи, Gatekeeper
```

Пока приложения нет — `build-pkg.sh --daemon-only --version 0.0.x`: пакет только со службой (для прогонов
службы на живых Mac). Без сети — `--no-notarize`, без идентичностей — `--unsigned`. Ветку с приложением
проверять поддельным: `JETBOX_APP=$(sh packaging/macos/testdata/make-fake-app.sh /tmp/x) sh packaging/macos/build-pkg.sh --no-notarize`.

## Что ставит

| Путь | Что |
|---|---|
| `/Applications/JetBox.app` | приложение (app/) |
| `/Library/Application Support/JetBox/jetboxd`, `uninstall.sh` | служба (root:wheel, **вне бандла**: её не подменить без пароля через `/Applications`, и она не пропадает, если приложение перетащили в корзину) |
| `/Library/LaunchDaemons/net.jet-net.jetbox.daemon.plist` | служба под launchd (root), `RunAtLoad`, `KeepAlive` |
| `/Library/LaunchAgents/net.jet-net.jetbox.desktop.plist` | автозапуск приложения при входе каждого пользователя: `<CFBundleExecutable> --autostart` (только значок в строке меню), сеанс Aqua, без `KeepAlive`; приложение само Login Item не регистрирует |
| `/var/run/jetbox.sock` | канал gRPC (0666 — создаёт служба; приложение подключается без прав администратора) |
| `/var/lib/jetbox` (700) | состояние службы (`NB_STATE_DIR`, `NB_DNS_STATE_FILE`) — переживает обновления |
| `/var/log/jetbox/` | `client.log` службы, `install.log` скриптов установки |

`preinstall` снимает автозапуск у вошедшего пользователя, закрывает приложение и останавливает службу;
`postinstall` создаёт каталоги и права, загружает службу (`scripts/launchd.sh`: `enable` → `bootout` с
**ожиданием выгрузки** → `bootstrap` с повторами, если задание осталось загруженным — `kickstart -k`;
🪤 `bootout` асинхронный, сразу после него `bootstrap` падает «5: Input/output error» — так упало обновление
0.0.2 → 0.0.3; тест — `testdata/test-launchd.sh` на поддельном `launchctl`), ждёт канал, поднимает
LaunchAgent в `gui/<uid>` и открывает окно приложения (второй запуск отдаётся первому — SingleInstance).
Обновление — тот же `.pkg` поверх: регистрация компьютера в `/var/lib/jetbox` сохраняется.

Удаление — в приложении «Настройки» → «Удалить JetBox…» (подтверждение и стандартный запрос пароля
администратора macOS) или вручную `sudo sh "/Library/Application Support/JetBox/uninstall.sh"`
(`--keep-state` — оставить регистрацию и не снимать с учёта; `--user <имя>` — чьи данные чистить, по умолчанию все).
Снимает компьютер с учёта (пока служба жива), убирает службу, автозапуск, приложение, каталоги, свои ключи scutil
`JetBox-*`, квитанцию пакета и данные пользователей (`~/Library/Application Support/JetBox`, кэши, данные веб-вида,
настройки, состояние окна). Чужое (в т.ч. официальный NetBird) не трогает. Тест — `testdata/test-uninstall.sh`
(песочница, поддельные launchctl/scutil, root не нужен).

Приложение вызывает удаление так (без оболочки, имя пользователя — только `[A-Za-z0-9._-]`):
`/usr/bin/osascript -e 'do shell script "/bin/sh \"/Library/Application Support/JetBox/uninstall.sh\" --user <имя>" with administrator privileges with prompt "…"'`.
Не через службу: в её gRPC такого вызова нет, а «удали JetBox от root» на канале 0666 мог бы дёрнуть любой
процесс пользователя; пароль администратора при удалении — норма macOS. Скрипт лежит в каталоге root —
пользовательский процесс подменить его не может.

## Подпись и нотаризация

* Служба и бандл: «Developer ID Application» вашей команды (`JETBOX_APP_SIGN`), hardened runtime, timestamp;
  служба — со своим идентификатором `net.jet-net.jetbox.daemon`; бандл изнутри наружу без `--deep`
  (вложенные `.framework/.app/.xpc/.appex/.bundle` — глубже раньше, затем прочие Mach-O, затем бандл;
  ошибка любой подписи валит сборку). Entitlements не нужны (у NetBird их тоже нет).
* Пакет: `productbuild --sign` «Developer ID Installer» вашей команды (`JETBOX_PKG_SIGN`); дистрибутив с `hostArchitectures="arm64,x86_64"`
  (иначе на Apple Silicon установщик просит Rosetta), macOS 13+ (рантайм Go 1.27; если приложение требует
больше — его `LSMinimumSystemVersion`), компонент с `BundleIsRelocatable=false`.
* Нотаризация: `notarytool` с ключом App Store Connect API из `JETBOX_ASC_DIR` (asc.p8/asc.keyid/asc.issuer),
  ~1 мин; `stapler staple`. Значения переменных — вне репо: окружение или локальный файл
  `~/.config/jetbox-desktop/macos-sign.env` (`JETBOX_SIGN_ENV`), строки `KEY='значение'`.

## Имена (те же, что в daemon/brand.env и core/platform; менять только вместе)

launchd `net.jet-net.jetbox.daemon` · канал `unix:///var/run/jetbox.sock` · `/var/lib/jetbox` ·
`/Applications/JetBox.app` · bundle ID `net.jet-net.jetbox.desktop` · `NB_ROUTE_PROTO_FLAG=2` (служба не
трогает маршруты официального NetBird) · интерфейс `utun120`/51830 — задаёт приложение через `SetConfig`.
