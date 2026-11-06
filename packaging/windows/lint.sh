#!/bin/sh
# Проверка NSIS-скриптов без makensis. В NSIS строку в одинарных кавычках нельзя «экранировать» удвоенной
# '' — makensis режет её на части, и плагин (nsExec) получает обрывок команды. Так снятие с учёта при
# удалении не запускалось вовсе (1.0.0–1.0.3). Ловим '' вне комментариев.
set -eu
DIR=$(cd "$(dirname "$0")" && pwd)
bad=$(grep -n "''" "$DIR/jetbox.nsi" "$DIR/lang.nsh" | grep -v -E "^[^:]+:[0-9]+:[[:space:]]*;" || true)
if [ -n "$bad" ]; then
  echo "NSIS: '' внутри строки в одинарных кавычках (используйте \$\\' или двойные кавычки):" >&2
  echo "$bad" >&2
  exit 1
fi
# Снятие с учёта при удалении — прямым вызовом службы, до её остановки.
grep -q "nsExec::ExecToLog /TIMEOUT=[0-9]* '\"\$INSTDIR\\\\jetboxd.exe\" deregister --daemon-addr npipe://jetbox'" "$DIR/jetbox.nsi" || {
  echo "NSIS: нет прямого вызова jetboxd.exe deregister в секции удаления" >&2; exit 1; }
# Машинный «managed lock» официального NetBird (NB_DISABLE_UPDATE_SETTINGS=true) не должен попадать в службу JetBox:
# ни при `service install`, ни в окружение работающей службы — иначе приложение не может подключиться (02.11.2026).
grep -q -- "--service-env NB_DISABLE_UPDATE_SETTINGS=false" "$DIR/jetbox.nsi" || {
  echo "NSIS: в DAEMON_FLAGS нет --service-env NB_DISABLE_UPDATE_SETTINGS=false" >&2; exit 1; }
awk '/SetEnvironmentVariable\(t "NB_DISABLE_UPDATE_SETTINGS", t "false"\)/ { set = 1 }
     /jetboxd.exe\" service install/ { if (!set) bad = 1; seen = 1 }
     END { exit (bad || !seen) }' "$DIR/jetbox.nsi" || {
  echo "NSIS: перед service install нет SetEnvironmentVariable NB_DISABLE_UPDATE_SETTINGS=false" >&2; exit 1; }
# Автозапуск — у каждого пользователя свой (HKCU\…\Run, «Настройки» приложения): HKLM\…\Run (≤1.0.3) не пишем,
# при обновлении переносим текущему пользователю, при удалении убираем оба (06.11.2026).
if grep -n 'WriteRegStr HKLM "${RUN_KEY}"' "$DIR/jetbox.nsi" >&2; then
  echo "NSIS: автозапуск в HKLM\\…\\Run — для всех пользователей; нужен HKCU" >&2; exit 1
fi
grep -q "WriteRegStr HKCU \"\${RUN_KEY}\" \"JetBox\" '\"\$INSTDIR\\\\JetBox.exe\" --autostart'" "$DIR/jetbox.nsi" || {
  echo "NSIS: нет записи HKCU\\…\\Run\\JetBox = \"\$INSTDIR\\JetBox.exe\" --autostart" >&2; exit 1; }
grep -q 'DeleteRegValue HKCU "${RUN_KEY}" "JetBox"' "$DIR/jetbox.nsi" || {
  echo "NSIS: удаление не убирает HKCU\\…\\Run\\JetBox" >&2; exit 1; }
# Переход с ≤1.0.3 при выключенной защите: состояние прежней службы снимается до её остановки, и новая служба
# опускается после запуска — иначе она подключилась бы сама (флаг автоподключения 1.0.3 не ставил).
awk '/jetboxd.exe\" status --json/ { st = NR } /^Section "JetBox"/ { sec = 1 } sec && /Call OldDaemonStatus/ { call = NR }
     sec && /insertmacro StopOld/ && !stop { stop = NR } /jetboxd.exe\" service start/ { start = NR }
     /jetboxd.exe\" down --daemon-addr npipe:\/\/jetbox/ { down = NR }
     END { exit !(st && call && stop && call < stop && start && down > start) }' "$DIR/jetbox.nsi" || {
  echo "NSIS: нет status --json до StopOld или down после service start (переход с 1.0.3)" >&2; exit 1; }
# Active Setup: автозапуск остальным пользователям Windows и при тихой установке — приложение с --autostart-init
# при их первом входе. Ключ пишется при установке и удаляется при удалении.
grep -q 'WriteRegStr HKLM "${ACTIVE_SETUP_KEY}" "StubPath" .*--autostart-init' "$DIR/jetbox.nsi" &&
  grep -q 'WriteRegDWORD HKLM "${ACTIVE_SETUP_KEY}" "IsInstalled" 1' "$DIR/jetbox.nsi" &&
  grep -q 'DeleteRegKey HKLM "${ACTIVE_SETUP_KEY}"' "$DIR/jetbox.nsi" &&
  grep -q 'ACTIVE_SETUP_KEY "Software\\Microsoft\\Active Setup\\Installed Components\\JetBox"' "$DIR/jetbox.nsi" || {
  echo "NSIS: ключ Active Setup JetBox (StubPath --autostart-init, IsInstalled) не пишется или не удаляется" >&2; exit 1; }
echo "nsis lint: ok"
