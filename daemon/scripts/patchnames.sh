# Номера файлов патчей службы. Номер — часть договорённости между исполнителями (0005 — своё
# обновление, 0007 — установщик обновления Windows, 0008 — маска management…) и может идти с
# пропуском: патч, которого в этой ветке нет, свой номер держит. git format-patch нумерует подряд,
# поэтому номер берём из имени уже лежащего файла с тем же subject (имя без «NNNN-»), а новому
# патчу даём следующий свободный выше последнего. Номера обязаны расти в порядке коммитов:
# prepare.sh накладывает патчи по имени.
# shellcheck shell=sh

# jb_format_patches <дерево> <куда> <образец> — коммиты upstream..HEAD дерева в каталог <куда>
# с номерами по образцу (каталог с прежними *.patch; может быть пустым).
jb_format_patches() {
  jb_src=$1; jb_out=$2; jb_ref=$3
  jb_tmp=$(mktemp -d)
  # core.abbrev=7: иначе длина хешей в «index …» зависит от числа объектов в дереве (7 или 8 знаков),
  # и одинаковые коммиты выгружаются по-разному — prepare.sh видел бы «чужие» коммиты.
  git -c core.abbrev=7 -C "$jb_src" format-patch -q --no-signature --zero-commit --no-numbered -N -o "$jb_tmp" upstream..HEAD || { rm -rf "$jb_tmp"; return 1; }
  jb_max=0
  for jb_r in "$jb_ref"/*.patch; do
    [ -e "$jb_r" ] || continue
    jb_n=$(expr "$(basename "$jb_r" | cut -c1-4)" + 0)
    [ "$jb_n" -gt "$jb_max" ] && jb_max=$jb_n
  done
  jb_prev=0
  for jb_f in "$jb_tmp"/*.patch; do
    [ -e "$jb_f" ] || continue
    jb_slug=$(basename "$jb_f" | cut -c6-)
    jb_old=""
    for jb_r in "$jb_ref"/*.patch; do
      [ -e "$jb_r" ] || continue
      [ "$(basename "$jb_r" | cut -c6-)" = "$jb_slug" ] && jb_old=$(basename "$jb_r" | cut -c1-4)
    done
    if [ -n "$jb_old" ]; then
      jb_n=$(expr "$jb_old" + 0)
    else
      jb_max=$((jb_max + 1)); jb_n=$jb_max
    fi
    if [ "$jb_n" -le "$jb_prev" ]; then
      echo "патч «$jb_slug» получает номер $jb_n после $jb_prev — номера не по порядку коммитов;" >&2
      echo "  переставь коммиты в дереве или переименуй файлы в daemon/patches вручную" >&2
      rm -rf "$jb_tmp"; return 1
    fi
    jb_prev=$jb_n
    mv "$jb_f" "$jb_out/$(printf '%04d' "$jb_n")-$jb_slug"
  done
  rm -rf "$jb_tmp"
}
