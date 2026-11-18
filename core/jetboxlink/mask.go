package jetboxlink

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ManagementSuffix — адрес management разрешён только на поддоменах этого домена (решение владельца
// 18.11.2026, план 1.0.6; правило одно на все платформы — LINK-CONTRACT §2.3, общие векторы
// testdata/mgmt-mask.json). Служба JetBox проверяет то же (brand.env AllowedManagementSuffix).
const ManagementSuffix = ".jet-net.net"

// AllowedManagement — m из ссылки (уже без percent-encoding) разрешён маской: https, без логина,
// пути (кроме «/»), query и фрагмента; хост — имя ASCII (не IP), оканчивается на ManagementSuffix
// (сам домен без поддомена — нет), порт — любой 1–65535.
func AllowedManagement(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return false
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return false
	}
	if u.Path != "" && u.Path != "/" {
		return false
	}
	if strings.HasSuffix(u.Host, ":") { // «host:» — двоеточие без порта
		return false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || net.ParseIP(host) != nil {
		return false
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" {
			return false
		}
	}
	return strings.HasSuffix(host, ManagementSuffix) && len(host) > len(ManagementSuffix)
}
