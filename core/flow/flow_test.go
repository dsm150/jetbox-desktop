package flow

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/daemonclient"
	"github.com/dsm150/jetbox-desktop/core/exitpick"
	"github.com/dsm150/jetbox-desktop/core/jetboxlink"
)

const key = "0EF79C2F-1A34-4B21-9C0D-1BF529332998"

// fakeDaemon без своей блокировки — нарочно: гонку вызовов поймает -race,
// если Flow их не упорядочит.
type fakeDaemon struct {
	status        string
	statuses      []string // ответы Status по очереди (последний повторяется); пусто — status
	statusErr     error
	loginErr      error
	browser       bool
	upErr         error
	downErr       error
	logoutErr     error
	netsErr       error
	nets          []exitpick.Network
	deselectErr   error              // ответ DeselectExits
	afterDeselect []exitpick.Network // не nil — сети после DeselectExits
	mgmt          string             // ManagementState.URL в ответе Status (служба пишет его из профиля)
	calls         []string
	netsCalls     int
	loginKeys     []string                        // ключи, с которыми звали Login
	upHook        func(ctx context.Context) error // если задан — вместо upErr
	downCtx       []error                         // ctx.Err() на момент каждого Down
}

func (f *fakeDaemon) Status(context.Context) (*proto.StatusResponse, error) {
	f.calls = append(f.calls, "Status")
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	st := f.status
	if len(f.statuses) > 0 {
		st = f.statuses[0]
		if len(f.statuses) > 1 {
			f.statuses = f.statuses[1:]
		}
	}
	return &proto.StatusResponse{Status: st, FullStatus: &proto.FullStatus{
		ManagementState: &proto.ManagementState{URL: f.mgmt}}}, nil
}
func (f *fakeDaemon) Configure(_ context.Context, mgmt, iface string, port int64) error {
	f.calls = append(f.calls, "Configure "+mgmt+" "+iface)
	return nil
}
func (f *fakeDaemon) Login(_ context.Context, k, mgmt string) (bool, error) {
	f.calls = append(f.calls, "Login "+mgmt)
	f.loginKeys = append(f.loginKeys, k)
	return f.browser, f.loginErr
}
func (f *fakeDaemon) Up(ctx context.Context) error {
	f.calls = append(f.calls, "Up")
	if f.upHook != nil {
		return f.upHook(ctx)
	}
	return f.upErr
}
func (f *fakeDaemon) Down(ctx context.Context) error {
	f.calls = append(f.calls, "Down")
	f.downCtx = append(f.downCtx, ctx.Err())
	return f.downErr
}
func (f *fakeDaemon) Logout(context.Context) error {
	f.calls = append(f.calls, "Logout")
	return f.logoutErr
}
func (f *fakeDaemon) Networks(context.Context) ([]exitpick.Network, error) {
	f.netsCalls++
	if f.netsErr != nil {
		return nil, f.netsErr
	}
	return f.nets, nil
}
func (f *fakeDaemon) SelectExit(_ context.Context, id string) error {
	f.calls = append(f.calls, "SelectExit "+id)
	return nil
}
func (f *fakeDaemon) DeselectExits(_ context.Context, ids []string) error {
	f.calls = append(f.calls, "DeselectExits "+strings.Join(ids, ","))
	if f.afterDeselect != nil {
		f.nets = f.afterDeselect
	}
	return f.deselectErr
}

var demoNets = []exitpick.Network{{ID: "sv-demo-exit", Range: "0.0.0.0/0, ::/0"}}

func link(t *testing.T, raw string) jetboxlink.Payload {
	t.Helper()
	p, out := jetboxlink.Parse(raw)
	if out != jetboxlink.OK {
		t.Fatal(raw)
	}
	return p
}

func newFlow(t *testing.T, d *fakeDaemon) (*Flow, *Store) {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "box.json"))
	return New(d, s, Config{Iface: "JetBox", Port: 51830, ExitWait: 50 * time.Millisecond, ExitPoll: time.Millisecond}), s
}

const demoLink = "jetbox://connect/#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&e=sv-demo-exit&n=Demo"

func TestNewBoxHappyPath(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	f, s := newFlow(t, d)
	var stages []Stage
	res, err := f.Connect(context.Background(), link(t, demoLink), Options{}, func(st Stage) { stages = append(stages, st) })
	if err != nil || res.ExitID != "sv-demo-exit" || res.NeedsChoice {
		t.Fatal(res, err)
	}
	wantCalls := []string{"Status", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, wantCalls) {
		t.Fatalf("calls %v", d.calls)
	}
	if !reflect.DeepEqual(stages, []Stage{StageRegistering, StageConnecting, StageChoosingExit, StageProtected}) {
		t.Fatalf("stages %v", stages)
	}
	b, ok, _ := s.Load()
	if !ok || b.Identity != "netbird.jet-net.net|sv-demo-exit" || b.Name != "Demo" || b.ExitNode != "sv-demo-exit" {
		t.Fatalf("box %+v", b)
	}
}

func TestSameBoxRegisteredStillLogsIn(t *testing.T) {
	// служба Idle, но пир мог быть удалён на management: Login без ключа не тратит
	// ключ, если пир жив, и перерегистрирует, если нет. Configure не нужен.
	d := &fakeDaemon{status: "Idle", nets: demoNets}
	f, s := newFlow(t, d)
	regAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit", Name: "Старое имя", RegisteredAt: regAt})
	var stages []Stage
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, func(st Stage) { stages = append(stages, st) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}) {
		t.Fatalf("calls %v", d.calls)
	}
	if !reflect.DeepEqual(stages, []Stage{StageConnecting, StageChoosingExit, StageProtected}) {
		t.Fatalf("stages %v", stages)
	}
	b, _, _ := s.Load()
	if b.Name != "Demo" || !b.RegisteredAt.Equal(regAt) {
		t.Fatalf("box %+v", b)
	}
}

func TestSameBoxLoginRejected(t *testing.T) {
	d := &fakeDaemon{status: "Idle", nets: demoNets,
		loginErr: status.Error(codes.PermissionDenied, "couldn't add peer: setup key is invalid")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit"})
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if KindOf(err) != KindLinkExpired {
		t.Fatal(err)
	}
}

func TestSameBoxButNeedsLoginRegistersAgain(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit"})
	var stages []Stage
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, func(st Stage) { stages = append(stages, st) }); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if stages[0] != StageRegistering {
		t.Fatalf("stages %v", stages)
	}
	if b, _, _ := s.Load(); b.RegisteredAt.IsZero() {
		t.Fatal("RegisteredAt не обновлён после новой регистрации")
	}
}

func TestEmptyStoreButDaemonRegisteredIsForeign(t *testing.T) {
	// тот же management, но выхода из ссылки в сетях службы нет — регистрация чужая
	otherNets := []exitpick.Network{{ID: "other-exit", Range: "0.0.0.0/0"}}
	d := &fakeDaemon{status: "Connected", nets: otherNets, mgmt: "https://netbird.jet-net.net:443"}
	f, s := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
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

	d.calls = nil
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Down", "Logout", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up", "SelectExit other-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
}

func TestForeignLogoutFailsStops(t *testing.T) {
	// Idle: движок не запущен, ListNetworks отвечает «not connected» (server/network.go:36-38)
	d := &fakeDaemon{status: "Idle", netsErr: errors.New("not connected"), logoutErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, _ := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil)
	if KindOf(err) != KindServerUnreachable {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Down", "Logout"}) {
		t.Fatalf("calls %v", d.calls)
	}
}

func TestSwitchLogoutFailsSameManagementStops(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, logoutErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, s := newFlow(t, d)
	old := Box{Identity: "netbird.jet-net.net|other-exit", Name: "Старая", ManagementURL: "https://netbird.jet-net.net"}
	_ = s.Save(old)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil)
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindServerUnreachable || fe.Detail != "не снять старую коробку" {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Down", "Logout"}) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, ok, _ := s.Load(); !ok || b != old {
		t.Fatalf("старая коробка потеряна: %+v %v", b, ok)
	}
}

func TestSwitchLogoutFailsOtherManagementContinues(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, logoutErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "old.example.com|x", ManagementURL: "https://old.example.com"})
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Down", "Logout", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); b.Identity != "netbird.jet-net.net|sv-demo-exit" {
		t.Fatalf("box %+v", b)
	}
}

func TestBoxSavedAfterLoginEvenIfUpFails(t *testing.T) {
	d := &fakeDaemon{statuses: []string{"NeedsLogin", "Idle"}, upErr: status.Error(codes.Unavailable, "connection refused")}
	f, s := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if KindOf(err) != KindServerUnreachable {
		t.Fatal(err)
	}
	b, ok, _ := s.Load()
	if !ok || b.Identity != "netbird.jet-net.net|sv-demo-exit" || b.RegisteredAt.IsZero() {
		t.Fatalf("коробка не запомнена после регистрации: %+v %v", b, ok)
	}
}

func TestUpErrorThenNeedsLoginIsLinkExpired(t *testing.T) {
	d := &fakeDaemon{statuses: []string{"NeedsLogin", "NeedsLogin"}, upErr: errors.New("up failed")}
	f, _ := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if KindOf(err) != KindLinkExpired {
		t.Fatal(err)
	}
}

func TestServerUnreachableButOfflineIsNoInternet(t *testing.T) {
	deadline := status.Error(codes.DeadlineExceeded, "context deadline exceeded")
	for _, online := range []bool{false, true} {
		want := KindServerUnreachable
		if !online {
			want = KindNoInternet
		}
		on := online
		// Login
		d := &fakeDaemon{status: "NeedsLogin", loginErr: deadline}
		f, _ := newFlow(t, d)
		f.cfg.Online = func(context.Context) bool { return on }
		if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); KindOf(err) != want {
			t.Fatalf("login online=%v: %v", online, err)
		}
		// Up
		d = &fakeDaemon{statuses: []string{"NeedsLogin", "Idle"}, upErr: deadline}
		f, _ = newFlow(t, d)
		f.cfg.Online = func(context.Context) bool { return on }
		if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); KindOf(err) != want {
			t.Fatalf("up online=%v: %v", online, err)
		}
	}
}

func TestNetworksAlwaysFailingIsError(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", netsErr: status.Error(codes.Internal, "no engine")}
	f, _ := newFlow(t, d)
	res, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindUnknown || fe.Detail != "список сетей" || res.NeedsChoice {
		t.Fatal(res, err)
	}
	if d.netsCalls < 2 {
		t.Fatalf("Networks спрошен %d раз", d.netsCalls)
	}
}

func TestPickedExitStored(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: []exitpick.Network{{ID: "real-exit", Range: "0.0.0.0/0"}}}
	f, s := newFlow(t, d)
	res, err := f.Connect(context.Background(), link(t, "jetbox://connect#k="+key+"&e=typo"), Options{}, nil)
	if err != nil || res.ExitID != "real-exit" {
		t.Fatal(res, err)
	}
	if b, _, _ := s.Load(); b.ExitNode != "real-exit" {
		t.Fatalf("box %+v", b)
	}
}

func TestOtherBoxNeedsConfirmation(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|other-exit"})
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if KindOf(err) != KindOtherBox {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status"}) {
		t.Fatalf("служба тронута без подтверждения: %v", d.calls)
	}
}

func TestOtherBoxConfirmedLogsOutFirst(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|other-exit"})
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Down", "Logout", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
}

func TestServiceMissing(t *testing.T) {
	d := &fakeDaemon{statusErr: status.Error(codes.Unavailable, "pipe not found")}
	f, _ := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if KindOf(err) != KindServiceMissing {
		t.Fatal(err)
	}
}

func TestExpiredKeyNotRemembered(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", loginErr: status.Error(codes.PermissionDenied, "invalid setup key")}
	f, s := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if KindOf(err) != KindLinkExpired {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load(); ok {
		t.Fatal("коробка запомнена после отказа")
	}
}

func TestBrowserLoginRefused(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", browser: true}
	f, _ := newFlow(t, d)
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if KindOf(err) != KindNeedsBrowserLogin {
		t.Fatal(err)
	}
}

func TestManyExitsNeedChoice(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: []exitpick.Network{
		{ID: "a-exit", Range: "0.0.0.0/0"}, {ID: "b-exit", Range: "0.0.0.0/0"}}}
	f, s := newFlow(t, d)
	res, err := f.Connect(context.Background(), link(t, "jetbox://connect#k="+key+"&e=typo"), Options{}, nil)
	if err != nil || !res.NeedsChoice || !reflect.DeepEqual(res.Candidates, []exitpick.Candidate{{ID: "a-exit"}, {ID: "b-exit"}}) {
		t.Fatal(res, err)
	}
	if err := f.ChooseExit(context.Background(), "b-exit"); err != nil {
		t.Fatal(err)
	}
	if d.calls[len(d.calls)-1] != "SelectExit b-exit" {
		t.Fatal(d.calls)
	}
	if b, _, _ := s.Load(); b.ExitNode != "b-exit" {
		t.Fatalf("выбор не запомнен: %+v", b)
	}
}

func TestNoExitsYetStillConnected(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin"}
	f, _ := newFlow(t, d)
	res, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if err != nil || !res.NeedsChoice || len(res.Candidates) != 0 {
		t.Fatal(res, err)
	}
}

func TestOtherVPNStopsUnlessIgnored(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	f, _ := newFlow(t, d)
	f.cfg.OtherVPN = func(context.Context) (string, bool) { return "NetBird", true }
	_, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindOtherVPN || fe.Detail != "NetBird" {
		t.Fatal(err)
	}
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{IgnoreOtherVPN: true}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestForgetLogsOutAndClears(t *testing.T) {
	d := &fakeDaemon{status: "Connected"}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y"})
	if err := f.Forget(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load(); ok || d.calls[len(d.calls)-1] != "Logout" {
		t.Fatal(d.calls)
	}
}

func TestForgetLogoutFailsStillForgets(t *testing.T) {
	d := &fakeDaemon{status: "Connected", logoutErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y"})
	err := f.Forget(context.Background())
	if KindOf(err) != KindServerUnreachable {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load(); ok {
		t.Fatal("коробка не забыта")
	}
}

func TestForgetLogoutPrivilegeErrorIsUnknown(t *testing.T) {
	d := &fakeDaemon{logoutErr: status.Error(codes.PermissionDenied, "not allowed")}
	f, _ := newFlow(t, d)
	if err := f.Forget(context.Background()); KindOf(err) != KindUnknown {
		t.Fatal(err)
	}
}

func TestReconnectNeedsLink(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin"}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "x|y"})
	if err := f.Reconnect(context.Background()); KindOf(err) != KindNeedsLink {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status"}) {
		t.Fatalf("calls %v", d.calls)
	}
}

func TestReconnectUpErrorClassified(t *testing.T) {
	d := &fakeDaemon{status: "Idle", upErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, _ := newFlow(t, d)
	f.cfg.Online = func(context.Context) bool { return false }
	err := f.Reconnect(context.Background())
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindNoInternet {
		t.Fatal(err)
	}
	if err := f.Reconnect(context.Background()); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	d.upErr = nil
	if err := f.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDisconnectErrorWrapped(t *testing.T) {
	d := &fakeDaemon{downErr: errors.New("boom")}
	f, _ := newFlow(t, d)
	var fe *Error
	if err := f.Disconnect(context.Background()); !errors.As(err, &fe) {
		t.Fatal(err)
	}
}

func TestConcurrentConnectDisconnectNoRace(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	f, _ := newFlow(t, d)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
		}()
		go func() {
			defer wg.Done()
			_ = f.Disconnect(context.Background())
			_ = f.Reconnect(context.Background())
			_ = f.ChooseExit(context.Background(), "sv-demo-exit")
		}()
	}
	wg.Wait()
}

var _ Daemon = (*daemonclient.Client)(nil)
