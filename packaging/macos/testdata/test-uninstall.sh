#!/bin/sh
# Тест uninstall.sh в песочнице: свой корень (JETBOX_UNINSTALL_ROOT) и поддельные launchctl/scutil/pkill/…
# в PATH — без root, настоящую систему не трогает. Проверяет: всё наше удалено (служба, агент,
# приложение, каталоги, пользовательские данные), чужое на месте, снятие с учёта вызвано, --keep-state
# сохраняет регистрацию и не снимает с учёта, --user ограничивает чистку, плохое имя отвергается.
# Запуск: sh packaging/macos/testdata/test-uninstall.sh
set -u
HERE=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
FAIL=0
ok() { echo "  ✓ $*"; }
bad() { echo "  ✗ $*"; FAIL=1; }

mkdir -p "$T/bin"
for tool in launchctl pkill osascript sudo pkgutil dscl; do
  printf '#!/bin/sh\necho "%s $*" >> "%s/calls"\nexit 0\n' "$tool" "$T" > "$T/bin/$tool"
done
printf '#!/bin/sh\ncat >/dev/null\nexit 0\n' > "$T/bin/scutil"
# stat -f%Su <console> → «alice»; id -u alice → 501
printf '#!/bin/sh\necho alice\n' > "$T/bin/stat"
printf '#!/bin/sh\n[ "$1" = -u ] && { echo 501; exit 0; }\nexec /usr/bin/id "$@"\n' > "$T/bin/id"
chmod +x "$T/bin/"*

mkroot() { # $1 — корень
  r=$1
  mkdir -p "$r/Applications/JetBox.app/Contents/MacOS" "$r/Applications/Other.app" \
    "$r/Library/Application Support/JetBox" "$r/Library/Application Support/Other" \
    "$r/Library/LaunchDaemons" "$r/Library/LaunchAgents" "$r/var/lib/jetbox" "$r/var/lib/netbird" \
    "$r/var/log/jetbox" "$r/var/run" "$r/dev"
  : > "$r/dev/console"
  touch "$r/Library/LaunchDaemons/net.jet-net.jetbox.daemon.plist" "$r/Library/LaunchDaemons/netbird.plist" \
    "$r/Library/LaunchAgents/net.jet-net.jetbox.desktop.plist" "$r/var/lib/jetbox/default.json" \
    "$r/var/lib/netbird/default.json"
  printf '#!/bin/sh\necho "jetboxd $*" >> "%s/calls"\n' "$T" > "$r/Library/Application Support/JetBox/jetboxd"
  chmod +x "$r/Library/Application Support/JetBox/jetboxd"
  /usr/bin/python3 -c "import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])" "$r/var/run/jetbox.sock"
  for u in alice bob; do
    h="$r/Users/$u/Library"
    mkdir -p "$h/Application Support/JetBox" "$h/Application Support/Other" "$h/Caches/net.jet-net.jetbox.desktop" \
      "$h/WebKit/net.jet-net.jetbox.desktop" "$h/Saved Application State/net.jet-net.jetbox.desktop.savedState" "$h/Preferences"
    touch "$h/Application Support/JetBox/app.log" "$h/Preferences/net.jet-net.jetbox.desktop.plist" "$h/Preferences/other.plist"
  done
}
run() { # $1 — корень, далее аргументы
  r=$1; shift
  PATH="$T/bin:$PATH" JETBOX_UNINSTALL_ROOT="$r" sh "$HERE/uninstall.sh" "$@" >"$T/out" 2>&1
}
gone() { [ ! -e "$1" ] && ok "$2" || bad "$2 — осталось: $1"; }
kept() { [ -e "$1" ] && ok "$2" || bad "$2 — пропало: $1"; }

echo "== полное удаление"
R1=$T/r1; mkroot "$R1"; : > "$T/calls"
run "$R1"; rc=$?
[ $rc = 0 ] && ok "код 0: $(tail -1 "$T/out")" || bad "код $rc: $(cat "$T/out")"
gone "$R1/Applications/JetBox.app" "приложение"
gone "$R1/Library/Application Support/JetBox" "служба и uninstall.sh"
gone "$R1/Library/LaunchDaemons/net.jet-net.jetbox.daemon.plist" "plist службы"
gone "$R1/Library/LaunchAgents/net.jet-net.jetbox.desktop.plist" "LaunchAgent"
gone "$R1/var/lib/jetbox" "состояние службы"
gone "$R1/var/log/jetbox" "логи службы"
gone "$R1/var/run/jetbox.sock" "канал"
for u in alice bob; do
  gone "$R1/Users/$u/Library/Application Support/JetBox" "$u: данные приложения"
  gone "$R1/Users/$u/Library/WebKit/net.jet-net.jetbox.desktop" "$u: данные веб-вида"
  gone "$R1/Users/$u/Library/Preferences/net.jet-net.jetbox.desktop.plist" "$u: настройки"
  kept "$R1/Users/$u/Library/Preferences/other.plist" "$u: чужие настройки на месте"
  kept "$R1/Users/$u/Library/Application Support/Other" "$u: чужие данные на месте"
done
kept "$R1/Applications/Other.app" "чужое приложение на месте"
kept "$R1/Library/LaunchDaemons/netbird.plist" "служба официального NetBird на месте"
kept "$R1/var/lib/netbird/default.json" "конфиг официального NetBird на месте"
grep -q "jetboxd .*deregister" "$T/calls" && ok "снятие с учёта вызвано" || bad "deregister не вызван"
grep -q "launchctl bootout system/net.jet-net.jetbox.daemon" "$T/calls" && ok "служба выгружена" || bad "служба не выгружена"
grep -q "launchctl bootout gui/501/net.jet-net.jetbox.desktop" "$T/calls" && ok "автозапуск выгружен" || bad "автозапуск не выгружен"

echo "== --keep-state --user alice"
R2=$T/r2; mkroot "$R2"; : > "$T/calls"
run "$R2" --keep-state --user alice
kept "$R2/var/lib/jetbox/default.json" "регистрация сохранена"
grep -q deregister "$T/calls" && bad "снятие с учёта при --keep-state" || ok "с учёта не снимали"
gone "$R2/Users/alice/Library/Application Support/JetBox" "alice: данные убраны"
kept "$R2/Users/bob/Library/Application Support/JetBox" "bob: не тронут (--user alice)"

echo "== плохое имя пользователя"
R3=$T/r3; mkroot "$R3"
run "$R3" --user "../../etc"; rc=$?
[ $rc = 2 ] && ok "отвергнуто (код 2)" || bad "код $rc"
kept "$R3/Applications/JetBox.app" "ничего не удалено"

[ "$FAIL" = 0 ] && printf '\nВСЁ ЗЕЛЁНОЕ\n' || { printf '\nЕСТЬ ПАДЕНИЯ\n'; exit 1; }
