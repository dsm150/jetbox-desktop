#!/bin/sh
# Проверка собранного установщика: состав, launchd (служба и автозапуск), служба (universal,
# подпись, runtime), скрипты, дистрибутив, подписи и нотаризация.
# Запуск: sh packaging/macos/test.sh [путь к .pkg]   (по умолчанию build-out/macos/JetBox.pkg)
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
PKG=${1:-$REPO/build-out/macos/JetBox.pkg}
FAIL=0
ok() { echo "  ✓ $*"; }
bad() { echo "  ✗ $*"; FAIL=1; }
check() { if eval "$2" >/dev/null 2>&1; then ok "$1"; else bad "$1"; fi; }

[ -f "$PKG" ] || { echo "нет $PKG"; exit 1; }
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
pkgutil --expand-full "$PKG" "$T/x" >/dev/null
C=$(find "$T/x" -maxdepth 1 -name '*.pkg' -type d | head -1)
P=$C/Payload
H="$P/Library/Application Support/JetBox"
D="$H/jetboxd"
APP=$P/Applications/JetBox.app
PL=$P/Library/LaunchDaemons/net.jet-net.jetbox.daemon.plist
AG=$P/Library/LaunchAgents/net.jet-net.jetbox.desktop.plist
HASAPP=0; [ -d "$APP" ] && HASAPP=1

echo "== состав ($( [ $HASAPP = 1 ] && echo 'служба + приложение' || echo 'только служба'))"
check "служба в /Library/Application Support/JetBox" "[ -x '$D' ]"
check "uninstall.sh рядом со службой" "[ -f '$H/uninstall.sh' ]"
check "launchd-plist службы" "[ -f '$PL' ]"
check "plist службы совпадает с packaging/macos" "cmp -s '$PL' '$HERE/net.jet-net.jetbox.daemon.plist'"
check "в приложении нет службы" "[ ! -e '$APP/Contents/MacOS/jetboxd' ]"
ALLOWED='^\./(Applications|Applications/JetBox\.app|Library|Library/LaunchDaemons|Library/LaunchAgents|Library/Application Support|Library/Application Support/JetBox)$'
check "ничего лишнего в корне payload" "[ -z \"\$(cd '$P' && find . -mindepth 1 -maxdepth 3 ! -path './Applications/JetBox.app/*' ! -path './Library/LaunchDaemons/*' ! -path './Library/LaunchAgents/*' ! -path './Library/Application Support/JetBox/*' | grep -v -E '$ALLOWED')\" ]"
if [ $HASAPP = 1 ]; then
  check "bundle ID net.jet-net.jetbox.desktop" "[ \"\$(defaults read '$APP/Contents/Info' CFBundleIdentifier)\" = net.jet-net.jetbox.desktop ]"
  check "LaunchAgent автозапуска" "[ -f '$AG' ]"
else
  check "без приложения нет и LaunchAgent" "[ ! -e '$AG' ]"
fi

echo "== launchd: служба"
pv() { /usr/libexec/PlistBuddy -c "Print $1" "$PL" 2>/dev/null; }
check "label net.jet-net.jetbox.daemon" "[ \"\$(pv :Label)\" = net.jet-net.jetbox.daemon ]"
check "служба из /Library/Application Support/JetBox" "[ \"\$(pv :ProgramArguments:0)\" = '/Library/Application Support/JetBox/jetboxd' ]"
check "канал unix:///var/run/jetbox.sock" "pv :ProgramArguments | grep -qx '    unix:///var/run/jetbox.sock'"
check "NB_STATE_DIR=/var/lib/jetbox" "[ \"\$(pv :EnvironmentVariables:NB_STATE_DIR)\" = /var/lib/jetbox ]"
check "NB_ROUTE_PROTO_FLAG=2" "[ \"\$(pv :EnvironmentVariables:NB_ROUTE_PROTO_FLAG)\" = 2 ]"
check "NB_METRICS_PUSH_ENABLED=false" "[ \"\$(pv :EnvironmentVariables:NB_METRICS_PUSH_ENABLED)\" = false ]"
check "лог службы ротируется по 5 МБ (NB_LOG_MAX_SIZE_MB=5)" "[ \"\$(pv :EnvironmentVariables:NB_LOG_MAX_SIZE_MB)\" = 5 ]"
check "нет NB_FORCE_RELAY" "! pv :EnvironmentVariables:NB_FORCE_RELAY"

if [ $HASAPP = 1 ]; then
  echo "== launchd: автозапуск приложения"
  av() { /usr/libexec/PlistBuddy -c "Print $1" "$AG" 2>/dev/null; }
  EXEC=$(defaults read "$APP/Contents/Info" CFBundleExecutable)
  check "запускает главный файл приложения" "[ \"\$(av :ProgramArguments:0)\" = '/Applications/JetBox.app/Contents/MacOS/$EXEC' ]"
  check "с --autostart" "[ \"\$(av :ProgramArguments:1)\" = --autostart ]"
  check "только в сеансе Aqua" "[ \"\$(av :LimitLoadToSessionType)\" = Aqua ]"
  check "без KeepAlive" "[ \"\$(av :KeepAlive)\" = false ]"
fi

echo "== служба"
check "universal (x86_64 arm64)" "lipo '$D' -verify_arch x86_64 arm64"
V=$("$D" version 2>/dev/null)
case "$V" in *+jetbox.*) ok "версия $V";; *) bad "версия «$V» — ждали …+jetbox.N";; esac

echo "== скрипты"
for s in preinstall postinstall; do
  check "$s есть и исполняемый" "[ -x '$C/Scripts/$s' ]"
  check "$s совпадает с packaging/macos" "cmp -s '$C/Scripts/$s' '$HERE/scripts/$s'"
done
check "launchd.sh рядом со скриптами" "cmp -s '$C/Scripts/launchd.sh' '$HERE/scripts/launchd.sh'"
if sh "$HERE/testdata/test-uninstall.sh" >"$T/uninstall-test.log" 2>&1; then
  ok "удаление: всё наше и данные пользователей, чужое на месте, --keep-state, --user (песочница)"
else
  bad "удаление — testdata/test-uninstall.sh:"; sed 's/^/      /' "$T/uninstall-test.log"
fi
if sh "$HERE/testdata/test-launchd.sh" >"$T/launchd-test.log" 2>&1; then
  ok "загрузка служб: поверх работающей, зависшая, чистая (поддельный launchctl)"
else
  bad "загрузка служб — testdata/test-launchd.sh:"; sed 's/^/      /' "$T/launchd-test.log"
fi

echo "== дистрибутив"
check "hostArchitectures arm64,x86_64" "grep -q 'hostArchitectures=\"arm64,x86_64\"' '$T/x/Distribution'"
MINOS=$(grep -o 'os-version min="[0-9.]*"' "$T/x/Distribution" | grep -o '[0-9.]*')
check "минимальная macOS $MINOS ≥ 13.0 (рантайм Go 1.27)" "[ \"\$(printf '%s\n13.0\n' '$MINOS' | sort -t. -k1,1n -k2,2n | head -1)\" = 13.0 ]"
if [ $HASAPP = 1 ]; then
  AMIN=$(defaults read "$APP/Contents/Info" LSMinimumSystemVersion 2>/dev/null || echo 0)
  check "установщик не пускает ниже, чем требует приложение ($AMIN)" "[ \"\$(printf '%s\n%s\n' '$MINOS' '$AMIN' | sort -t. -k1,1n -k2,2n | tail -1)\" = '$MINOS' ]"
fi
if [ $HASAPP = 1 ]; then
  check "бандл не переезжает (relocatable=false, <relocate/> пуст)" "grep -q 'relocatable=\"false\"' '$C/PackageInfo' && grep -q '<relocate/>' '$C/PackageInfo'"
  check "бандл ставится без проверки версии (нет <bundle-version> с приложением)" "! sed -n '/<bundle-version>/,/<\/bundle-version>/p' '$C/PackageInfo' | grep -q 'net.jet-net.jetbox.desktop'"
fi

echo "== подпись"
if pkgutil --check-signature "$PKG" | grep -q "Developer ID Installer"; then
  ok "пакет подписан Developer ID Installer"
  check "служба: подпись валидна" "codesign --verify --strict '$D'"
  check "служба: hardened runtime" "codesign -dv '$D' 2>&1 | grep -q 'flags=.*runtime'"
  check "служба: идентификатор net.jet-net.jetbox.daemon" "codesign -dv '$D' 2>&1 | grep -q 'Identifier=net.jet-net.jetbox.daemon\$'"
  if [ $HASAPP = 1 ]; then
    check "приложение: подпись валидна (strict, deep)" "codesign --verify --strict --deep '$APP'"
    check "приложение: hardened runtime" "codesign -dv '$APP' 2>&1 | grep -q 'flags=.*runtime'"
  fi
  check "Gatekeeper: установщик принят" "spctl -a -t install '$PKG'"
  check "нотаризация пришита (stapler)" "xcrun stapler validate '$PKG'"
else
  echo "  — пакет без подписи (--unsigned): проверки подписи пропущены"
fi

[ "$FAIL" = 0 ] && printf '\nВСЁ ЗЕЛЁНОЕ\n' || { printf '\nЕСТЬ ПАДЕНИЯ\n'; exit 1; }
