# Служба JetBox (`jetboxd`) — сборка из апстрима NetBird

Служба JetBox — это клиентская служба NetBird ровно тега `v0.81.4` (BSD-3) плюс три патча файлами.
Приложение JetBox (`app/`, `core/`) говорит с ней только по gRPC-контракту апстрима
(`client/proto/daemon.proto` тега) — его патчи не трогают.

## 1. Как собрать

```sh
sh daemon/scripts/build.sh            # обе ОС; или: build.sh darwin | build.sh windows
sh daemon/scripts/test.sh             # патчи на чистый тег во временной копии, тесты, -X обеих ОС, vet Windows, smoke без root
```

| Выход | Что |
|---|---|
| `build-out/darwin-amd64/jetboxd`, `build-out/darwin-arm64/jetboxd` | macOS, без cgo (как апстрим) |
| `build-out/darwin-universal/jetboxd` | `lipo` из двух — его кладёт в `.pkg` `packaging/macos` |
| `build-out/windows-amd64/jetboxd.exe` | Windows, `-tags load_wgnt_from_rsrc`, свой GUID адаптера, ресурсы exe «JetBox» |
| `build-out/jetboxd.version` | версия службы |

Нужно: Go ≥ версии из `go.mod` тега, `lipo` (Xcode CLT), сеть до `proxy.golang.org` при первой сборке
(зип тега, модули, `goversioninfo@v1.7.0` — ставится в `daemon/upstream/bin`). Всё, что качается, лежит в
`daemon/upstream/` (в `.gitignore`). Подписи здесь нет: macOS подписывает `packaging/macos`.

## 2. Откуда исходник

`daemon/upstream.env`: тег, адрес зипа на proxy.golang.org и его **sha256** (сверяется при каждой
подготовке). `scripts/prepare.sh` распаковывает зип в `daemon/upstream/netbird-<тег>`, делает там свой git
(коммит `upstream <тег>`, тег `upstream`) и накладывает `daemon/patches/*.patch` через `git am`.
Дерево пересобирается, только если поменялись патчи (штамп — `daemon/upstream/netbird-<тег>.patches.sha256`,
вне дерева). `--force` пересобирает, но **отказывается**, если в дереве есть незакоммиченные правки или
коммиты, не выгруженные в `daemon/patches` (сверка `format-patch` с файлами), — работа не стирается.

🪤 Файл называется `upstream.env`, а не `UPSTREAM`: на macOS ФС регистронезависимая, `UPSTREAM` и
каталог `upstream/` — одно имя.

## 3. Патчи

| Патч | Что делает | Тесты |
|---|---|---|
| `0001-jetbox-disable-upstream-auto-update` | `client/internal/updater`: `Start` не запускает опрос `pkgs.netbird.io`; директива management «expected version / force update» игнорируется; `Install`/`tryInstall` всегда отказывают. Иначе служба по команде нашего management скачивает **официальный** установщик NetBird и ставит его (поймано на Win11 26.10). | `jetbox_disabled_test.go` — 4 теста; upstream-тесты логики идут с включённым обновлением (`TestMain`) |
| `0002-jetbox-do-not-send-hardware-identifiers-or-the-system-hostname` | `client/system`: не собираются серийный номер, модель, производитель (`ioreg`/WMI не вызываются), в сетевых адресах нет MAC; **системное имя компьютера не отправляется** — имя устройства только из `Login/Up{hostname}` приложения, служба хранит его в каталоге состояния (`jetbox-device-name`), до первого — «JetBox». ОС, версия, IP — как у апстрима. | `jetbox_privacy_test.go`, `jetbox_device_name_test.go` — на живой системе |
| `0004-jetbox-do-not-auto-connect-before-the-first-login` | `client/server`: пока на компьютере не было успешного входа (признак `jetbox-registered` в каталоге состояния, снимается `Logout`), служба при старте не идёт в management — `NeedsLogin` без сетевых попыток | `jetbox_registered_test.go`; smoke `test.sh` (второй старт с профилем) |
| `0003-jetbox-make-system-visible-names…` | новый пакет `client/jetboxbrand` — имена в системе и всё, что служба делит с официальным NetBird, задаются `-X` при сборке, по умолчанию = апстрим (§4) | `jetboxbrand/brand_test.go` (умолчания = апстрим; с `-X` — значения дошли), `dns/jetbox_scutil_darwin_test.go`, `profilemanager/jetbox_legacy_test.go`, `iface/device/jetbox_guid_test.go` |

Версия службы — не патч, а флаг: `-X github.com/netbirdio/netbird/version.version=0.81.4+jetbox.N`.
🔴 Только **метаданные** (`+jetbox.N`), не pre-release: `0.81.4-jetbox…` по semver *меньше* `0.81.4`, и
апстримовая логика обновления сочла бы службу устаревшей (так и было на Win11). `N` — в `upstream.env`.

## 4. Имена (`daemon/brand.env` → `client/jetboxbrand`)

| Переменная | Апстрим | JetBox | Где видно |
|---|---|---|---|
| `ServiceName` | `Netbird` / `netbird` | Win `JetBox`, macOS `net.jet-net.jetbox.daemon` | имя службы по умолчанию для `jetboxd service …` |
| `ServiceDisplayName` | `Netbird` | `JetBox` | «Службы» Windows |
| `ServiceDescription` | `NetBird mesh network client` | `JetBox — подключение к вашему JetBox` | там же |
| `FirewallRuleName` | `Netbird` (+`-v6`) | `JetBox` | брандмауэр Windows |
| `NRPTKeyPrefix` | `NetBird-` | `JetBox-` | DNS-правила Windows; уборка по префиксу |
| `ScutilKeyPrefix` | `NetBird-` | `JetBox-` | `State:/Network/Service/<префикс>…/DNS` macOS; уборка по префиксу |
| `WindowsDataDirName` | `Netbird` | `JetBox` | `%ProgramData%\…`: состояние без `NB_STATE_DIR`, лог паники, tmp-install |
| `UnixDataDir` | `/var/lib/netbird` | `/var/lib/jetbox` | то же на macOS |
| `PortMappingDescription` | `NetBird` | `JetBox` | проброс UPnP на роутере клиента |
| `LegacyUnixConfigDir` | `/etc/netbird/` | `/etc/jetbox/` | 🔴 служба при **первом старте копирует** оттуда `config.json` в свой профиль — с апстримовым значением JetBox подхватил бы ключ WG и management официального NetBird, стоявшего на Mac (находка ревью); CLI держит там конфиг |
| `UnixLogDir`, `UnixRunDir` | `/var/log/netbird/`, `/var/run/netbird` | `/var/log/jetbox/`, `/var/run/jetbox` | значения CLI по умолчанию |
| `DaemonAddrUnix` / `DaemonAddrWindows` | `unix:///var/run/netbird.sock` / апстрим | `unix:///var/run/jetbox.sock` / `npipe://jetbox` | канал по умолчанию для `jetboxd status` и др. — без флага CLI не уйдёт к официальной службе |
| `MDMPlistPath` / `MDMRegistryPath` | `…/io.netbird.client.plist` / `Software\Policies\NetBird` | `…/net.jet-net.jetbox.desktop.plist` / `Software\Policies\JetBox` | MDM-политика (применяется последним слоем: management, порт, PSK, remote jobs) — корпоративная политика NetBird не должна править JetBox |
| `SSHClientConfigFile` | `99-netbird.conf` | `99-jetbox.conf` | `/etc/ssh/ssh_config.d/…` — служба удаляет его при каждой остановке движка |
| `DefaultManagementURL` / `DefaultAdminURL` | `https://api.netbird.io:443` / `https://app.netbird.io:443` | `https://netbird.jet-net.net:443` / `https://box.jet-net.net:443` | адрес в новом профиле, пока приложение не передало свой; свежий JetBox не смотрит в облако NetBird |
| `DarwinLaunchdStdoutLog` / `…StderrLog` | `/var/log/netbird.{out,err}.log` | `/var/log/jetbox/daemon.{out,err}.log` | что собирает отладочный пакет |

Что задаётся **не** при сборке, а установщиком (`packaging/*`): адрес канала (`--daemon-addr`),
`NB_STATE_DIR`/`NB_DNS_STATE_FILE`, `NB_METRICS_PUSH_ENABLED=false` (явный запрет отправки метрик на
`ingest.netbird.io` — иначе management может её включить), на macOS `NB_ROUTE_PROTO_FLAG=2` (иначе служба при
старте сносит маршруты официального NetBird с флагом `RTF_PROTO1`), интерфейс/порт — приложение через `SetConfig` до `Login`
(`Login` эти поля игнорирует). GUID адаптера Windows — `-X …/client/iface/device.CustomWindowsGUIDString`
в `scripts/lib.sh`.

🪤 `-X` с опечаткой в пути линкер **молча** игнорирует — поэтому `test.sh` гоняет `TestBuildValues` с теми же
флагами, что и сборка, для обеих ОС.

## 5. Как править патч

```sh
sh daemon/scripts/prepare.sh                       # дерево daemon/upstream/netbird-<тег>
cd daemon/upstream/netbird-0.81.4
# правка → git commit (новый патч) или git commit --fixup <sha> && GIT_SEQUENCE_EDITOR=: git rebase -i --autosquash upstream
cd - && sh daemon/scripts/export-patches.sh        # коммиты поверх upstream → daemon/patches/
sh daemon/scripts/test.sh
```

Комментарии в патчах — по-русски, сообщения коммитов патчей — по-английски (их же можно предложить
апстриму). Врезки держать маленькими: новые файлы `jetbox_*.go` + несколько строк в апстримовых.

## 6. Новый тег апстрима

1. Не раньше недели от выхода тега (тот же порядок, что у iOS-клиента JetBox).
2. В `upstream.env`: `TAG`, `ZIP_URL`, `ZIP_SHA256` (`curl -fsSL … | shasum -a 256`), `JETBOX_N=1`.
3. Удалить старое дерево (`rm -rf daemon/upstream/netbird-<старый>`), `prepare.sh`; если патч не лёг — `git am`
   останавливается и дерево остаётся в его состоянии: поправить файлы, `git add`, `git am --continue`, затем
   `export-patches.sh`.
4. Проверить, не появилось ли в апстриме новых мест с именами NetBird в системе
   (`grep -rn '"[Nn]et[Bb]ird' client --include='*_windows.go' --include='*_darwin.go'`) и новых путей
   автообновления (`grep -rn 'installer\.\(New\|Run\)' client`).
5. `build.sh`, `test.sh`, живой прогон перед первым подключением к нашему management: в логе службы нет
   `download installer` / `new version available`.

## 7. Что проверяет `test.sh` и чего не проверяет

* Всё идёт во **временной копии** (`daemon/upstream/.test-tree`): патчи на чистый тег; рабочее дерево не трогается.
* `TMPDIR` — короткий (`/tmp/jbtest.*`): тесты апстрима создают unix-сокеты, путь к сокету на macOS ≤ 104 байт
  (иначе `bind: invalid argument` в `daemonaddr` — не наш дефект).
* Пакет `client/internal/dns` целиком **не гоняется**: его darwin-тесты пишут в `scutil` этого Mac.
* Smoke без root подтверждает версию, `NeedsLogin` и профиль в `NB_STATE_DIR`. **Автообновление smoke не
  проверяет**: менеджер обновлений стартует, только когда служба подключена к management. Его закрывают
  юнит-тесты патча 0001, а приёмка — живой прогон: после подключения к нашему management в логе службы
  (уровень info) есть `ignoring update directive`, нет `download installer` / `new version available`.
