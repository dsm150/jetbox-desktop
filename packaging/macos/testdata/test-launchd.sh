#!/bin/sh
# Тест scripts/launchd.sh на поддельном launchctl (без root и без настоящего launchd).
# Сценарии — как при установке поверх работающей службы:
#   1) bootout асинхронный: задание «висит» ещё 3 опроса после bootout, bootstrap в это время
#      падает «5: Input/output error» (ровно так упало обновление 0.0.2 → 0.0.3);
#   2) задание не выгружается вовсе — после неудачных bootstrap срабатывает kickstart -k;
#   3) чистая установка (задания не было) — bootstrap с первой попытки.
# Запуск: sh packaging/macos/testdata/test-launchd.sh
set -u
HERE=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
FAIL=0
mkdir -p "$T/bin"
# Поддельный launchctl: состояние в файлах $T/state.*
cat > "$T/bin/launchctl" <<'FAKE'
#!/bin/sh
S=$FAKE_STATE
echo "$*" >> "$S.calls"
case "$1" in
  print)
    [ -f "$S.loaded" ] || exit 113
    if [ -f "$S.linger" ]; then
      n=$(cat "$S.linger"); n=$((n - 1))
      if [ "$n" -le 0 ]; then rm -f "$S.linger" "$S.loaded"; else echo "$n" > "$S.linger"; fi
    fi
    exit 0 ;;
  bootout)
    [ -f "$S.loaded" ] || exit 3
    if [ -f "$S.sticky" ]; then exit 0; fi
    echo "${FAKE_LINGER:-0}" > "$S.linger"
    [ "${FAKE_LINGER:-0}" = 0 ] && rm -f "$S.linger" "$S.loaded"
    exit 0 ;;
  bootstrap)
    if [ -f "$S.loaded" ]; then echo "Bootstrap failed: 5: Input/output error" >&2; exit 5; fi
    touch "$S.loaded"; exit 0 ;;
  kickstart) [ -f "$S.loaded" ] && { echo kicked >> "$S.calls"; exit 0; }; exit 113 ;;
  enable) exit 0 ;;
esac
exit 0
FAKE
chmod +x "$T/bin/launchctl"

run_case() { # имя, подготовка состояния, ожидаемое: ok|fail, ожидаемая строка в выводе
  name=$1; prep=$2; want=$3; grepfor=$4
  rm -f "$T"/state.*; export FAKE_STATE="$T/state"
  eval "$prep"
  out=$(PATH="$T/bin:$PATH" JB_STEP=0 JB_RETRY=0 sh -c ". '$HERE/scripts/launchd.sh'; jb_load system net.jet-net.jetbox.daemon /x.plist" 2>&1)
  rc=$?
  got=fail; [ $rc = 0 ] && got=ok
  if [ "$got" = "$want" ] && printf '%s' "$out" | grep -q "$grepfor"; then
    echo "  ✓ $name — $(printf '%s' "$out" | tail -1)"
  else
    echo "  ✗ $name — rc=$rc, вывод: $out"; FAIL=1
  fi
}

echo "== scripts/launchd.sh на поддельном launchctl"
run_case "поверх работающей службы, bootout асинхронный (3 опроса)" \
  'touch "$T/state.loaded"; export FAKE_LINGER=3' ok "загружено system/net.jet-net.jetbox.daemon (попытка 1)"
run_case "задание не выгружается — kickstart -k" \
  'touch "$T/state.loaded" "$T/state.sticky"; export FAKE_LINGER=0' ok "уже было загружено — перезапущено"
run_case "чистая установка" \
  'export FAKE_LINGER=0' ok "загружено system/net.jet-net.jetbox.daemon (попытка 1)"

# Регрессия: старый порядок (bootout → сразу bootstrap) на асинхронном bootout должен падать —
# значит, тест действительно воспроизводит сбой 0.0.2 → 0.0.3.
rm -f "$T"/state.*; export FAKE_STATE="$T/state" FAKE_LINGER=3; touch "$T/state.loaded"
if PATH="$T/bin:$PATH" sh -c 'launchctl bootout system/x; launchctl bootstrap system /x.plist' >/dev/null 2>&1; then
  echo "  ✗ поддельный launchctl не воспроизводит сбой старого порядка"; FAIL=1
else
  echo "  ✓ старый порядок bootout → bootstrap воспроизводит «5: Input/output error»"
fi

[ "$FAIL" = 0 ] && printf '\nВСЁ ЗЕЛЁНОЕ\n' || { printf '\nЕСТЬ ПАДЕНИЯ\n'; exit 1; }
