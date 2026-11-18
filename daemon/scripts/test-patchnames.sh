#!/bin/sh
# Тест patchnames.sh: номера патчей по subject, пропуск номера держится, новый — следующий
# свободный, порядок нарушен — отказ. Временный git-репозиторий, сеть не нужна.
#   sh daemon/scripts/test-patchnames.sh
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
. "$HERE/patchnames.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
FAIL=0
ok() { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAIL=1; }
g() { git -C "$T/src" -c user.name=t -c user.email=t@t "$@" >/dev/null 2>&1; }
mkdir -p "$T/src" "$T/ref" "$T/out"
g init -q; echo 0 > "$T/src/f"; g add f; g commit -qm base; g tag upstream
commit() { echo "$1" >> "$T/src/f"; g commit -qam "$1"; }
commit "jetbox: one"; commit "jetbox: two"; commit "jetbox: three"
# Образец: в ветке когда-то было 0001, 0002 и 0004 (0003 — чужой патч, его здесь нет).
touch "$T/ref/0001-jetbox-one.patch" "$T/ref/0002-jetbox-two.patch" "$T/ref/0004-jetbox-three.patch"
jb_format_patches "$T/src" "$T/out" "$T/ref" && [ "$(ls "$T/out" | tr '\n' ' ')" = "0001-jetbox-one.patch 0002-jetbox-two.patch 0004-jetbox-three.patch " ] \
  && ok "пропуск номера держится (0001 0002 0004)" || bad "пропуск: $(ls "$T/out" | tr '\n' ' ')"

commit "jetbox: four"; rm -f "$T/out"/*
jb_format_patches "$T/src" "$T/out" "$T/ref" && [ "$(ls "$T/out" | tail -1)" = "0005-jetbox-four.patch" ] \
  && ok "новый патч — следующий свободный (0005)" || bad "новый: $(ls "$T/out" | tr '\n' ' ')"

rm -f "$T/out"/*; rm -f "$T/ref"/*
jb_format_patches "$T/src" "$T/out" "$T/ref" && [ "$(ls "$T/out" | tr '\n' ' ')" = "0001-jetbox-one.patch 0002-jetbox-two.patch 0003-jetbox-three.patch 0004-jetbox-four.patch " ] \
  && ok "без образца — подряд, как format-patch" || bad "без образца: $(ls "$T/out" | tr '\n' ' ')"

# Порядок нарушен: «three» в образце с номером меньше, чем «two».
rm -f "$T/out"/*; touch "$T/ref/0001-jetbox-one.patch" "$T/ref/0003-jetbox-two.patch" "$T/ref/0002-jetbox-three.patch"
if jb_format_patches "$T/src" "$T/out" "$T/ref" 2>/dev/null; then bad "нарушенный порядок не заметили"; else ok "номера не по порядку коммитов — отказ"; fi

[ "$FAIL" = 0 ] && printf '\nВСЁ ЗЕЛЁНОЕ\n' || { printf '\nЕСТЬ ПАДЕНИЯ\n'; exit 1; }
