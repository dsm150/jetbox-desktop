# Общее для preinstall/postinstall: надёжная выгрузка и загрузка launchd-заданий.
# 🪤 `launchctl bootout` асинхронный: сразу после него `bootstrap` того же задания падает
# «Bootstrap failed: 5: Input/output error» (так упало обновление 0.0.2 → 0.0.3 поверх работающей
# службы). Поэтому: выгрузить → дождаться, пока задание исчезнет → загрузить с повторами; если
# задание так и осталось загруженным — перезапустить его (kickstart -k).
# shellcheck shell=sh

# jb_loaded <домен/метка> — загружено ли задание.
jb_loaded() { launchctl print "$1" >/dev/null 2>&1; }

# jb_unload <домен/метка> — выгрузить и дождаться (до ~10 с).
jb_unload() {
  launchctl bootout "$1" 2>/dev/null || true
  n=0
  while jb_loaded "$1" && [ $n -lt 20 ]; do sleep "${JB_STEP:-0.5}"; n=$((n + 1)); done
  if jb_loaded "$1"; then echo "задание $1 не выгрузилось за 10 с"; return 1; fi
  return 0
}

# jb_load <домен> <метка> <plist> — загрузить; при сбое повторить; если задание уже загружено —
# перезапустить его. Код 0 — задание загружено и запущено.
jb_load() {
  dom=$1; lbl=$2; pl=$3
  launchctl enable "$dom/$lbl" 2>/dev/null || true
  jb_unload "$dom/$lbl" || true
  n=1
  while [ $n -le 5 ]; do
    if launchctl bootstrap "$dom" "$pl"; then echo "загружено $dom/$lbl (попытка $n)"; return 0; fi
    if jb_loaded "$dom/$lbl"; then
      launchctl kickstart -k "$dom/$lbl" && { echo "уже было загружено — перезапущено $dom/$lbl"; return 0; }
    fi
    sleep "${JB_RETRY:-1}"
    n=$((n + 1))
  done
  echo "не удалось загрузить $dom/$lbl"
  return 1
}
