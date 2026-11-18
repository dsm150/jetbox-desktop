package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/app/controller"
	"github.com/dsm150/jetbox-desktop/core/exitpick"
	"github.com/dsm150/jetbox-desktop/core/flow"
	"github.com/dsm150/jetbox-desktop/core/jetboxlink"
)

const testLink = "jetbox://connect/#k=0EF79C2F-1A34-4B21-9C0D-1BF529332998&m=https%3A%2F%2Fnetbird.jet-net.net&e=sv-demo-exit&n=Demo"

// testEngine — коробка есть; вызовы службы считаются и могут ждать block.
type testEngine struct {
	mu      sync.Mutex
	calls   map[string]int
	block   chan struct{}
	entered chan string

	autoConnect *bool // последняя галочка; nil — не меняли (в коробке включено)
	syncErr     error // ответ SyncAutoConnect
}

func (e *testEngine) enter(name string) {
	e.mu.Lock()
	if e.calls == nil {
		e.calls = map[string]int{}
	}
	e.calls[name]++
	blk, ent := e.block, e.entered
	e.mu.Unlock()
	if ent != nil {
		ent <- name
	}
	if blk != nil {
		<-blk
	}
}

func (e *testEngine) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls[name]
}

func (e *testEngine) Connect(context.Context, jetboxlink.Payload, flow.Options, func(flow.Stage)) (flow.Result, error) {
	e.enter("Connect")
	return flow.Result{}, nil
}
func (e *testEngine) ChooseExit(context.Context, string) error { e.enter("ChooseExit"); return nil }
func (e *testEngine) ChooseNoExit(context.Context) error       { e.enter("ChooseNoExit"); return nil }
func (e *testEngine) Exits(context.Context) ([]exitpick.Candidate, error) {
	e.enter("Exits")
	return []exitpick.Candidate{{ID: "a", Selected: true}, {ID: "b"}}, nil
}
func (e *testEngine) Reconcile(context.Context) error  { e.enter("Reconcile"); return nil }
func (e *testEngine) Disconnect(context.Context) error { e.enter("Disconnect"); return nil }
func (e *testEngine) Reconnect(context.Context) error  { e.enter("Reconnect"); return nil }
func (e *testEngine) Forget(context.Context) error     { e.enter("Forget"); return nil }
func (e *testEngine) SetAutoConnect(_ context.Context, on bool) error {
	e.enter("SetAutoConnect")
	e.mu.Lock()
	e.autoConnect = &on
	e.mu.Unlock()
	return nil
}
func (e *testEngine) SyncAutoConnect(context.Context) error {
	e.enter("SyncAutoConnect")
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.syncErr
}
func (e *testEngine) Box() (flow.Box, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ac := e.autoConnect == nil || *e.autoConnect
	return flow.Box{Identity: "id", Name: "Demo", AutoConnect: ac}, true, nil
}

type testStatus struct {
	mu    sync.Mutex
	state string
}

func (s *testStatus) Status(context.Context) (*proto.StatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &proto.StatusResponse{Status: s.state}, nil
}

func (s *testStatus) Networks(context.Context) ([]exitpick.Network, error) { return nil, nil }

// newTestBridge — мост без Wails: контроллер с коробкой, служба в состоянии svc.
func newTestBridge(t *testing.T, svc string) (*bridge, *testEngine) {
	t.Helper()
	eng := &testEngine{}
	probe := func(context.Context, string) (time.Duration, error) { return 0, nil }
	ctl := controller.New(eng, &testStatus{state: svc}, probe, func(controller.State) {}, "ru", "test")
	ctl.Init(context.Background())
	if s := ctl.State(); s.Screen != controller.ScreenHome || !s.Panel.ServiceUp {
		t.Fatalf("init: screen=%s serviceUp=%v", s.Screen, s.Panel.ServiceUp)
	}
	return &bridge{ctl: ctl}, eng
}

func TestToggleWanted(t *testing.T) {
	cases := []struct {
		intent, svc string
		want        bool
	}{
		{"", "Idle", true},
		{"", "Connected", true},
		{"on", "Idle", true},
		{"on", "Connected", false},
		{"on", "Connecting", false},
		{"off", "Idle", false},
		{"off", "Connected", true},
		{"off", "Connecting", true},
		{"off", "", false},
	}
	for _, c := range cases {
		s := controller.State{Panel: controller.Panel{ServiceState: c.svc}}
		if got := toggleWanted(c.intent, s); got != c.want {
			t.Errorf("toggleWanted(%q, %q) = %v, want %v", c.intent, c.svc, got, c.want)
		}
	}
}

// Двойной клик по щиту: второе «выключить» после того, как служба уже выключена, — не включает.
func TestToggleIntentOffWhenIdleIsNoop(t *testing.T) {
	b, eng := newTestBridge(t, "Idle")
	b.exec("toggle", "off")
	if n := eng.count("Reconnect") + eng.count("Disconnect"); n != 0 {
		t.Fatalf("toggle off при Idle: вызовов службы %d, want 0", n)
	}
	b.exec("toggle", "on")
	if eng.count("Reconnect") != 1 {
		t.Fatalf("toggle on при Idle: Reconnect=%d, want 1", eng.count("Reconnect"))
	}
}

func TestToggleIntentOnWhenConnectedIsNoop(t *testing.T) {
	b, eng := newTestBridge(t, "Connected")
	b.exec("toggle", "on")
	if n := eng.count("Reconnect") + eng.count("Disconnect"); n != 0 {
		t.Fatalf("toggle on при Connected: вызовов службы %d, want 0", n)
	}
	b.exec("toggle", "off")
	if eng.count("Disconnect") != 1 {
		t.Fatalf("toggle off при Connected: Disconnect=%d, want 1", eng.count("Disconnect"))
	}
	b.exec("toggle", "") // без намерения — как раньше
	if eng.count("Disconnect") != 2 {
		t.Fatalf("toggle без намерения: Disconnect=%d, want 2", eng.count("Disconnect"))
	}
}

// Команды не стоят в общей очереди: пока идёт действие, повтор упирается в Busy
// контроллера и возвращается сразу, а не выполняется следом.
func TestCommandsDoNotQueueBehindBusy(t *testing.T) {
	b, eng := newTestBridge(t, "Idle")
	eng.block, eng.entered = make(chan struct{}), make(chan string, 4)

	done := make(chan struct{})
	go func() { b.exec("toggle", "on"); close(done) }()
	if got := <-eng.entered; got != "Reconnect" {
		t.Fatalf("первым вызван %s", got)
	}

	second := make(chan struct{})
	go func() {
		b.exec("toggle", "on")
		b.exec("chooseExit", "x")
		b.exec("forget", "")
		close(second)
	}()
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("команды во время Busy ждут первую — общая очередь")
	}
	close(eng.block)
	<-done
	if eng.count("Reconnect") != 1 || eng.count("ChooseExit") != 0 || eng.count("Forget") != 0 {
		t.Fatalf("вызовы: %v", eng.calls)
	}
}

// Ссылка до Init не теряется, даже если контроллер ещё не создан.
func TestOpenLinkBeforeStartIsKept(t *testing.T) {
	b := &bridge{}
	b.openLink(testLink)
	b.startMu.Lock()
	got := b.earlyLink
	b.startMu.Unlock()
	if got != testLink {
		t.Fatalf("earlyLink = %q", got)
	}
}

func TestSecondInstanceBeforeStart(t *testing.T) {
	b := &bridge{}
	b.onSecondInstance([]string{"--autostart"})
	if b.wantShow {
		t.Fatal("второй запуск автозапуском не должен показывать окно")
	}
	b.onSecondInstance(nil)
	if !b.wantShow {
		t.Fatal("второй запуск без ссылки: показ окна должен быть отложен до готовности")
	}
	b.onSecondInstance([]string{testLink})
	if b.earlyLink != testLink {
		t.Fatalf("ссылка второго запуска до Init потеряна: %q", b.earlyLink)
	}
}

// «Сменить» и «Без выхода» из окна доходят до контроллера.
func TestChangeExitAndNoExitCommands(t *testing.T) {
	b, eng := newTestBridge(t, "Connected")
	b.exec("changeExit", "")
	if s := b.ctl.State(); eng.count("Exits") != 1 || s.Screen != controller.ScreenChooseExit || len(s.Candidates) != 2 || !s.Candidates[0].Active {
		t.Fatalf("changeExit: %+v %v", s, eng.calls)
	}
	b.exec("noExit", "")
	if s := b.ctl.State(); eng.count("ChooseNoExit") != 1 || s.Screen != controller.ScreenHome {
		t.Fatalf("noExit: %+v %v", s, eng.calls)
	}
}

// Ручной запуск при подключённой коробке показывает окно: на Windows 11 значок
// трея прячется в скрытые, и без окна человек не видит, что приложение открылось.
// Автозапуск при входе в систему — только значок.
func TestColdStartHomeShowsWindowUnlessAutostart(t *testing.T) {
	b, _ := newTestBridge(t, "Connected")
	b.start("", true)
	if b.wantShow {
		t.Fatal("автозапуск при входе не должен показывать окно")
	}
	b, _ = newTestBridge(t, "Connected")
	b.start("", false)
	if !b.wantShow {
		t.Fatal("ручной запуск на главном экране должен показать окно")
	}
}

// В журнал идёт текст отказа, а не только вид: errorKind="Unknown" без причины не разобрать
// (02.11.2026 — служба отклоняла SetConfig, в app.log было только Unknown).
func TestScreenLogLineCarriesErrorDetail(t *testing.T) {
	got := screenLogLine(controller.State{Screen: controller.ScreenError, ErrorKind: "Unknown",
		ErrorDetail: "Unknown: настройка службы: update settings are disabled"})
	want := `screen error errorKind="Unknown" detail="Unknown: настройка службы: update settings are disabled"`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got := screenLogLine(controller.State{Screen: controller.ScreenReady}); got != `screen ready errorKind=""` {
		t.Fatalf("без отказа: %s", got)
	}
}

// «Выход» во время действия: отменяет его контекст и ждёт, пока оно закончит откат.
func TestQuitCancelsAndWaitsForActions(t *testing.T) {
	b := &bridge{}
	ctx, done := b.action()
	rolledBack := make(chan struct{})
	go func() {
		<-ctx.Done()                      // отмена дошла до действия
		time.Sleep(50 * time.Millisecond) // откат
		close(rolledBack)
		done()
	}()
	b.quit()
	select {
	case <-rolledBack:
	default:
		t.Fatal("выход не дождался отката")
	}
	// Без действий — сразу.
	start := time.Now()
	(&bridge{}).quit()
	if time.Since(start) > time.Second {
		t.Fatal("выход без действий ждал")
	}
}

func TestRecoverReplaceLogsOnly(t *testing.T) {
	calls := 0
	b := &bridge{recover: func(context.Context) (bool, error) { calls++; return false, errors.New("служба занята") }}
	b.recoverReplace(context.Background())
	(&bridge{}).recoverReplace(context.Background()) // nil — ничего
	if calls != 1 {
		t.Fatal(calls)
	}
}

// Сверка тронула службу — экран обновляется по её новому состоянию.
func TestRecoverReplaceRefreshesScreen(t *testing.T) {
	ts := &testStatus{state: "Connected"}
	probe := func(context.Context, string) (time.Duration, error) { return 0, nil }
	ctl := controller.New(&testEngine{}, ts, probe, func(controller.State) {}, "ru", "test")
	ctl.Init(context.Background())
	b := &bridge{ctl: ctl}
	ts.mu.Lock()
	ts.state = "Idle" // сверка опустила службу и вернула профиль
	ts.mu.Unlock()
	b.recover = func(context.Context) (bool, error) { return false, nil }
	b.recoverReplace(context.Background())
	if got := ctl.State().Panel.ServiceState; got != "Connected" {
		t.Fatalf("без изменений экран не трогаем: %s", got)
	}
	b.recover = func(context.Context) (bool, error) { return true, nil }
	b.recoverReplace(context.Background())
	if got := ctl.State().Panel.ServiceState; got != "Idle" {
		t.Fatalf("экран не обновлён: %s", got)
	}
}

// После начала выхода новое действие получает отменённый контекст, и выход его не ждёт.
func TestActionAfterQuitIsCanceled(t *testing.T) {
	b := &bridge{}
	b.quit()
	ctx, done := b.action()
	defer done()
	if ctx.Err() == nil {
		t.Fatal("контекст после выхода не отменён")
	}
	start := time.Now()
	b.quit() // повторный выход не ждёт действие, взятое после флага
	if time.Since(start) > time.Second {
		t.Fatal("выход ждал действие, начатое после него")
	}
}
