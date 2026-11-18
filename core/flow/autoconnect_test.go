package flow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Флаг службы «не подключаться при старте» всегда = НЕ(AutoConnect коробки И защита
// включена человеком): после включения — !AutoConnect, после выключения щитом — true.

func TestConnectSetsAutoConnectAfterUp(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets, traceAC: true}
	f, s := newFlow(t, d)
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net",
		"Up", "DisableAutoConnect false", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); !b.AutoConnect {
		t.Fatal("новая коробка: автоподключение по умолчанию включено")
	}
}

// Своя коробка с выключенной галочкой: выбор сохраняется, служба при старте не подключается.
func TestConnectSameBoxKeepsAutoConnectOff(t *testing.T) {
	d := &fakeDaemon{status: "Idle", nets: demoNets}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit", ExitNode: "sv-demo-exit", AutoConnect: false})
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); b.AutoConnect {
		t.Fatal("выбор «не подключаться автоматически» потерян")
	}
	if !reflect.DeepEqual(d.ac, []bool{true}) {
		t.Fatalf("ac %v", d.ac)
	}
}

// Неудачное подключение флаг не трогает.
func TestConnectFailureLeavesAutoConnect(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", loginErr: errors.New("boom")}
	f, _ := newFlow(t, d)
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if len(d.ac) != 0 {
		t.Fatalf("ac %v", d.ac)
	}
}

// Ошибка выставления флага включению не мешает.
func TestConnectAutoConnectErrorIgnored(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets, acErr: errors.New("update settings are disabled")}
	f, _ := newFlow(t, d)
	var logged []string
	f.cfg.Logf = func(format string, args ...any) { logged = append(logged, format) }
	res, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if err != nil || res.ExitID != "sv-demo-exit" {
		t.Fatal(res, err)
	}
	if len(logged) != 1 {
		t.Fatalf("журнал %v", logged)
	}
}

func TestReconnectSetsAutoConnect(t *testing.T) {
	for _, on := range []bool{true, false} {
		d := &fakeDaemon{status: "Idle", nets: rootNets("exit-1"), traceAC: true}
		f, s := newFlow(t, d)
		_ = s.Save(Box{Identity: "netbird.jet-net.net|", ExitNode: "exit-5", AutoConnect: on})
		if err := f.Reconnect(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := []string{"Status", "Up", "DisableAutoConnect " + map[bool]string{true: "false", false: "true"}[on], "SelectExit exit-5"}
		if !reflect.DeepEqual(d.calls, want) {
			t.Fatalf("AutoConnect=%v: calls %v", on, d.calls)
		}
	}
}

func TestReconnectFailureLeavesAutoConnect(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin"}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y", AutoConnect: true})
	_ = f.Reconnect(context.Background())
	d.status, d.upErr = "Idle", errors.New("boom")
	_ = f.Reconnect(context.Background())
	if len(d.ac) != 0 {
		t.Fatalf("ac %v", d.ac)
	}
}

// Выключение щитом запоминается: после перезагрузки служба сама не подключится.
func TestDisconnectDisablesAutoConnect(t *testing.T) {
	d := &fakeDaemon{status: "Connected", traceAC: true}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y", AutoConnect: true})
	if err := f.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Down", "DisableAutoConnect true"}) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); !b.AutoConnect {
		t.Fatal("выключение щитом не меняет галочку")
	}
}

func TestDisconnectFailureLeavesAutoConnect(t *testing.T) {
	d := &fakeDaemon{downErr: errors.New("boom")}
	f, _ := newFlow(t, d)
	_ = f.Disconnect(context.Background())
	if len(d.ac) != 0 {
		t.Fatalf("ac %v", d.ac)
	}
}

func TestDisconnectAutoConnectErrorIgnored(t *testing.T) {
	d := &fakeDaemon{acErr: errors.New("boom")}
	f, _ := newFlow(t, d)
	if err := f.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Отмена после Up опускает службу — защита выключена, флаг тоже.
func TestCancelAfterUpDisablesAutoConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &fakeDaemon{status: "Idle", traceAC: true}
	d.upHook = func(context.Context) error { cancel(); return context.Canceled }
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y", AutoConnect: true})
	_ = f.Reconnect(ctx)
	if !reflect.DeepEqual(d.calls, []string{"Status", "Up", "Down", "DisableAutoConnect true"}) {
		t.Fatalf("calls %v", d.calls)
	}
}

// Галочка: защита включена — флаг = !галочка; выключена — флаг остаётся true.
func TestSetAutoConnect(t *testing.T) {
	cases := []struct {
		svc    string
		on     bool
		wantAC bool
	}{
		{"Connected", false, true},
		{"Connected", true, false},
		{"Connecting", true, false},
		{"Idle", true, true},
		{"Idle", false, true},
		{"NeedsLogin", true, true},
	}
	for _, c := range cases {
		d := &fakeDaemon{status: c.svc, traceAC: true}
		f, s := newFlow(t, d)
		_ = s.Save(Box{Identity: "x|y", AutoConnect: !c.on})
		if err := f.SetAutoConnect(context.Background(), c.on); err != nil {
			t.Fatal(err)
		}
		if b, _, _ := s.Load(); b.AutoConnect != c.on {
			t.Fatalf("%s %v: в коробке %v", c.svc, c.on, b.AutoConnect)
		}
		want := []string{"Status", "DisableAutoConnect " + map[bool]string{true: "true", false: "false"}[c.wantAC]}
		if !reflect.DeepEqual(d.calls, want) {
			t.Fatalf("%s %v: calls %v", c.svc, c.on, d.calls)
		}
	}
}

func TestSetAutoConnectWithoutBox(t *testing.T) {
	d := &fakeDaemon{status: "Idle"}
	f, _ := newFlow(t, d)
	if err := f.SetAutoConnect(context.Background(), true); err == nil {
		t.Fatal("без коробки галочке негде храниться")
	}
	if len(d.ac) != 0 {
		t.Fatalf("ac %v", d.ac)
	}
}

// Служба не ответила на SetConfig — галочка всё равно запомнена, ошибки нет.
func TestSetAutoConnectDaemonErrorIgnored(t *testing.T) {
	d := &fakeDaemon{status: "Connected", acErr: errors.New("boom")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y", AutoConnect: true})
	if err := f.SetAutoConnect(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); b.AutoConnect {
		t.Fatal("галочка не запомнена")
	}
}

// Старый box.json (≤1.0.3) без поля — автоподключение включено.
func TestStoreAutoConnectDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.json")
	if err := os.WriteFile(path, []byte(`{"identity":"x|y","exit_node":"y"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if b, ok, _ := s.Load(); !ok || !b.AutoConnect {
		t.Fatalf("%+v", b)
	}
	if err := s.Save(Box{Identity: "x|y", AutoConnect: false}); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); b.AutoConnect {
		t.Fatalf("выключенная галочка не сохранилась: %+v", b)
	}
}

// Отвязка: после Down флаг ставится до Logout — если Logout не прошёл (нет сети), служба
// после перезагрузки не подключится к забытой коробке.
func TestForgetDisablesAutoConnect(t *testing.T) {
	d := &fakeDaemon{status: "Connected", traceAC: true, logoutErr: errors.New("offline")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y", AutoConnect: true})
	_ = f.Forget(context.Background())
	if !reflect.DeepEqual(d.calls, []string{"Down", "DisableAutoConnect true", "Logout"}) {
		t.Fatalf("calls %v", d.calls)
	}
}

// Замена подключения: флаг прежнего профиля не трогаем (активным он больше не будет),
// новый профиль включён — флаг по галочке.
func TestReplaceSetsAutoConnectInNewProfile(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, traceAC: true}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "old.example.com|x", ManagementURL: "https://old.example.com", AutoConnect: true})
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Down", "AddProfile", "ConfigureProfile new1 https://netbird.jet-net.net JetBox", "LoginProfile new1 https://netbird.jet-net.net", "SwitchProfile new1", "Up", "DisableAutoConnect false", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
}

// Up не дождался подключения (тайм-аут службы), а служба подключается дальше — защита
// включена человеком: флаг по галочке.
func TestUpTimeoutButConnectingSetsAutoConnect(t *testing.T) {
	for _, svc := range []string{"Connecting", "Connected"} {
		d := &fakeDaemon{statuses: []string{"Idle", svc}, upErr: errors.New("up: timeout waiting for connection"), traceAC: true}
		f, s := newFlow(t, d)
		_ = s.Save(Box{Identity: "x|y", AutoConnect: true})
		if err := f.Reconnect(context.Background()); err == nil {
			t.Fatal("ожидалась ошибка")
		}
		if !reflect.DeepEqual(d.calls, []string{"Status", "Up", "Status", "DisableAutoConnect false"}) {
			t.Fatalf("%s: calls %v", svc, d.calls)
		}
	}
}

func TestUpErrorIdleLeavesAutoConnect(t *testing.T) {
	d := &fakeDaemon{statuses: []string{"Idle", "Idle"}, upErr: errors.New("boom")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y", AutoConnect: true})
	_ = f.Reconnect(context.Background())
	if len(d.ac) != 0 {
		t.Fatalf("ac %v", d.ac)
	}
}

// Первый запуск 1.0.4: флаг выставляется по текущему состоянию службы, ошибка — наружу
// (метку тогда не пишут, повторят при следующем запуске).
func TestSyncAutoConnect(t *testing.T) {
	cases := []struct {
		svc     string
		box     bool
		auto    bool
		disable bool
	}{
		{"Connected", true, true, false},
		{"Connecting", true, false, true},
		{"Idle", true, true, true},
		{"Connected", false, false, true},
	}
	for _, c := range cases {
		d := &fakeDaemon{status: c.svc}
		f, s := newFlow(t, d)
		if c.box {
			_ = s.Save(Box{Identity: "x|y", AutoConnect: c.auto})
		}
		if err := f.SyncAutoConnect(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d.ac, []bool{c.disable}) {
			t.Fatalf("%+v: ac %v", c, d.ac)
		}
	}
	d := &fakeDaemon{status: "Idle", acErr: errors.New("boom")}
	f, _ := newFlow(t, d)
	if err := f.SyncAutoConnect(context.Background()); err == nil {
		t.Fatal("ошибка службы потеряна")
	}
}
