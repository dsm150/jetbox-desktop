#!/bin/sh
# Тесты службы JetBox. Рабочее дерево (daemon/upstream/netbird-<тег>) не трогает: патчи
# накладываются на чистый тег во временную копию, все тесты идут в ней.
#   1) патчи ложатся на чистый тег;
#   2) тесты патчей и затронутых пакетов (значения по умолчанию = апстрим);
#   3) флаги -X из brand.env и GUID адаптера доходят до пакетов (обе ОС);
#   4) vet под Windows;
#   5) smoke собранной службы без root: версия, NeedsLogin, ничего не пишет в систему.
# Автообновление проверяют юнит-тесты (2): в smoke менеджер обновлений не стартует —
# он запускается, только когда служба подключена к management (см. JETBOX-BUILD.md §7).
# Запуск: sh daemon/scripts/test.sh
set -eu
. "$(dirname "$0")/lib.sh"
FAIL=0
LOG=$(mktemp -d /tmp/jbtest.XXXXXX)
# Короткий TMPDIR: тесты апстрима создают unix-сокеты, а путь к сокету на macOS ≤ 104 байт.
export TMPDIR=$LOG
trap 'rm -rf "$LOG" "$DAEMON/upstream/.test-tree" "$DAEMON/upstream/.test-tree.patches.sha256"' EXIT
step() { printf '\n== %s\n' "$*"; }
bad() { echo "  ✗ $*"; FAIL=1; }
# run <имя> <команда…> — вывод в лог, при падении — хвост лога и FAIL=1.
run() {
  n=$1; shift
  if "$@" >"$LOG/$n.log" 2>&1; then
    grep -E '^(ok|--- (PASS|SKIP|FAIL)|PASS|FAIL)' "$LOG/$n.log" | sed 's/^/  /' || true
  else
    tail -25 "$LOG/$n.log" | sed 's/^/  /'
    bad "$n"
  fi
}

step "1. патчи на чистый тег ${TAG}"
T=$DAEMON/upstream/.test-tree
if DEST=$T sh "$DAEMON/scripts/prepare.sh" --force >"$LOG/prepare.log" 2>&1; then
  sed 's/^/  /' "$LOG/prepare.log"
else
  sed 's/^/  /' "$LOG/prepare.log"; bad "патчи не легли"; exit 1
fi
cd "$T"

step "2. тесты патчей и затронутых пакетов"
# Пакет dns целиком не гоняем: его darwin-тесты пишут в scutil (DNS этого мака).
run unit go test -count=1 ./client/internal/updater/... ./client/system/ ./client/jetboxbrand/ \
  ./client/internal/profilemanager/ ./client/internal/portforward/ ./client/mdm/ \
  ./client/internal/daemonaddr/ ./client/ssh/config/ ./client/iface/device/ ./client/server/
run dns-jetbox go test -count=1 -run Jetbox -v ./client/internal/dns/

for os in darwin windows; do
  step "3. флаги -X сборки $os доходят до пакетов"
  if [ "$os" = windows ]; then G=$WINDOWS_GUID; else G=""; fi
  run "x-$os" env JETBOX_BRAND_EXPECT="$(brand_lines "$os")" JETBOX_EXPECT_GUID="$G" \
    go test -count=1 -ldflags "$(ldflags "$os")" -run 'BuildValues|Jetbox' -v \
    ./client/jetboxbrand/ ./client/internal/dns/ ./client/iface/device/ ./client/internal/profilemanager/
  grep -q -- '--- PASS: TestBuildValues' "$LOG/x-$os.log" || bad "TestBuildValues не прошёл для $os"
  if [ "$os" = windows ]; then
    grep -q -- '--- PASS: TestJetboxWindowsGUIDFlag' "$LOG/x-$os.log" || bad "GUID адаптера не дошёл"
  fi
done

step "4. vet под Windows (затронутые пакеты)"
run vet-windows env GOOS=windows go vet ./client/internal/dns/ ./client/firewall/uspfilter/ ./client/server/ \
  ./client/internal/updater/... ./client/system/ ./client/cmd/ ./client/configs/ ./client/mdm/ \
  ./client/ssh/... ./client/internal/debug/
[ "$FAIL" = 0 ] && echo "  vet чистый"

BIN=$OUT/darwin-$(go env GOARCH)/jetboxd
if [ -x "$BIN" ]; then
  step "5. smoke: $BIN без root"
  V=$("$BIN" version)
  [ "$V" = "$VERSION" ] && echo "  версия $V" || bad "версия $V, ждали $VERSION"
  S=$LOG/smoke
  mkdir -p "$S"
  # Без root служба не трогает ни маршруты, ни DNS, ни интерфейсы; канал — tcp на localhost.
  NB_STATE_DIR=$S NB_DNS_STATE_FILE=$S/state.json NB_ROUTE_PROTO_FLAG=2 NB_METRICS_PUSH_ENABLED=false \
    "$BIN" service run --daemon-addr tcp://127.0.0.1:41799 --log-file "$S/client.log" --log-level debug \
    >"$S/out.log" 2>&1 &
  PID=$!
  sleep 4
  ST=$("$BIN" --daemon-addr tcp://127.0.0.1:41799 status 2>&1 | head -1 || true)
  kill "$PID" 2>/dev/null; wait "$PID" 2>/dev/null || true
  case "$ST" in *NeedsLogin*) echo "  $ST";; *) bad "статус: $ST"; tail -5 "$S/client.log";; esac
  [ -f "$S/default.json" ] && echo "  профиль создан в NB_STATE_DIR" || bad "нет профиля в NB_STATE_DIR"
  grep -q 'netbird.jet-net.net' "$S/default.json" && echo "  management по умолчанию — наш" || bad "management по умолчанию не netbird.jet-net.net"
  # Второй старт с уже существующим профилем: без входа служба не подключается (патч 0004).
  NB_STATE_DIR=$S NB_DNS_STATE_FILE=$S/state.json NB_ROUTE_PROTO_FLAG=2 NB_METRICS_PUSH_ENABLED=false \
    "$BIN" service run --daemon-addr tcp://127.0.0.1:41799 --log-file "$S/client2.log" --log-level debug \
    >"$S/out2.log" 2>&1 &
  PID=$!
  sleep 4
  ST=$("$BIN" --daemon-addr tcp://127.0.0.1:41799 status 2>&1 | head -1 || true)
  kill "$PID" 2>/dev/null; wait "$PID" 2>/dev/null || true
  case "$ST" in *NeedsLogin*) echo "  перезапуск без входа: $ST";; *) bad "перезапуск без входа: $ST";; esac
  grep -q "has not logged in yet" "$S/client2.log" && echo "  автоподключение до первого входа выключено" || bad "служба пыталась подключиться без входа"
else
  step "5. smoke пропущен: нет $BIN (сначала build.sh)"
fi

[ "$FAIL" = 0 ] && printf '\nВСЁ ЗЕЛЁНОЕ\n' || { printf '\nЕСТЬ ПАДЕНИЯ\n'; exit 1; }
