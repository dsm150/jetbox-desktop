//go:build windows

package othervpn

import "testing"

// Дымовой тест на живой Windows: признаки снимаются без ошибки; что нашли — в лог.
func TestInternetTunnelsOnThisMachine(t *testing.T) {
	best, err := bestInterface()
	if err != nil {
		t.Logf("лучший интерфейс: %v", err)
	} else {
		t.Logf("лучший интерфейс: %q, IfType %d", best.Alias, best.Type)
	}
	routes, err := halfDefaultRoutes()
	t.Logf("маршруты /1: %+v, ошибка %v", routes, err)
	got, err := internetTunnels("JetBox")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("чужие туннели: %q", got)
}
