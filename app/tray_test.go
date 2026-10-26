package main

import (
	"testing"

	"github.com/dsm150/jetbox-desktop/app/controller"
)

func homeState(seq uint64, svc string, connected bool) controller.State {
	return controller.State{
		Seq: seq, Screen: controller.ScreenHome, BoxName: "Demo",
		Panel: controller.Panel{ServiceUp: true, ServiceState: svc, Connected: connected},
	}
}

func TestTrayModelView(t *testing.T) {
	cases := []struct {
		name    string
		s       controller.State
		look    trayLook
		text    string
		off     bool
		enabled bool
	}{
		{"on", homeState(1, "Connected", true), lookOn, "on", true, true},
		{"off", homeState(1, "Idle", false), lookOff, "off", false, true},
		{"connecting", homeState(1, "Connecting", false), lookConnecting, "connecting", true, true},
		{"busy", func() controller.State { s := homeState(1, "Idle", false); s.Busy = true; return s }(), lookOff, "off", false, false},
		{"nobox", controller.State{Seq: 1, Screen: controller.ScreenNoBox, Panel: controller.Panel{ServiceUp: true}}, lookOff, "nobox", false, false},
		{"service", controller.State{Seq: 1, Screen: controller.ScreenServiceMissing, BoxName: "Demo"}, lookAlert, "service", false, false},
		{"exitDown", func() controller.State { s := homeState(1, "Connected", false); s.Panel.ExitDown = true; return s }(), lookAlert, "exitDown", true, true},
		{"progress", controller.State{Seq: 1, Screen: controller.ScreenProgress, Panel: controller.Panel{ServiceUp: true}}, lookConnecting, "connecting", false, false},
	}
	for _, c := range cases {
		var m trayModel
		if !m.apply(c.s, "en") {
			t.Errorf("%s: первое состояние не применено", c.name)
			continue
		}
		if m.look != c.look || m.text != tr("en", c.text) || m.off != c.off || m.enabled != c.enabled {
			t.Errorf("%s: look=%d text=%q off=%v enabled=%v", c.name, m.look, m.text, m.off, m.enabled)
		}
	}
}

// Трей применяет только состояние новее уже применённого.
func TestTrayModelIgnoresStaleSeq(t *testing.T) {
	var m trayModel
	if !m.apply(homeState(5, "Connected", true), "en") {
		t.Fatal("seq 5 не применён")
	}
	if m.apply(homeState(4, "Idle", false), "en") || m.apply(homeState(5, "Idle", false), "en") {
		t.Fatal("устаревшее состояние применено")
	}
	if m.look != lookOn || !m.off {
		t.Fatalf("после устаревших: look=%d off=%v", m.look, m.off)
	}
	if !m.apply(homeState(6, "Idle", false), "en") || m.look != lookOff {
		t.Fatalf("seq 6: look=%d", m.look)
	}
}

func TestTrayIntent(t *testing.T) {
	var m trayModel
	m.apply(homeState(1, "Connected", true), "en")
	if m.intent() != "off" {
		t.Fatalf("intent при «Выключить» = %q", m.intent())
	}
	m.apply(homeState(2, "Idle", false), "en")
	if m.intent() != "on" {
		t.Fatalf("intent при «Включить» = %q", m.intent())
	}
}

// Значок по виду: свой рисунок у каждого состояния, у «подключаем» — два кадра мигания
// (приглушённый след и монохром), у остальных кадр один.
func TestTrayIconFrames(t *testing.T) {
	seen := map[string]trayLook{}
	for _, l := range []trayLook{lookOff, lookOn, lookConnecting, lookAlert} {
		light, dark := trayIcon(l, 0)
		if len(light) == 0 || len(dark) == 0 || string(light) == string(dark) {
			t.Fatalf("look %d: нет пары светлый/тёмный", l)
		}
		if prev, ok := seen[string(light)]; ok {
			t.Fatalf("look %d рисуется как look %d", l, prev)
		}
		seen[string(light)] = l
		if blinks(l) != (l == lookConnecting) {
			t.Fatalf("look %d: мигание %v", l, blinks(l))
		}
	}
	a, _ := trayIcon(lookConnecting, 0)
	b, _ := trayIcon(lookConnecting, 1)
	off, _ := trayIcon(lookOff, 0)
	if string(a) == string(b) || string(b) != string(off) {
		t.Fatal("«подключаем»: второй кадр — монохромный знак")
	}
}
