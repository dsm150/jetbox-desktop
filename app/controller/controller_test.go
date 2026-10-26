package controller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netbirdio/netbird/client/proto"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
	"github.com/dsm150/jetbox-desktop/core/flow"
	"github.com/dsm150/jetbox-desktop/core/jetboxlink"
)

const key = "0EF79C2F-1A34-4B21-9C0D-1BF529332998"
const demoLink = "jetbox://connect/#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&e=sv-demo-exit&n=Demo"
const otherLink = "jetbox://connect/#k=1EF79C2F-1A34-4B21-9C0D-1BF529332999&m=https%3A%2F%2Fnetbird.jet-net.net&e=other-exit&n=Other"

type fakeEngine struct {
	mu        sync.Mutex
	box       flow.Box
	hasBox    bool
	res       flow.Result
	err       error // Connect
	reconnErr error
	discErr   error
	chooseErr error
	exits     []exitpick.Candidate // ответ Exits
	exitsErr  error
	forgetErr error
	opts      []flow.Options
	links     []jetboxlink.Payload
	calls     []string
	chosen    string
	block     chan struct{} // не nil — вызовы службы ждут, пока его не закроют
	entered   chan string   // не nil — сигнал о входе в вызов
}

// enter отмечает вызов и, если надо, ждёт block (без блокировки f.mu).
func (f *fakeEngine) enter(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	blk, ent := f.block, f.entered
	f.mu.Unlock()
	if ent != nil {
		ent <- name
	}
	if blk != nil {
		<-blk
	}
}

func (f *fakeEngine) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func (f *fakeEngine) Connect(_ context.Context, l jetboxlink.Payload, o flow.Options, progress func(flow.Stage)) (flow.Result, error) {
	f.enter("Connect")
	progress(flow.StageRegistering)
	progress(flow.StageConnecting)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opts = append(f.opts, o)
	f.links = append(f.links, l)
	if f.err == nil {
		f.box, f.hasBox = flow.Box{Identity: l.BoxIdentity(), Name: l.DisplayName(), ExitNode: f.res.ExitID}, true
	}
	return f.res, f.err
}
func (f *fakeEngine) ChooseExit(_ context.Context, id string) error {
	f.enter("ChooseExit")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.chooseErr != nil {
		return f.chooseErr
	}
	f.chosen = id
	f.box.ExitNode = id // как core/flow: выбранный выход запоминается в коробке
	return nil
}
func (f *fakeEngine) ChooseNoExit(context.Context) error {
	f.enter("ChooseNoExit")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.chooseErr != nil {
		return f.chooseErr
	}
	f.box.ExitNode, f.box.NoExit = "", true
	return nil
}
func (f *fakeEngine) Exits(context.Context) ([]exitpick.Candidate, error) {
	f.enter("Exits")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exits, f.exitsErr
}
func (f *fakeEngine) Reconcile(context.Context) error {
	f.enter("Reconcile")
	return nil
}
func (f *fakeEngine) Disconnect(context.Context) error {
	f.enter("Disconnect")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.discErr
}
func (f *fakeEngine) Reconnect(context.Context) error {
	f.enter("Reconnect")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reconnErr
}
func (f *fakeEngine) Forget(context.Context) error {
	f.enter("Forget")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hasBox, f.box = false, flow.Box{} // core/flow забывает коробку всегда
	return f.forgetErr
}
func (f *fakeEngine) Box() (flow.Box, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.box, f.hasBox, nil
}

type fakeStatus struct {
	mu  sync.Mutex
	st  *proto.StatusResponse
	err error
	// nets — ответ ListNetworks; nil — один выход коробки, включён; пустой срез —
	// карта от management ещё не пришла (служба уже Connected, сетей нет).
	nets    []exitpick.Network
	netsErr error
}

func (f *fakeStatus) Status(context.Context) (*proto.StatusResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st, f.err
}

func (f *fakeStatus) Networks(context.Context) ([]exitpick.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.netsErr != nil {
		return nil, f.netsErr
	}
	if f.nets == nil {
		return []exitpick.Network{{ID: "sv-demo-exit", Range: "0.0.0.0/0, ::/0", Selected: true}}, nil
	}
	return f.nets, nil
}

func (f *fakeStatus) set(st *proto.StatusResponse) {
	f.mu.Lock()
	f.st = st
	f.mu.Unlock()
}

func (f *fakeStatus) setNets(nets []exitpick.Network) {
	f.mu.Lock()
	f.nets = nets
	f.mu.Unlock()
}

func connectedAt(since int64, relayed bool) *proto.StatusResponse {
	return &proto.StatusResponse{Status: "Connected", FullStatus: &proto.FullStatus{
		ManagementState: &proto.ManagementState{Connected: true}, Peers: []*proto.PeerState{{
			IP: "100.64.239.180", ConnStatus: "Connected", BytesRx: 10, BytesTx: 20, Relayed: relayed,
			ConnStatusUpdate: timestamppb.New(time.Unix(since, 0)), Networks: []string{"0.0.0.0/0"},
		}}}}
}

func connected() *proto.StatusResponse { return connectedAt(1793000000, false) }

func probe12(context.Context, string) (time.Duration, error) { return 12 * time.Millisecond, nil }

func newCtl(e *fakeEngine, s *fakeStatus) (*Controller, *[]State) {
	return newCtlOpts(e, s, Options{})
}

func newCtlOpts(e *fakeEngine, s *fakeStatus, o Options) (*Controller, *[]State) {
	var seen []State
	c := New(e, s, probe12, func(st State) { seen = append(seen, st) }, "ru", "1.0.0", o)
	return c, &seen
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("не дождались: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestInitWithoutBoxShowsNoBox(t *testing.T) {
	c, _ := newCtl(&fakeEngine{}, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.Init(context.Background())
	if st := c.State(); st.Screen != ScreenNoBox || st.Lang != "ru" || st.Version != "1.0.0" || !st.Panel.ServiceUp {
		t.Fatalf("%+v", st)
	}
}

func TestInitWithBoxShowsHomeAndPanel(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo", ExitNode: "sv-demo-exit"}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	st := c.State()
	if st.Screen != ScreenHome || st.BoxName != "Demo" || st.ExitName != "sv-demo-exit" || !st.Panel.Connected ||
		st.Panel.HomeMs != 12 || st.Panel.RxBytes != 10 || st.Panel.SinceUnix != 1793000000 ||
		st.Panel.ServiceState != "Connected" {
		t.Fatalf("%+v", st)
	}
}

func TestServiceDownShowsServiceMissing(t *testing.T) {
	c, _ := newCtl(&fakeEngine{}, &fakeStatus{err: errors.New("pipe not found")})
	c.Init(context.Background())
	if st := c.State(); st.Screen != ScreenServiceMissing || st.Panel.ServiceUp {
		t.Fatalf("%+v", st)
	}
}

func TestNewLinkShowsReadyWithMaskOnly(t *testing.T) {
	c, _ := newCtl(&fakeEngine{}, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.Init(context.Background())
	c.OnLink(context.Background(), demoLink)
	st := c.State()
	if st.Screen != ScreenReady || st.LinkBoxName != "Demo" || st.Host != "netbird.jet-net.net" || st.KeyMask != "••••••••…2998" {
		t.Fatalf("%+v", st)
	}
}

func TestBrokenLinkShowsError(t *testing.T) {
	c, _ := newCtl(&fakeEngine{}, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.OnLink(context.Background(), "jetbox://connect#k=not-a-uuid")
	if st := c.State(); st.Screen != ScreenError || st.ErrorKind != "LinkBroken" || st.CanRetry {
		t.Fatalf("%+v", st)
	}
}

func TestForeignLinkIgnored(t *testing.T) {
	c, _ := newCtl(&fakeEngine{}, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.Init(context.Background())
	c.OnLink(context.Background(), "https://example.com/connect#k="+key)
	if st := c.State(); st.Screen != ScreenNoBox {
		t.Fatalf("%+v", st)
	}
}

func TestConnectGoesHome(t *testing.T) {
	e := &fakeEngine{res: flow.Result{ExitID: "sv-demo-exit"}}
	c, seen := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	st := c.State()
	if st.Screen != ScreenHome || st.BoxName != "Demo" || st.ExitName != "sv-demo-exit" || st.Busy || st.KeyMask != "" {
		t.Fatalf("%+v", st)
	}
	var sawStage bool
	for _, s := range *seen {
		if s.Screen == ScreenProgress && s.Stage == "connecting" {
			sawStage = true
		}
	}
	if !sawStage {
		t.Fatal("не было экрана «Подключаем…»")
	}
}

func TestSameBoxLinkConnectsImmediately(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Identity: "netbird.jet-net.net|sv-demo-exit", Name: "Demo"}, hasBox: true,
		res: flow.Result{ExitID: "sv-demo-exit"}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	if len(e.calls) != 1 || e.calls[0] != "Connect" || c.State().Screen != ScreenHome {
		t.Fatalf("%v %+v", e.calls, c.State())
	}
}

func TestOtherBoxAsksThenSwitches(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Identity: "netbird.jet-net.net|old-exit", Name: "Старый"}, hasBox: true,
		res: flow.Result{ExitID: "sv-demo-exit"}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	st := c.State()
	if st.Screen != ScreenSwitch || st.BoxName != "Старый" || st.LinkBoxName != "Demo" || len(e.calls) != 0 {
		t.Fatalf("%+v %v", st, e.calls)
	}
	c.ConfirmSwitch(context.Background())
	if !e.opts[0].ConfirmSwitch || c.State().Screen != ScreenHome {
		t.Fatalf("%+v %+v", e.opts, c.State())
	}
}

func TestCancelSwitchGoesHome(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Identity: "x|old", Name: "Старый"}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	c.CancelSwitch()
	if st := c.State(); st.Screen != ScreenHome || st.LinkBoxName != "" {
		t.Fatalf("%+v", st)
	}
}

// Служба зарегистрирована на другую JetBox, имя неизвестно (хранилище пусто):
// экран switch с пустым BoxName и именем коробки из ссылки.
func TestUnknownOtherBoxShowsSwitchWithEmptyName(t *testing.T) {
	e := &fakeEngine{err: &flow.Error{Kind: flow.KindOtherBox, Detail: ""}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	st := c.State()
	if st.Screen != ScreenSwitch || st.BoxName != "" || st.LinkBoxName != "Demo" || st.Busy {
		t.Fatalf("%+v", st)
	}
}

func TestOtherBoxErrorFillsNames(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Identity: "netbird.jet-net.net|sv-demo-exit", Name: "Demo"}, hasBox: true,
		err: &flow.Error{Kind: flow.KindOtherBox, Detail: "Старая"}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink) // та же коробка → сразу Connect → OtherBox
	st := c.State()
	if st.Screen != ScreenSwitch || st.BoxName != "Demo" || st.LinkBoxName != "Demo" {
		t.Fatalf("%+v", st)
	}
}

func TestOtherVPNThenIgnore(t *testing.T) {
	e := &fakeEngine{err: &flow.Error{Kind: flow.KindOtherVPN, Detail: "NetBird"}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	if st := c.State(); st.Screen != ScreenOtherVPN || st.OtherVPN != "NetBird" {
		t.Fatalf("%+v", st)
	}
	e.err, e.res = nil, flow.Result{ExitID: "sv-demo-exit"}
	c.IgnoreOtherVPN(context.Background())
	if st := c.State(); !e.opts[1].IgnoreOtherVPN || st.Screen != ScreenHome || st.OtherVPN != "" {
		t.Fatalf("%+v %+v", e.opts, st)
	}
}

func TestExpiredLinkErrorNoRetry(t *testing.T) {
	e := &fakeEngine{err: &flow.Error{Kind: flow.KindLinkExpired, Detail: "регистрация"}}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	st := c.State()
	if st.Screen != ScreenError || st.ErrorKind != "LinkExpired" || st.CanRetry || st.ErrorOp != OpConnect {
		t.Fatalf("%+v", st)
	}
	c.Dismiss()
	if c.State().Screen != ScreenNoBox {
		t.Fatalf("%+v", c.State())
	}
}

func TestServerUnreachableCanRetryAndSuccessClearsError(t *testing.T) {
	e := &fakeEngine{err: &flow.Error{Kind: flow.KindServerUnreachable}}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	if st := c.State(); !st.CanRetry || st.ErrorKind != "ServerUnreachable" {
		t.Fatalf("%+v", st)
	}
	e.err, e.res = nil, flow.Result{ExitID: "sv-demo-exit"}
	c.Connect(context.Background())
	st := c.State()
	if st.Screen != ScreenHome || st.ErrorKind != "" || st.ErrorDetail != "" || st.CanRetry || st.ErrorOp != "" {
		t.Fatalf("%+v", st)
	}
}

// В подробностях ошибки не должно быть ключа из ссылки — только маска.
func TestErrorDetailScrubsSetupKey(t *testing.T) {
	for _, leaked := range []string{key, strings.ToLower(key)} {
		e := &fakeEngine{err: &flow.Error{Kind: flow.KindUnknown, Detail: "регистрация", Err: errors.New("bad key " + leaked)}}
		c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
		c.OnLink(context.Background(), demoLink)
		c.Connect(context.Background())
		st := c.State()
		if st.Screen != ScreenError || strings.Contains(strings.ToLower(st.ErrorDetail), strings.ToLower(key)) ||
			!strings.Contains(st.ErrorDetail, "…2998") {
			t.Fatalf("%q", st.ErrorDetail)
		}
	}
}

// Ссылка той же коробки → сразу Connect без экрана ready; при отказе «Закрыть»
// должно вернуть заполненный ready, а не пустой.
func TestDismissReturnsFilledReady(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Identity: "netbird.jet-net.net|sv-demo-exit", Name: "Demo"}, hasBox: true,
		err: &flow.Error{Kind: flow.KindServerUnreachable}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	if st := c.State(); st.Screen != ScreenError || !st.CanRetry {
		t.Fatalf("%+v", st)
	}
	c.Dismiss()
	st := c.State()
	if st.Screen != ScreenReady || st.Host != "netbird.jet-net.net" || st.KeyMask != "••••••••…2998" ||
		st.LinkBoxName != "Demo" || st.ErrorKind != "" {
		t.Fatalf("%+v", st)
	}
}

func TestChooseExitReloadsBox(t *testing.T) {
	e := &fakeEngine{res: flow.Result{NeedsChoice: true, Candidates: []exitpick.Candidate{{ID: "a"}, {ID: "b"}}}}
	s := &fakeStatus{st: connected()}
	c, _ := newCtl(e, s)
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	if st := c.State(); st.Screen != ScreenChooseExit || len(st.Candidates) != 2 {
		t.Fatalf("%+v", st)
	}
	c.ChooseExit(context.Background(), "b")
	if st := c.State(); e.chosen != "b" || st.Screen != ScreenHome || st.ExitName != "b" {
		t.Fatalf("%+v", st)
	}
	// После перезапуска приложения выход берётся из коробки.
	c2, _ := newCtl(e, s)
	c2.Init(context.Background())
	if st := c2.State(); st.ExitName != "b" {
		t.Fatalf("%+v", st)
	}
}

func TestChooseExitFailureNoRetry(t *testing.T) {
	e := &fakeEngine{res: flow.Result{NeedsChoice: true, Candidates: []exitpick.Candidate{{ID: "a"}, {ID: "b"}}},
		chooseErr: &flow.Error{Kind: flow.KindUnknown, Detail: "выбор выхода"}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	c.ChooseExit(context.Background(), "b")
	if st := c.State(); st.Screen != ScreenError || st.CanRetry || st.ErrorOp != OpChooseExit {
		t.Fatalf("%+v", st)
	}
}

func TestToggleDisconnectsAndReconnects(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	s := &fakeStatus{st: connected()}
	c, _ := newCtl(e, s)
	c.Init(context.Background())
	c.Toggle(context.Background())
	s.set(&proto.StatusResponse{Status: "Idle"})
	c.Refresh(context.Background())
	c.Toggle(context.Background())
	if len(e.calls) != 2 || e.calls[0] != "Disconnect" || e.calls[1] != "Reconnect" {
		t.Fatal(e.calls)
	}
}

// Щит решает по состоянию службы: Connecting (пир коробки ещё не Connected) —
// это «включено», нажатие выключает.
func TestToggleWhileServiceConnectingDisconnects(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "Connecting"}})
	c.Init(context.Background())
	if st := c.State(); st.Panel.Connected || st.Panel.ServiceState != "Connecting" {
		t.Fatalf("%+v", st.Panel)
	}
	c.Toggle(context.Background())
	if len(e.calls) != 1 || e.calls[0] != "Disconnect" {
		t.Fatal(e.calls)
	}
}

// Служба Connected, а пир коробки ещё нет — всё равно «включено».
func TestToggleServiceConnectedPeerNotDisconnects(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	st := connected()
	st.FullStatus.Peers[0].ConnStatus = "Connecting"
	c, _ := newCtl(e, &fakeStatus{st: st})
	c.Init(context.Background())
	c.Toggle(context.Background())
	if len(e.calls) != 1 || e.calls[0] != "Disconnect" {
		t.Fatal(e.calls)
	}
}

func TestToggleWithoutBoxDoesNothing(t *testing.T) {
	e := &fakeEngine{}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "Idle"}})
	c.Init(context.Background())
	c.Toggle(context.Background())
	if len(e.calls) != 0 {
		t.Fatal(e.calls)
	}
}

func TestNeedsLinkAfterReinstall(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "NeedsLogin"}})
	c.Init(context.Background())
	c.Toggle(context.Background())
	if st := c.State(); !st.Panel.NeedsLink || len(e.calls) != 0 {
		t.Fatalf("%+v %v", st, e.calls)
	}
}

func TestReconnectNeedsLinkStaysHome(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true,
		reconnErr: &flow.Error{Kind: flow.KindNeedsLink, Detail: "служба не зарегистрирована"}}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "Idle"}})
	c.Init(context.Background())
	c.Toggle(context.Background())
	if st := c.State(); st.Screen != ScreenHome || !st.Panel.NeedsLink || st.Busy || st.ErrorKind != "" {
		t.Fatalf("%+v", st)
	}
}

func TestReconnectFailureNoRetry(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true,
		reconnErr: &flow.Error{Kind: flow.KindServerUnreachable, Detail: "подключение"}}
	c, _ := newCtl(e, &fakeStatus{st: &proto.StatusResponse{Status: "Idle"}})
	c.Init(context.Background())
	c.Toggle(context.Background())
	st := c.State()
	if st.Screen != ScreenError || st.CanRetry || st.ErrorKind != "ServerUnreachable" || st.ErrorOp != OpReconnect || st.Busy {
		t.Fatalf("%+v", st)
	}
	c.Dismiss()
	if st := c.State(); st.Screen != ScreenHome || st.ErrorOp != "" {
		t.Fatalf("%+v", st)
	}
}

func TestDisconnectFailureNoRetry(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true,
		discErr: &flow.Error{Kind: flow.KindUnknown, Detail: "отключение"}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	c.Toggle(context.Background())
	if st := c.State(); st.Screen != ScreenError || st.CanRetry || st.ErrorOp != OpDisconnect || st.Busy {
		t.Fatalf("%+v", st)
	}
}

func otherVPNOpt(found *atomic.Bool) Options {
	return Options{OtherVPN: func(context.Context) (string, bool) { return "WireGuard", found.Load() }}
}

// Щит перед Reconnect проверяет чужой VPN; «Всё равно подключить» без ссылки — Reconnect.
func TestShieldChecksOtherVPNThenIgnore(t *testing.T) {
	var found atomic.Bool
	found.Store(true)
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c, _ := newCtlOpts(e, &fakeStatus{st: &proto.StatusResponse{Status: "Idle"}}, otherVPNOpt(&found))
	c.Init(context.Background())
	c.Toggle(context.Background())
	if st := c.State(); st.Screen != ScreenOtherVPN || st.OtherVPN != "WireGuard" || st.Busy || len(e.calls) != 0 {
		t.Fatalf("%+v %v", st, e.calls)
	}
	c.IgnoreOtherVPN(context.Background())
	if st := c.State(); st.Screen != ScreenHome || st.OtherVPN != "" || e.count("Reconnect") != 1 {
		t.Fatalf("%+v %v", st, e.calls)
	}
}

// «Проверить снова» без ссылки на экране other-vpn: проверка заново, чисто — Reconnect.
func TestShieldRecheckOtherVPN(t *testing.T) {
	var found atomic.Bool
	found.Store(true)
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c, _ := newCtlOpts(e, &fakeStatus{st: &proto.StatusResponse{Status: "Idle"}}, otherVPNOpt(&found))
	c.Init(context.Background())
	c.Toggle(context.Background())
	c.Recheck(context.Background())
	if st := c.State(); st.Screen != ScreenOtherVPN || len(e.calls) != 0 {
		t.Fatalf("%+v %v", st, e.calls)
	}
	found.Store(false)
	c.Recheck(context.Background())
	if st := c.State(); st.Screen != ScreenHome || e.count("Reconnect") != 1 {
		t.Fatalf("%+v %v", st, e.calls)
	}
}

func TestShieldNoOtherVPNReconnects(t *testing.T) {
	var found atomic.Bool
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c, _ := newCtlOpts(e, &fakeStatus{st: &proto.StatusResponse{Status: "Idle"}}, otherVPNOpt(&found))
	c.Init(context.Background())
	c.Toggle(context.Background())
	if e.count("Reconnect") != 1 {
		t.Fatal(e.calls)
	}
}

func TestForgetGoesNoBox(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	c.Forget(context.Background())
	if st := c.State(); st.Screen != ScreenNoBox || st.BoxName != "" {
		t.Fatalf("%+v", st)
	}
}

// Forget с ошибкой (нет сети) всё равно забывает коробку локально.
func TestForgetErrorStillForgetsBox(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo", ExitNode: "x"}, hasBox: true,
		forgetErr: &flow.Error{Kind: flow.KindNoInternet, Detail: "снятие с учёта"}}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	c.Forget(context.Background())
	st := c.State()
	if st.Screen != ScreenError || st.BoxName != "" || st.ExitName != "" || st.CanRetry || st.ErrorOp != OpForget ||
		st.Panel.Connected {
		t.Fatalf("%+v", st)
	}
	c.Dismiss()
	if st := c.State(); st.Screen != ScreenNoBox {
		t.Fatalf("%+v", st)
	}
}

func TestProbeThrottled(t *testing.T) {
	calls := 0
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c := New(e, &fakeStatus{st: connected()},
		func(context.Context, string) (time.Duration, error) { calls++; return time.Millisecond, nil },
		func(State) {}, "ru", "1.0.0")
	c.Init(context.Background())
	c.Refresh(context.Background())
	c.Refresh(context.Background())
	if calls != 1 {
		t.Fatalf("замер «до дома» %d раз, ждали 1", calls)
	}
}

// «В сети» считаем сами: смена relay↔direct у службы не сбрасывает время.
func TestOnlineSinceSurvivesRelaySwitch(t *testing.T) {
	var mu sync.Mutex
	now := time.Unix(1800000000, 0)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	s := &fakeStatus{st: &proto.StatusResponse{Status: "Idle"}}
	c, _ := newCtlOpts(e, s, Options{Now: clock})
	c.Init(context.Background())
	if st := c.State(); st.Panel.SinceUnix != 0 {
		t.Fatalf("%+v", st.Panel)
	}
	advance(time.Minute)
	s.set(connectedAt(1700000000, false))
	c.Refresh(context.Background())
	if st := c.State(); st.Panel.SinceUnix != 1800000060 {
		t.Fatalf("включились: %+v", st.Panel)
	}
	advance(time.Minute)
	s.set(connectedAt(1800000100, true)) // служба сбросила время при переходе на ретранслятор
	c.Refresh(context.Background())
	if st := c.State(); st.Panel.SinceUnix != 1800000060 || !st.Panel.Relayed {
		t.Fatalf("relay: %+v", st.Panel)
	}
	advance(time.Minute)
	s.set(&proto.StatusResponse{Status: "Idle"})
	c.Refresh(context.Background())
	if st := c.State(); st.Panel.SinceUnix != 0 {
		t.Fatalf("выключились: %+v", st.Panel)
	}
	advance(time.Minute)
	s.set(connectedAt(1800000100, false))
	c.Refresh(context.Background())
	if st := c.State(); st.Panel.SinceUnix != 1800000240 {
		t.Fatalf("снова включились: %+v", st.Panel)
	}
}

// Два быстрых «Подключить» — один вызов службы.
func TestConnectDoubleClickOneEngineCall(t *testing.T) {
	e := &fakeEngine{res: flow.Result{ExitID: "sv-demo-exit"}, block: make(chan struct{})}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	const n = 30
	var wg sync.WaitGroup
	var returned atomic.Int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c.Connect(context.Background())
			returned.Add(1)
		}()
	}
	close(start)
	waitFor(t, "лишние нажатия вернулись", func() bool { return returned.Load() == n-1 })
	if k := e.count("Connect"); k != 1 {
		t.Fatalf("Connect вызван %d раз", k)
	}
	close(e.block)
	wg.Wait()
}

// Два быстрых нажатия на щит — один вызов службы.
func TestToggleDoubleClickOneEngineCall(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	e.mu.Lock()
	e.block = make(chan struct{})
	e.mu.Unlock()
	const n = 30
	var wg sync.WaitGroup
	var returned atomic.Int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c.Toggle(context.Background())
			returned.Add(1)
		}()
	}
	close(start)
	waitFor(t, "лишние нажатия вернулись", func() bool { return returned.Load() == n-1 })
	if k := e.count("Disconnect") + e.count("Reconnect"); k != 1 {
		t.Fatalf("служба вызвана %d раз: %v", k, e.calls)
	}
	e.mu.Lock()
	close(e.block)
	e.mu.Unlock()
	wg.Wait()
}

// Ссылка во время подключения не подменяет текущую, а обрабатывается после.
func TestLinkDuringConnectIsQueued(t *testing.T) {
	e := &fakeEngine{res: flow.Result{ExitID: "sv-demo-exit"}, block: make(chan struct{}), entered: make(chan string, 10)}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	done := make(chan struct{})
	go func() { c.Connect(context.Background()); close(done) }()
	<-e.entered
	c.OnLink(context.Background(), otherLink)
	if st := c.State(); st.Screen != ScreenProgress || st.LinkBoxName != "Demo" || !st.Busy {
		t.Fatalf("ссылка подменила подключение: %+v", st)
	}
	close(e.block)
	<-done
	e.mu.Lock()
	links := append([]jetboxlink.Payload(nil), e.links...)
	e.mu.Unlock()
	if len(links) != 1 || links[0].DisplayName() != "Demo" {
		t.Fatalf("%+v", links)
	}
	st := c.State()
	if st.Screen != ScreenSwitch || st.BoxName != "Demo" || st.LinkBoxName != "Other" {
		t.Fatalf("отложенная ссылка не обработана: %+v", st)
	}
}

// Параллельные изменения доставляются строго по возрастанию seq, последнее — доставлено.
func TestOnChangeNeverGoesBack(t *testing.T) {
	var last uint64
	var bad atomic.Bool
	e := &fakeEngine{box: flow.Box{Name: "Demo"}, hasBox: true}
	c := New(e, &fakeStatus{st: connected()}, probe12, func(st State) {
		if st.Seq <= last {
			bad.Store(true)
		}
		last = st.Seq
	}, "ru", "1.0.0")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				c.SkipExit()
			}
		}()
	}
	wg.Wait()
	if bad.Load() {
		t.Fatal("доставлено старое состояние после нового")
	}
	if st := c.State(); st.Seq != last || st.Seq < 1000 {
		t.Fatalf("последнее состояние %d, доставлено %d", st.Seq, last)
	}
}

// Та же ссылка, открытая повторно во время её же подключения, второй раз не подключает.
func TestSameLinkDuringConnectIgnored(t *testing.T) {
	e := &fakeEngine{res: flow.Result{ExitID: "sv-demo-exit"}, block: make(chan struct{}), entered: make(chan string, 10)}
	c, _ := newCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	done := make(chan struct{})
	go func() { c.Connect(context.Background()); close(done) }()
	<-e.entered
	c.OnLink(context.Background(), demoLink)
	close(e.block)
	<-done
	if k := e.count("Connect"); k != 1 || c.State().Screen != ScreenHome {
		t.Fatalf("Connect %d раз, %+v", k, c.State())
	}
}

// Служба занята (смена сети, переподключение сотни пиров) и не уложилась в тайм-аут
// опроса — экран не прыгает в «служба не отвечает»: только после трёх таких опросов
// подряд. Сокета нет (Unavailable) — сразу.
func TestStatusTimeoutKeepsScreen(t *testing.T) {
	e := &fakeEngine{hasBox: true, box: flow.Box{Name: "Demo"}}
	s := &fakeStatus{st: connected()}
	c, _ := newCtl(e, s)
	c.Init(context.Background())
	if st := c.State(); st.Screen != ScreenHome || !st.Panel.Connected {
		t.Fatalf("init: %+v", st)
	}
	s.mu.Lock()
	s.err = grpcstatus.Error(codes.DeadlineExceeded, "context deadline exceeded")
	s.mu.Unlock()
	for i := 1; i <= 2; i++ {
		c.Refresh(context.Background())
		if st := c.State(); st.Screen != ScreenHome || !st.Panel.Connected {
			t.Fatalf("тайм-аут %d: экран %s, connected=%v", i, st.Screen, st.Panel.Connected)
		}
	}
	c.Refresh(context.Background())
	if st := c.State(); st.Screen != ScreenServiceMissing {
		t.Fatalf("третий тайм-аут подряд: экран %s", st.Screen)
	}

	s.mu.Lock()
	s.err = nil
	s.mu.Unlock()
	c.Refresh(context.Background())
	if st := c.State(); st.Screen != ScreenHome {
		t.Fatalf("служба ответила: экран %s", st.Screen)
	}
	s.mu.Lock()
	s.err = grpcstatus.Error(codes.Unavailable, "connection error: dial unix /var/run/jetbox.sock: connect: no such file or directory")
	s.mu.Unlock()
	c.Refresh(context.Background())
	if st := c.State(); st.Screen != ScreenServiceMissing {
		t.Fatalf("сокета нет: экран %s", st.Screen)
	}
}
