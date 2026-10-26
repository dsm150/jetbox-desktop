package othervpn

import "strings"

// Типы интерфейсов Windows (IANA ifType, MIB_IF_ROW2.Type), под которыми живут туннели.
const (
	ifTypePPP         = 23  // встроенный VPN Windows (IKEv2/SSTP/L2TP/PPTP) и прочий RAS
	ifTypePropVirtual = 53  // wintun/TAP: WireGuard для Windows, NetBird, OpenVPN, AnyConnect
	ifTypeTunnel      = 131 // туннельные интерфейсы с инкапсуляцией
)

// routeIface — маршрут IPv4 из таблицы: на каком интерфейсе и с какой длиной префикса.
type routeIface struct {
	Alias string
	Bits  int
}

// bestIface — интерфейс, который Windows выбрала бы для пакета в интернет.
type bestIface struct {
	Alias string
	Type  uint32
}

// foreignTunnels — решение для Windows, без обращений к системе. Два признака:
//   - маршруты 0.0.0.0/1 и 128.0.0.0/1 — так «весь трафик в туннель» ставят NetBird
//     и OpenVPN с redirect-gateway def1, не трогая маршрут по умолчанию;
//   - лучший интерфейс до интернета — туннель: WireGuard для Windows, встроенный VPN
//     Windows, AnyConnect, OpenVPN без def1 ставят свой 0.0.0.0/0, а не пару /1.
//
// Свой интерфейс (без учёта регистра) не в счёт. Лучший интерфейс — первым, без повторов.
func foreignTunnels(own string, routes []routeIface, best *bestIface) []string {
	var out []string
	add := func(name string) {
		if name == "" || strings.EqualFold(name, own) {
			return
		}
		for _, seen := range out {
			if strings.EqualFold(seen, name) {
				return
			}
		}
		out = append(out, name)
	}
	if best != nil && isTunnelType(best.Type) {
		add(best.Alias)
	}
	for _, r := range routes {
		if r.Bits == 1 {
			add(r.Alias)
		}
	}
	return out
}

func isTunnelType(t uint32) bool {
	return t == ifTypePPP || t == ifTypePropVirtual || t == ifTypeTunnel
}
