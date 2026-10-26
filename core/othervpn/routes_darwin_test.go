//go:build darwin

package othervpn

import (
	"reflect"
	"testing"
)

func TestTunnelFromRouteGet(t *testing.T) {
	viaUtun := "   route to: 1.1.1.1\ndestination: 0.0.0.0\n       mask: 128.0.0.0\n  interface: utun100\n      flags: <UP,DONE,STATIC,PRCLONING>\n"
	viaEn0 := "   route to: 1.1.1.1\ndestination: default\n  interface: en0\n"
	if got := tunnelFromRouteGet(viaUtun); !reflect.DeepEqual(got, []string{"utun100"}) {
		t.Fatal(got)
	}
	if got := tunnelFromRouteGet(viaEn0); got != nil {
		t.Fatal(got)
	}
}
