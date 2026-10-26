package othervpn

import (
	"reflect"
	"testing"
)

func TestForeignTunnels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		routes []routeIface
		best   *bestIface
		want   []string
	}{
		{"наши /1 не в счёт", []routeIface{{"JetBox", 1}, {"JetBox", 1}}, nil, nil},
		{"своё имя без учёта регистра", []routeIface{{"jetbox", 1}}, &bestIface{"JETBOX", ifTypePropVirtual}, nil},
		{"официальный NetBird /1", []routeIface{{"wt0", 1}, {"wt0", 1}}, nil, []string{"wt0"}},
		{"маршруты не /1 не в счёт", []routeIface{{"Ethernet", 0}, {"OpenVPN TAP", 24}}, nil, nil},
		{"WireGuard для Windows: лучший интерфейс", nil, &bestIface{"WireGuard Tunnel", ifTypePropVirtual}, []string{"WireGuard Tunnel"}},
		{"встроенный VPN Windows", nil, &bestIface{"Работа VPN", ifTypePPP}, []string{"Работа VPN"}},
		{"туннель IfType 131", nil, &bestIface{"AnyConnect", ifTypeTunnel}, []string{"AnyConnect"}},
		{"обычный Ethernet", nil, &bestIface{"Ethernet", 6}, nil},
		{"Wi-Fi", nil, &bestIface{"Wi-Fi", 71}, nil},
		{"лучший первым, без повторов", []routeIface{{"wt0", 1}, {"WireGuard Tunnel", 1}},
			&bestIface{"WireGuard Tunnel", ifTypePropVirtual}, []string{"WireGuard Tunnel", "wt0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := foreignTunnels("JetBox", tc.routes, tc.best); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
