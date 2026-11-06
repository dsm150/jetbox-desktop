package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/app/controller"
)

// fakeAutostart — автозапуск ОС в памяти.
type fakeAutostart struct {
	mu        sync.Mutex
	supported bool
	on        bool
	sets      []bool
	setErr    error
}

func (a *fakeAutostart) autostartSupported() bool { return a.supported }
func (a *fakeAutostart) autostartEnabled() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.on, nil
}
func (a *fakeAutostart) setAutostart(on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sets = append(a.sets, on)
	if a.setErr != nil {
		return a.setErr
	}
	a.on = on
	return nil
}

func (a *fakeAutostart) setsCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sets)
}

// Первый запуск у пользователя включает автозапуск один раз; выключенный человеком
// следующий запуск не трогает.
func TestAutostartDefaultOncePerUser(t *testing.T) {
	dir := t.TempDir()
	a := &fakeAutostart{supported: true}
	applyAutostartDefault(a, dir)
	if !a.on || a.setsCount() != 1 {
		t.Fatalf("первый запуск: on=%v sets=%v", a.on, a.sets)
	}
	if _, err := os.Stat(filepath.Join(dir, autostartMarker)); err != nil {
		t.Fatal("нет метки первого запуска:", err)
	}
	a.on = false // человек выключил в «Настройках»
	applyAutostartDefault(a, dir)
	if a.on || a.setsCount() != 1 {
		t.Fatalf("повторный запуск включил автозапуск: on=%v sets=%v", a.on, a.sets)
	}
}

// Установщик уже записал автозапуск — включать нечего, метка ставится.
func TestAutostartDefaultAlreadyOn(t *testing.T) {
	dir := t.TempDir()
	a := &fakeAutostart{supported: true, on: true}
	applyAutostartDefault(a, dir)
	if a.setsCount() != 0 {
		t.Fatalf("sets=%v", a.sets)
	}
	if _, err := os.Stat(filepath.Join(dir, autostartMarker)); err != nil {
		t.Fatal(err)
	}
}

// Метка пишется до включения: сбой не повторяется при каждом запуске.
func TestAutostartDefaultErrorNotRetried(t *testing.T) {
	dir := t.TempDir()
	a := &fakeAutostart{supported: true, setErr: errors.New("access denied")}
	applyAutostartDefault(a, dir)
	applyAutostartDefault(a, dir)
	if a.setsCount() != 1 {
		t.Fatalf("sets=%v", a.sets)
	}
}

// macOS и прочие: автозапуск не наш (его ставит .pkg) — ничего не трогаем, метки нет.
func TestAutostartDefaultUnsupported(t *testing.T) {
	dir := t.TempDir()
	a := &fakeAutostart{}
	applyAutostartDefault(a, dir)
	if a.setsCount() != 0 {
		t.Fatalf("sets=%v", a.sets)
	}
	if _, err := os.Stat(filepath.Join(dir, autostartMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("метка без поддержки автозапуска:", err)
	}
}

// Запуск приложения: первый — автозапуск включён и виден окну.
func TestStartAppliesAutostartDefault(t *testing.T) {
	b, _ := newTestBridge(t, "Connected")
	a := &fakeAutostart{supported: true}
	b.auto, b.dir = a, t.TempDir()
	b.start("", true)
	if st := b.ctl.State().Autostart; !st.Supported || !st.Enabled || a.setsCount() != 1 {
		t.Fatalf("%+v sets=%v", st, a.sets)
	}
	// Флаг автоподключения выставляется в фоне — дождаться, пока TempDir ещё жив.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(b.dir, autoConnectMarker)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("флаг автоподключения при первом запуске не выставлен")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSetAutostartCommand(t *testing.T) {
	b, _ := newTestBridge(t, "Connected")
	a := &fakeAutostart{supported: true, on: true}
	b.auto, b.dir = a, t.TempDir()
	b.syncAutostart()
	b.exec("setAutostart", "off")
	if st := b.ctl.State().Autostart; st.Enabled || a.on {
		t.Fatalf("off: %+v on=%v", st, a.on)
	}
	b.exec("setAutostart", "on")
	if st := b.ctl.State().Autostart; !st.Enabled || !a.on {
		t.Fatalf("on: %+v on=%v", st, a.on)
	}
	b.exec("setAutostart", "maybe")
	if a.setsCount() != 2 {
		t.Fatalf("непонятный аргумент дошёл до ОС: %v", a.sets)
	}
}

// Без поддержки ОС команда ничего не делает, строка скрыта.
func TestSetAutostartUnsupported(t *testing.T) {
	b, _ := newTestBridge(t, "Connected")
	a := &fakeAutostart{}
	b.auto = a
	b.syncAutostart()
	b.exec("setAutostart", "on")
	if st := b.ctl.State().Autostart; st.Supported || a.setsCount() != 0 {
		t.Fatalf("%+v sets=%v", st, a.sets)
	}
}

func TestSetAutoConnectCommand(t *testing.T) {
	b, eng := newTestBridge(t, "Connected")
	if !b.ctl.State().AutoConnect {
		t.Fatal("по умолчанию автоподключение включено")
	}
	b.exec("setAutoConnect", "off")
	if eng.count("SetAutoConnect") != 1 || b.ctl.State().AutoConnect {
		t.Fatalf("off: %v %+v", eng.calls, b.ctl.State())
	}
	b.exec("setAutoConnect", "on")
	if !b.ctl.State().AutoConnect {
		t.Fatal("on не дошло")
	}
	b.exec("setAutoConnect", "")
	if eng.count("SetAutoConnect") != 2 {
		t.Fatalf("пустой аргумент дошёл до службы: %v", eng.calls)
	}
}

// «Диспетчер задач → Автозагрузка» хранит выключение в StartupApproved\Run: первый байт
// с младшим битом 1 (0x03) — выключено, 0x02/0x06 — включено.
func TestStartupApprovedDisabled(t *testing.T) {
	cases := []struct {
		b    []byte
		want bool
	}{
		{nil, false},
		{[]byte{}, false},
		{[]byte{0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false},
		{[]byte{0x06, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false},
		{[]byte{0x03, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f, 0x70, 0x81, 0x92, 0xa3, 0xb4}, true},
		{[]byte{0x07}, true},
	}
	for _, c := range cases {
		if got := startupDisabled(c.b); got != c.want {
			t.Errorf("startupDisabled(% x) = %v, want %v", c.b, got, c.want)
		}
	}
}

// Active Setup (--autostart-init): только автозапуск по умолчанию, без окна и службы.
func TestAutostartInitOnly(t *testing.T) {
	dir := t.TempDir()
	a := &fakeAutostart{supported: true}
	if autostartInitOnly([]string{"--autostart"}, a, dir) || a.setsCount() != 0 {
		t.Fatalf("--autostart — обычный запуск: sets=%v", a.sets)
	}
	if !autostartInitOnly([]string{autostartInitFlag}, a, dir) || !a.on {
		t.Fatal("--autostart-init не включил автозапуск")
	}
	a.on = false // человек выключил; следующий вход с Active Setup не включает
	if !autostartInitOnly([]string{autostartInitFlag}, a, dir) || a.on {
		t.Fatal("--autostart-init включил выключенный человеком автозапуск")
	}
}

// blockedStatus — служба, которая не отвечает, пока не закроют release.
type blockedStatus struct {
	testStatus
	release chan struct{}
}

func (s *blockedStatus) Status(ctx context.Context) (*proto.StatusResponse, error) {
	<-s.release
	return s.testStatus.Status(ctx)
}

// Автозапуск по умолчанию не ждёт службу: Init при её молчании идёт до 2 мин.
func TestStartAutostartDoesNotWaitForService(t *testing.T) {
	st := &blockedStatus{testStatus: testStatus{state: "Idle"}, release: make(chan struct{})}
	ctl := controller.New(&testEngine{}, st, func(context.Context, string) (time.Duration, error) { return 0, nil },
		func(controller.State) {}, "ru", "test")
	a := &fakeAutostart{supported: true}
	b := &bridge{ctl: ctl, auto: a, dir: t.TempDir()}
	done := make(chan struct{})
	go func() { b.start("", true); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for a.setsCount() == 0 || !ctl.State().Autostart.Enabled {
		if time.Now().After(deadline) {
			t.Fatal("автозапуск ждёт ответа службы")
		}
		time.Sleep(time.Millisecond)
	}
	close(st.release)
	<-done
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(time.Millisecond) {
		if _, err := os.Stat(filepath.Join(b.dir, autoConnectMarker)); err == nil || time.Now().After(deadline) {
			break
		}
	}
}
