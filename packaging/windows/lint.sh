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
echo "nsis lint: ok"
