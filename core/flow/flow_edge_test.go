package flow

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
)

func TestNormManagement(t *testing.T) {
	same := [][2]string{
		{"https://netbird.jet-net.net:443", "https://netbird.jet-net.net"},
		{"https://NetBird.Jet-Net.net/", "https://netbird.jet-net.net"},
		{"HTTPS://netbird.jet-net.net:443/", "https://netbird.jet-net.net"},
		{" https://netbird.jet-net.net ", "https://netbird.jet-net.net:443"},
		{"https://[2001:db8::1]:443", "https://[2001:db8::1]"},
	}
	for _, c := range same {
		if !sameManagement(c[0], c[1]) {
			t.Errorf("%q и %q должны совпасть: %q / %q", c[0], c[1], normManagement(c[0]), normManagement(c[1]))
		}
	}
	diff := [][2]string{
		{"https://api.netbird.io:443", "https://netbird.jet-net.net"},
		{"https://netbird.jet-net.net:33073", "https://netbird.jet-net.net"},
		{"http://netbird.jet-net.net", "https://netbird.jet-net.net"},
	}
	for _, c := range diff {
		if sameManagement(c[0], c[1]) {
			t.Errorf("%q и %q не должны совпасть", c[0], c[1])
		}
	}
}

func TestEmptyStoreDaemonOnOtherManagementNoQuestion(t *testing.T) {
	// свежий профиль службы: Connecting к api.netbird.io (умолчание апстрима) —
	// спрашивать не о чем; Down и Logout по возможности, их ошибки не мешают.
	d := &fakeDaemon{status: "Connecting", mgmt: "https://api.netbird.io:443", nets: demoNets,
		downErr:   errors.New("down failed"),
		logoutErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, s := newFlow(t, d)
	res, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if err != nil || res.ExitID != "sv-demo-exit" {
		t.Fatal(res, err)
	}
	want := []string{"Status", "Down", "Logout", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, ok, _ := s.Load(); !ok || b.Identity != "netbird.jet-net.net|sv-demo-exit" {
		t.Fatalf("box %+v %v", b, ok)
	}
}

func TestEmptyStoreSameManagementExitPresentAdopts(t *testing.T) {
	// тот же management и выход из ссылки виден службе — это та же коробка
	// (второй пользователь Windows, потерянный %APPDATA%): не снимать с учёта.
	d := &fakeDaemon{status: "Connected", mgmt: "https://netbird.jet-net.net:443", nets: demoNets}
	f, s := newFlow(t, d)
	var stages []Stage
	res, err := f.Connect(context.Background(), link(t, demoLink), Options{}, func(st Stage) { stages = append(stages, st) })
	if err != nil || res.ExitID != "sv-demo-exit" {
		t.Fatal(res, err)
	}
	want := []string{"Status", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if !reflect.DeepEqual(stages, []Stage{StageConnecting, StageChoosingExit, StageProtected}) {
		t.Fatalf("stages %v", stages)
	}
	b, ok, _ := s.Load()
	if !ok || b.Identity != "netbird.jet-net.net|sv-demo-exit" || b.ManagementURL != "https://netbird.jet-net.net" || b.RegisteredAt.IsZero() {
		t.Fatalf("box %+v %v", b, ok)
	}
}

func TestEmptyStoreSameManagementExitAbsentAsks(t *testing.T) {
	for name, tc := range map[string]struct {
		link string
		nets []exitpick.Network
	}{
		"выхода нет в сетях":    {demoLink, []exitpick.Network{{ID: "other-exit", Range: "0.0.0.0/0"}}},
		"ссылка без выхода":     {"jetbox://connect#k=" + key, demoNets},
		"сети не получить":      {demoLink, nil},
		"management неизвестен": {demoLink, []exitpick.Network{{ID: "other-exit", Range: "0.0.0.0/0"}}},
	} {
		t.Run(name, func(t *testing.T) {
			d := &fakeDaemon{status: "Connected", mgmt: "https://netbird.jet-net.net:443", nets: tc.nets}
			if tc.nets == nil {
				d.netsErr = errors.New("not connected")
			}
			if name == "management неизвестен" {
				d.mgmt = ""
			}
			f, s := newFlow(t, d)
			_, err := f.Connect(context.Background(), link(t, tc.link), Options{}, nil)
			var fe *Error
			if !errors.As(err, &fe) || fe.Kind != KindOtherBox || fe.Detail != "" {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.calls, []string{"Status"}) {
				t.Fatalf("служба тронута без подтверждения: %v", d.calls)
			}
			if _, ok, _ := s.Load(); ok {
				t.Fatal("коробка записана без подтверждения")
			}
		})
	}
}

func TestSwitchDaemonNeedsLoginLogoutFailureContinues(t *testing.T) {
	// management уже отверг старую регистрацию (NeedsLogin): Logout ей не нужен.
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets,
		logoutErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|other-exit", ManagementURL: "https://netbird.jet-net.net"})
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Down", "Logout", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
}

func TestSwitchSameManagementIsNormalized(t *testing.T) {
	// :443 и хвостовой слэш — тот же management: Logout не прошёл — стоп.
	d := &fakeDaemon{status: "Connected", nets: demoNets,
		logoutErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|other-exit", ManagementURL: "https://NetBird.jet-net.net:443/"})
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil)
	if KindOf(err) != KindServerUnreachable {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Down", "Logout"}) {
		t.Fatalf("calls %v", d.calls)
	}
}

func TestSameBoxLoginFailedKeepsRegisteredAt(t *testing.T) {
	// LoginFailed — не прошла проверка входа (сеть), регистрация жива:
	// Configure не нужен, дата регистрации остаётся.
	d := &fakeDaemon{status: "LoginFailed", nets: demoNets}
	f, s := newFlow(t, d)
	regAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit", ManagementURL: "https://netbird.jet-net.net", RegisteredAt: regAt})
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); !b.RegisteredAt.Equal(regAt) {
		t.Fatalf("RegisteredAt потерян: %+v", b)
	}
}

func TestReconnectLoginFailedLogsInAgain(t *testing.T) {
	d := &fakeDaemon{status: "LoginFailed"}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit", ManagementURL: "https://netbird.jet-net.net"})
	if err := f.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Login https://netbird.jet-net.net", "Up"}) {
		t.Fatalf("calls %v", d.calls)
	}
	if !reflect.DeepEqual(d.loginKeys, []string{""}) {
		t.Fatalf("Login должен идти без ключа: %q", d.loginKeys)
	}
}

func TestReconnectLoginFailedNetworkError(t *testing.T) {
	for _, online := range []bool{false, true} {
		want := KindServerUnreachable
		if !online {
			want = KindNoInternet
		}
		on := online
		d := &fakeDaemon{status: "LoginFailed", loginErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
		f, s := newFlow(t, d)
		f.cfg.Online = func(context.Context) bool { return on }
		_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit", ManagementURL: "https://netbird.jet-net.net"})
		if err := f.Reconnect(context.Background()); KindOf(err) != want {
			t.Fatalf("online=%v: %v", online, err)
		}
		if !reflect.DeepEqual(d.calls, []string{"Status", "Login https://netbird.jet-net.net"}) {
			t.Fatalf("calls %v", d.calls)
		}
	}
}

func TestReconnectLoginFailedRegistrationGoneNeedsLink(t *testing.T) {
	for name, d := range map[string]*fakeDaemon{
		"браузер":      {status: "LoginFailed", browser: true},
		"нет SSO":      {status: "LoginFailed", loginErr: status.Error(codes.Unknown, "no SSO provider returned from management. Please proceed with setting up this device using setup keys")},
		"не умеет SSO": {status: "LoginFailed", loginErr: status.Error(codes.Unknown, "the management server, https://netbird.jet-net.net:443, does not support SSO providers, please update your server or use Setup Keys to login")},
	} {
		f, s := newFlow(t, d)
		_ = s.Save(Box{Identity: "x|y", ManagementURL: "https://netbird.jet-net.net"})
		if err := f.Reconnect(context.Background()); KindOf(err) != KindNeedsLink {
			t.Fatalf("%s: %v", name, err)
		}
		if d.calls[len(d.calls)-1] == "Up" {
			t.Fatalf("%s: Up без регистрации: %v", name, d.calls)
		}
	}
}

func TestReconnectNeedsLoginAndSessionExpiredNeedLink(t *testing.T) {
	for _, st := range []string{"NeedsLogin", "SessionExpired"} {
		d := &fakeDaemon{status: st}
		f, s := newFlow(t, d)
		_ = s.Save(Box{Identity: "x|y"})
		if err := f.Reconnect(context.Background()); KindOf(err) != KindNeedsLink {
			t.Fatalf("%s: %v", st, err)
		}
		if !reflect.DeepEqual(d.calls, []string{"Status"}) {
			t.Fatalf("%s: calls %v", st, d.calls)
		}
	}
}

// assertCancelled: отказ — *Error вида Unknown с отменой внутри, служба опущена
// не отменённым контекстом.
func assertCancelled(t *testing.T, d *fakeDaemon, err error) {
	t.Helper()
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindUnknown || !errors.Is(err, context.Canceled) {
		t.Fatalf("ожидалась отмена: %v", err)
	}
	if len(d.calls) == 0 || d.calls[len(d.calls)-1] != "Down" {
		t.Fatalf("служба не опущена после отмены: %v", d.calls)
	}
	if len(d.downCtx) != 1 || d.downCtx[0] != nil {
		t.Fatalf("Down с отменённым контекстом: %v", d.downCtx)
	}
}

func TestConnectCancelledDuringUpGoesDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	d.upHook = func(context.Context) error {
		cancel()
		return status.Error(codes.Canceled, "context canceled")
	}
	f, _ := newFlow(t, d)
	_, err := f.Connect(ctx, link(t, demoLink), Options{}, nil)
	assertCancelled(t, d, err)
}

func TestConnectCancelledWhileChoosingExitGoesDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &fakeDaemon{status: "NeedsLogin"} // выходов нет — ждём маршруты
	d.upHook = func(context.Context) error {
		time.AfterFunc(20*time.Millisecond, cancel)
		return nil
	}
	f, _ := newFlow(t, d)
	f.cfg.ExitWait = 10 * time.Second
	start := time.Now()
	_, err := f.Connect(ctx, link(t, demoLink), Options{}, nil)
	if time.Since(start) > 5*time.Second {
		t.Fatal("отмена не прервала ожидание выхода")
	}
	assertCancelled(t, d, err)
}

func TestDaemonCanceledCodeTreatedAsCancel(t *testing.T) {
	// служба сама отменила Up (Down из другого клиента и т. п.) — ctx жив
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets, upErr: status.Error(codes.Canceled, "context canceled")}
	f, _ := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	assertCancelled(t, d, err)
}

func TestReconnectCancelledDuringUpGoesDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &fakeDaemon{status: "Idle"}
	d.upHook = func(context.Context) error {
		cancel()
		return status.Error(codes.Canceled, "context canceled")
	}
	f, _ := newFlow(t, d)
	err := f.Reconnect(ctx)
	assertCancelled(t, d, err)
}
