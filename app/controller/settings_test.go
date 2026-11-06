package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/flow"
)

// «Подключаться автоматически» — из коробки; без коробки — выключено (строка скрыта).
func TestStateCarriesAutoConnect(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo", AutoConnect: true}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	if !c.State().AutoConnect {
		t.Fatal("галочка из коробки не дошла до окна")
	}
	c.SetAutoConnect(context.Background(), false)
	if st := c.State(); st.AutoConnect || e.count("SetAutoConnect") != 1 || e.box.AutoConnect {
		t.Fatalf("%+v %v", st, e.calls)
	}
	c.SetAutoConnect(context.Background(), true)
	if !c.State().AutoConnect {
		t.Fatal("галочка не включилась")
	}
	c.Forget(context.Background())
	if c.State().AutoConnect {
		t.Fatal("после отвязки коробки нет — галочки тоже")
	}
}

func TestSetAutoConnectWithoutBoxIsNoop(t *testing.T) {
	e := &fakeEngine{}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.Init(context.Background())
	c.SetAutoConnect(context.Background(), true)
	if e.count("SetAutoConnect") != 0 || c.State().AutoConnect {
		t.Fatal(e.calls)
	}
}

// Ошибка — без экрана ошибки: галочка показывает то, что в коробке.
func TestSetAutoConnectErrorKeepsScreen(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo", AutoConnect: true}, hasBox: true, acErr: errors.New("disk full")}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	c.SetAutoConnect(context.Background(), false)
	if st := c.State(); st.Screen != ScreenHome || st.ErrorKind != "" || !st.AutoConnect {
		t.Fatalf("%+v", st)
	}
}

// Во время действия галочка не меняется (core/flow ждал бы его конца).
func TestSetAutoConnectWhileBusyIsNoop(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo", AutoConnect: true}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	c.update(func(s *State) { s.Busy = true })
	c.SetAutoConnect(context.Background(), false)
	if e.count("SetAutoConnect") != 0 {
		t.Fatal(e.calls)
	}
}

func TestSetAutostartState(t *testing.T) {
	c, seen := newCtl(&fakeEngine{}, &fakeStatus{st: connected()})
	c.SetAutostart(Autostart{Supported: true, Enabled: true})
	if st := c.State(); !st.Autostart.Supported || !st.Autostart.Enabled {
		t.Fatalf("%+v", st.Autostart)
	}
	n := len(*seen)
	c.SetAutostart(Autostart{Supported: true, Enabled: true})
	if len(*seen) != n {
		t.Fatal("то же состояние автозапуска рассылается заново")
	}
	c.SetAutostart(Autostart{Supported: true})
	if c.State().Autostart.Enabled {
		t.Fatal("выключение автозапуска не дошло")
	}
}
