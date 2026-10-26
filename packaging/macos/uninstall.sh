#!/bin/sh
# Полностью удаляет JetBox с Mac. Запуск: sudo sh "/Library/Application Support/JetBox/uninstall.sh"
# Его же вызывает пункт «Удалить JetBox…» в приложении — через стандартный запрос пароля администратора
# macOS (osascript … with administrator privileges), с --user <кто нажал>.
#
#   --keep-state    оставить регистрацию компьютера (/var/lib/jetbox) и не снимать его с учёта — для
#                   переустановки поверх
#   --user <имя>    чьи пользовательские данные убрать (можно повторять); по умолчанию — все локальные
#                   пользователи, у кого есть данные JetBox
#
# Что удаляется: службу и автозапуск (launchd), приложение, службу и этот скрипт
# (/Library/Application Support/JetBox), каталоги службы, свои ключи DNS (scutil JetBox-*), квитанцию пакета;
# у пользователей — ~/Library/Application Support/JetBox (настройки, журнал), кэши, данные веб-вида, настройки,
# сохранённое состояние окна. Без --keep-state компьютер снимается с учёта в сети JetBox (deregister).
#
# JETBOX_UNINSTALL_ROOT — корень для тестов (testdata/test-uninstall.sh): все пути — от него, root не нужен.
set -u
R=${JETBOX_UNINSTALL_ROOT:-}
[ -n "$R" ] || [ "$(id -u)" = 0 ] || { echo "нужен sudo: sudo sh $0"; exit 1; }
LABEL=net.jet-net.jetbox.daemon
AGENT=net.jet-net.jetbox.desktop
BUNDLE_ID=net.jet-net.jetbox.desktop
APP=$R/Applications/JetBox.app
HELPER="$R/Library/Application Support/JetBox"
JETBOXD="$HELPER/jetboxd"

KEEP=0; USERS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --keep-state) KEEP=1 ;;
    --user)
      # Только «простые» имена учётных записей macOS — значение уходит в пути.
      case "${2:-}" in ""|*[!A-Za-z0-9._-]*|.*) echo "плохое имя пользователя: ${2:-}"; exit 2 ;; esac
      USERS="$USERS $2"; shift ;;
    *) echo "неизвестный аргумент $1"; exit 2 ;;
  esac
  shift
done

# 1. Снять с учёта (пока служба жива): компьютер исчезает из сети JetBox. Без сети — не страшно,
#    запись удалит владелец коробки; ждём не дольше 15 с.
if [ "$KEEP" = 0 ] && [ -x "$JETBOXD" ] && [ -S "$R/var/run/jetbox.sock" ]; then
  "$JETBOXD" --daemon-addr "unix://$R/var/run/jetbox.sock" deregister >/dev/null 2>&1 &
  dp=$!; n=0
  while kill -0 "$dp" 2>/dev/null && [ $n -lt 15 ]; do sleep 1; n=$((n + 1)); done
  kill "$dp" 2>/dev/null
fi

# 2. Остановить автозапуск и приложение у вошедшего пользователя.
console=$(stat -f%Su "$R/dev/console" 2>/dev/null || true)
case "$console" in
  ""|root|loginwindow|_mbsetupuser) ;;
  *)
    uid=$(id -u "$console" 2>/dev/null)
    if [ -n "$uid" ]; then
      launchctl bootout "gui/$uid/$AGENT" 2>/dev/null
      launchctl asuser "$uid" sudo -u "$console" osascript -e "quit app id \"$BUNDLE_ID\"" 2>/dev/null || true
    fi ;;
esac
pkill -x JetBox 2>/dev/null

# 3. Служба: при остановке сама снимает туннель, маршруты и свои ключи DNS (JetBox-*).
launchctl bootout "system/$LABEL" 2>/dev/null
sleep 1
rm -f "$R/Library/LaunchDaemons/$LABEL.plist" "$R/Library/LaunchAgents/$AGENT.plist" "$R/var/run/jetbox.sock"

# Страховка на случай аварийной остановки: только свои ключи scutil.
for key in $(printf 'list State:/Network/Service/JetBox-.*\n' | scutil 2>/dev/null | awk '/subKey/ {print $4}'); do
  printf 'remove %s\n' "$key" | scutil
done

# 4. Файлы системы.
rm -rf "$APP" "$HELPER" "$R/var/log/jetbox"
[ "$KEEP" = 1 ] || rm -rf "$R/var/lib/jetbox"
[ -n "$R" ] || pkgutil --forget "$BUNDLE_ID" >/dev/null 2>&1

# 5. Данные пользователей. Домашние каталоги — из каталога учётных записей (dscl), не из /Users/*.
home_of() {
  if [ -n "$R" ]; then echo "$R/Users/$1"; else
    dscl . -read "/Users/$1" NFSHomeDirectory 2>/dev/null | awk '{print $2}'
  fi
}
if [ -z "$USERS" ]; then
  if [ -n "$R" ]; then USERS=$(ls "$R/Users" 2>/dev/null); else
    USERS=$(dscl . -list /Users UniqueID 2>/dev/null | awk '$2 >= 500 {print $1}')
  fi
fi
for u in $USERS; do
  h=$(home_of "$u")
  # Системные учётки (дом в /var/…, /) пропускаем; сверяем путь без корня песочницы.
  case "${h#"$R"}" in ""|/|/var/*|/private/var/*) continue ;; esac
  [ -d "$h/Library" ] || continue
  rm -rf "$h/Library/Application Support/JetBox" \
         "$h/Library/Caches/$BUNDLE_ID" \
         "$h/Library/WebKit/$BUNDLE_ID" \
         "$h/Library/HTTPStorages/$BUNDLE_ID" \
         "$h/Library/HTTPStorages/$BUNDLE_ID.binarycookies" \
         "$h/Library/Saved Application State/$BUNDLE_ID.savedState" \
         "$h/Library/Preferences/$BUNDLE_ID.plist"
done

if [ "$KEEP" = 1 ]; then
  echo "JetBox удалён (регистрация сохранена в /var/lib/jetbox)."
else
  echo "JetBox удалён."
fi
