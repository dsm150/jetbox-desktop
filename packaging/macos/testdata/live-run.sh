#!/bin/sh
# Живой прогон службы JetBox (без приложения) на Mac против тестовой коробки — без sudo.
# Повторяет «одну кнопку» CLI службы: SetConfig (utun120/51830) → Login ключом → Up → выход.
# Вход: $JETBOX_LIVE_DIR/demo-link.local (600; каталог по умолчанию ~/jetbox-live) — ссылка подключения
# целиком; ключ уходит службе файлом (--setup-key-file) и сразу удаляется, в вывод не попадает.
# JETBOX_LIVE_CHECK_URL — необязательный адрес в сети коробки для проверки «до дома» по HTTP.
# Запуск: sh live-run.sh            подключиться и снять показатели
#         sh live-run.sh logout     снять компьютер с учёта (пир удаляется на management)
set -u
DIR=${JETBOX_LIVE_DIR:-$HOME/jetbox-live}
NB="/Library/Application Support/JetBox/jetboxd"
nb() { "$NB" --daemon-addr unix:///var/run/jetbox.sock "$@"; }

if [ "${1:-}" = logout ]; then nb deregister; nb status | head -1; exit 0; fi

echo "== служба: $("$NB" version), канал $(ls -l /var/run/jetbox.sock | awk '{print $1, $3":"$4}')"
LINK=$DIR/demo-link.local
[ -f "$LINK" ] || { echo "нет $LINK"; exit 1; }
KEYF=$(mktemp "$DIR/.k.XXXXXX"); chmod 600 "$KEYF"
eval "$(/usr/bin/python3 - "$LINK" "$KEYF" <<'PY'
import sys, shlex, urllib.parse
link = open(sys.argv[1]).read().strip()
frag = link.split('#', 1)[1] if '#' in link else ''
p = {}
for pair in frag.split('&'):
    if '=' in pair:
        k, v = pair.split('=', 1)
        p[k.lower()] = urllib.parse.unquote(v).strip()
open(sys.argv[2], 'w').write(p.get('k', ''))
print('M=%s E=%s KLEN=%d' % (shlex.quote(p.get('m', '')), shlex.quote(p.get('e', '')), len(p.get('k', ''))))
PY
)"
echo "ссылка: m=$M e=$E длина ключа=$KLEN (значение не выводится)"

T0=$(date +%s)
nb up --setup-key-file "$KEYF" --management-url "$M" --hostname JetBox-Mac \
  --interface-name utun120 --wireguard-port 51830 2>&1 | tail -2
rm -f "$KEYF"
echo "== up за $(( $(date +%s) - T0 )) с"
sleep 15

echo "== выход"
nb networks ls 2>&1 | grep -A3 "ID: $E" || true
nb networks ls 2>&1 | grep -A3 "ID: $E" | grep -q Selected || nb networks select "$E"

echo "== статус"
nb status 2>&1 | grep -E "Management|Signal|Relays|Nameservers|NetBird IP|Interface type|Wireguard port|Peers count"
nb status -d 2>&1 | sed -n '/Peers detail/,/^Events/p' | grep -E "^ [^ ]|Status:|Connection type|ICE candidate \(|Latency|Transfer" | head -12
echo "== события службы (обновлений быть не должно)"
nb status -d 2>&1 | sed -n '/^Events/,$p' | grep -E "Message:" | sort | uniq -c | head -12

echo "== система"
ifconfig utun120 2>/dev/null | head -2 || echo "нет utun120"
netstat -rn -f inet | grep -E "^(0/1|128.0/1)"
echo "маршрутов с PROTO1 у чужих: $(netstat -rn -f inet | awk 'NR>3 && $3 ~ /1/' | wc -l | tr -d ' ')"
printf 'list State:/Network/Service/.*\n' | scutil | grep -E "JetBox-|NetBird-" || echo "DNS-ключей нет"
ls /etc/ssh/ssh_config.d/ 2>/dev/null

echo "== путь трафика"
route -n get 1.1.1.1 | grep interface
BOXIP=$(nb status -d 2>&1 | awk '/^ [^ ]/{h=$0} /NetBird IP:/{ip=$3} /Networks: .*0\.0\.0\.0\/0/{print ip; exit}')
[ -n "$BOXIP" ] && for i in 1 2 3; do dig @"$BOXIP" jet-net.net +time=2 +tries=1 | grep -E "Query time|timed out"; done
if [ -n "${JETBOX_LIVE_CHECK_URL:-}" ]; then
  curl -s -m 8 -o /dev/null -w "$JETBOX_LIVE_CHECK_URL → %{http_code} за %{time_total} с\n" "$JETBOX_LIVE_CHECK_URL"
fi
