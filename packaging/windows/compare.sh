#!/bin/sh
# Сверка двух сборок Windows (например, локальной и артефакта CI):
#   packaging/windows/compare.sh <каталог-a> <каталог-b>
# В каталогах — JetBox.exe, jetboxd.exe, JetBox-Setup.exe (и, если есть, *.buildinfo от сборочных скриптов).
# Печатает sha256 трёх файлов; для расхождения — подсказку, где оно: секции PE с разными байтами,
# `go version -m` у exe, различия *.buildinfo. Код выхода 0 — все три совпали.
set -eu
[ $# -eq 2 ] && [ -d "$1" ] && [ -d "$2" ] || { echo "packaging/windows/compare.sh <каталог-a> <каталог-b>" >&2; exit 2; }
A=$1
B=$2
sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }
PY=$(command -v python3 || command -v python || true)

# Байты, которыми различаются два PE-файла, — по секциям (заголовки, .text, .rdata, .rsrc…, overlay —
# хвост после секций: у установщика NSIS там сжатые файлы и скрипт).
pe_sections_diff() {
  "$PY" - "$1" "$2" <<'EOF'
import struct, sys
a, b = (open(p, "rb").read() for p in sys.argv[1:3])
def sections(d):
    try:
        pe = struct.unpack_from("<I", d, 0x3C)[0]
        if d[pe:pe + 4] != b"PE\0\0":
            return []
        n, opt = struct.unpack_from("<H", d, pe + 6)[0], struct.unpack_from("<H", d, pe + 20)[0]
        out, off = [], pe + 24 + opt
        for i in range(n):
            name = d[off + 40 * i:off + 40 * i + 8].rstrip(b"\0").decode("latin1")
            size, ptr = struct.unpack_from("<II", d, off + 40 * i + 16)
            if size:
                out.append((ptr, ptr + size, name))
        return sorted(out)
    except struct.error:
        return []
sa = sections(a)
def where(o):
    if sa and o < sa[0][0]:
        return "заголовки PE"
    for s, e, n in sa:
        if s <= o < e:
            return n
    return "overlay"
print("  размер: %d / %d байт" % (len(a), len(b)))
if [(e - s, n) for s, e, n in sa] != [(e - s, n) for s, e, n in sections(b)]:
    print("  таблица секций разная (размеры/состав) — разный код или данные, смещения дальше несравнимы")
diff, first, B = {}, None, 4096
for o in range(0, min(len(a), len(b)), B):
    x, y = a[o:o + B], b[o:o + B]
    if x != y:
        i = next(k for k in range(len(x)) if k >= len(y) or x[k] != y[k])
        first = o + i if first is None else first
        w = where(o + i)
        diff[w] = diff.get(w, 0) + 1
if first is None:
    print("  общая часть совпадает, отличается только длина")
else:
    print("  первое различие: 0x%x (%s)" % (first, where(first)))
    print("  блоков по 4 КБ с различиями: " + ", ".join("%s %d" % kv for kv in sorted(diff.items())))
EOF
}

hint() { # файл
  case "$1" in
    JetBox.exe | jetboxd.exe)
      echo "  подсказка: .text/.rdata — версия Go, флаги, исходники (frontend/dist для JetBox.exe);"
      echo "  .rsrc — ресурсы goversioninfo (версия, значок, манифест); заголовки — метка времени/контрольная сумма."
      if command -v go >/dev/null 2>&1; then
        go version -m "$A/$1" > "$TMPD/a" 2>&1 || true
        go version -m "$B/$1" > "$TMPD/b" 2>&1 || true
        sed -i.bak '1s/^[^:]*: //' "$TMPD/a" "$TMPD/b"
        if diff -u "$TMPD/a" "$TMPD/b" > "$TMPD/d"; then
          echo "  go version -m: одинаково (версия Go, зависимости, флаги)"
        else
          echo "  go version -m: различия"; sed -n '3,40p' "$TMPD/d" | sed 's/^/    /'
        fi
      fi
      ;;
    JetBox-Setup.exe)
      echo "  подсказка: overlay — сжатые данные NSIS: сначала сверить входы (JetBox.exe, jetboxd.exe, LICENSE/NOTICE"
      echo "  с CRLF?) — JetBox-Setup.exe.buildinfo; .rsrc/заголовки — версия makensis, VIProductVersion, значок."
      ;;
  esac
}

TMPD=$(mktemp -d)
trap 'rm -rf "$TMPD"' EXIT
rc=0
for f in JetBox.exe jetboxd.exe JetBox-Setup.exe; do
  if [ ! -f "$A/$f" ] || [ ! -f "$B/$f" ]; then
    echo "?  $f — нет в одном из каталогов"; rc=1; continue
  fi
  ha=$(sha256 "$A/$f"); hb=$(sha256 "$B/$f")
  if [ "$ha" = "$hb" ]; then
    echo "=  $f  $ha"
  else
    rc=1
    echo "≠  $f"
    echo "  a: $ha"
    echo "  b: $hb"
    if [ -n "$PY" ]; then pe_sections_diff "$A/$f" "$B/$f"; else echo "  (нет python — без разбора по секциям)"; fi
    hint "$f"
  fi
done

for i in "$A"/*.buildinfo; do
  [ -f "$i" ] || continue
  n=$(basename "$i")
  [ -f "$B/$n" ] || { echo "?  $n — только в $A"; continue; }
  tr -d '\r' < "$i" > "$TMPD/ia"; tr -d '\r' < "$B/$n" > "$TMPD/ib"
  if ! diff -u "$TMPD/ia" "$TMPD/ib" > "$TMPD/id"; then
    echo "≠  $n:"; sed -n '3,40p' "$TMPD/id" | sed 's/^/    /'
  fi
done
exit $rc
