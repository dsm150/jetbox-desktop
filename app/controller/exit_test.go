package controller

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/netbirdio/netbird/client/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
	"github.com/dsm150/jetbox-desktop/core/flow"
)

// Ссылка аккаунта ROOT — без e: 8 выходов и сети клиентов.
const rootLink = "jetbox://connect#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&n=ROOT"

var rootExits = []string{"exit-1", "exit-2", "exit-3", "exit-4", "exit-5", "exit-6", "exit-7", "exit-8"}

// rootNets — ListNetworks: выходы rootExits (включён sel; "" — ни один) и сети клиентов.
func rootNets(sel string) []exitpick.Network {
	nets := []exitpick.Network{{ID: "client-lan", Range: "10.1.0.0/24", Selected: true}}
	for _, id := range rootExits {
		nets = append(nets, exitpick.Network{ID: id, Range: "0.0.0.0/0, ::/0", Selected: id == sel})
	}
	return nets
}

// rootStatus — служба и management на связи, пир клиента; exitIP != "" — пир выхода с 0/0.
func rootStatus(exitIP, conn string, latency time.Duration) *proto.StatusResponse {
	peers := []*proto.PeerState{{IP: "100.64.1.1", ConnStatus: "Connected", Networks: []string{"10.1.0.0/24"}}}
	if exitIP != "" {
		peers = append(peers, &proto.PeerState{IP: exitIP, ConnStatus: conn, Networks: []string{"0.0.0.0/0", "::/0"},
			Latency: durationpb.New(latency)})
	}
	return &proto.StatusResponse{Status: "Connected", FullStatus: &proto.FullStatus{
		ManagementState: &proto.ManagementState{Connected: true}, Peers: peers}}
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// probeRec — «до дома»: запоминает адреса; fail — проба не отвечает.
type probeRec struct {
	mu   sync.Mutex
	ips  []string
	fail bool
}

func (p *probeRec) probe(_ context.Context, ip string) (time.Duration, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ips = append(p.ips, ip)
	if p.fail {
		return 0, context.DeadlineExceeded
	}
	return 7 * time.Millisecond, nil
}

func newExitCtl(e *fakeEngine, s *fakeStatus) (*Controller, *testClock, *probeRec) {
	clk := &testClock{now: time.Unix(1800000000, 0)}
	p := &probeRec{}
	c := New(e, s, p.probe, func(State) {}, "ru", "1.0.0", Options{Now: clk.Now})
	return c, clk, p
}

// ROOT без e, ядро включило exit-3, а он офлайн: 15 с «подключаем», затем «выход не отвечает»
// (не вечное «Подключаем…»). «Сменить» → exit-5 → «Под защитой», «до дома» — к пиру exit-5.
func TestRootExitOfflineThenChangeExit(t *testing.T) {
	cands := exitpick.Exits(rootNets("exit-3"))
	e := &fakeEngine{res: flow.Result{NeedsChoice: true, Candidates: cands}, exits: cands}
	s := &fakeStatus{st: rootStatus("", "", 0), nets: rootNets("exit-3")}
	c, clk, p := newExitCtl(e, s)
	c.OnLink(context.Background(), rootLink)
	c.Connect(context.Background())
	st := c.State()
	if st.Screen != ScreenChooseExit || len(st.Candidates) != 8 || !st.Candidates[2].Active || st.Candidates[0].Active {
		t.Fatalf("%+v", st)
	}
	c.SkipExit()

	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.Connected || pn.ExitDown || pn.Exit != "exit-3" || !pn.CanChangeExit || pn.HomeMs != -1 {
		t.Fatalf("сразу: %+v", pn)
	}
	clk.advance(14 * time.Second)
	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.ExitDown {
		t.Fatalf("14 с: %+v", pn)
	}
	clk.advance(2 * time.Second)
	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.Connected || !pn.ExitDown || pn.Exit != "exit-3" || pn.ServiceState != "Connected" {
		t.Fatalf("16 с: %+v", pn)
	}
	if len(p.ips) != 0 {
		t.Fatalf("мерили до дома без выхода: %v", p.ips)
	}

	// щит в «выход не отвечает» выключает защиту; служба опущена
	c.Toggle(context.Background())
	if e.count("Disconnect") != 1 {
		t.Fatal(e.calls)
	}
	s.set(&proto.StatusResponse{Status: "Idle"})
	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.ExitDown || pn.Connected || pn.ServiceState != "Idle" {
		t.Fatalf("выключено: %+v", pn)
	}
	// включили снова — служба Connected, exit-3 по-прежнему офлайн
	s.set(rootStatus("", "", 0))
	c.Toggle(context.Background())
	if e.count("Reconnect") != 1 || c.State().Panel.ExitDown {
		t.Fatalf("%v %+v", e.calls, c.State().Panel)
	}

	c.ChangeExit(context.Background())
	st = c.State()
	if st.Screen != ScreenChooseExit || e.count("Exits") != 1 || !st.Candidates[2].Active {
		t.Fatalf("%+v", st)
	}
	s.setNets(rootNets("exit-5"))
	s.set(rootStatus("100.64.0.50", "Connected", 0))
	c.ChooseExit(context.Background(), "exit-5")
	st = c.State()
	if st.Screen != ScreenHome || e.chosen != "exit-5" || st.ExitName != "exit-5" {
		t.Fatalf("%+v", st)
	}
	if pn := st.Panel; !pn.Connected || pn.ExitDown || pn.Exit != "exit-5" || pn.HomeMs != 7 {
		t.Fatalf("%+v", pn)
	}
	if !reflect.DeepEqual(p.ips, []string{"100.64.0.50"}) {
		t.Fatalf("до дома мерили к %v", p.ips)
	}
}

// Сменили выход, а служба давно Connected: новый пир ждём заново 15 с, а не сразу «не отвечает».
func TestExitChangeRestartsGrace(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT"}, hasBox: true}
	s := &fakeStatus{st: rootStatus("100.64.0.10", "Connected", 0), nets: rootNets("exit-1")}
	c, clk, _ := newExitCtl(e, s)
	c.Init(context.Background())
	clk.advance(time.Minute)
	s.setNets(rootNets("exit-2"))
	s.set(rootStatus("", "", 0))
	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.ExitDown || pn.Connected {
		t.Fatalf("%+v", pn)
	}
	clk.advance(16 * time.Second)
	c.Refresh(context.Background())
	if pn := c.State().Panel; !pn.ExitDown || pn.Exit != "exit-2" {
		t.Fatalf("%+v", pn)
	}
}

// «Без выхода»: ChooseNoExit в ядре, «Под защитой» без пира выхода, «до дома» скрыт (-1).
func TestNoExitProtectedWithoutHome(t *testing.T) {
	cands := exitpick.Exits(rootNets("exit-1"))
	e := &fakeEngine{res: flow.Result{NeedsChoice: true, Candidates: cands}}
	s := &fakeStatus{st: rootStatus("", "", 0), nets: rootNets("exit-1")}
	c, clk, p := newExitCtl(e, s)
	c.OnLink(context.Background(), rootLink)
	c.Connect(context.Background())
	s.setNets(rootNets(""))
	c.NoExit(context.Background())
	st := c.State()
	if e.count("ChooseNoExit") != 1 || st.Screen != ScreenHome || st.ExitName != "" || st.Busy {
		t.Fatalf("%+v %v", st, e.calls)
	}
	if pn := st.Panel; !pn.Connected || !pn.NoExit || pn.Exit != "" || pn.ExitDown || pn.HomeMs != -1 || !pn.CanChangeExit {
		t.Fatalf("%+v", pn)
	}
	clk.advance(time.Minute)
	c.Refresh(context.Background())
	if pn := c.State().Panel; !pn.Connected || pn.ExitDown || pn.HomeMs != -1 {
		t.Fatalf("%+v", pn)
	}
	if len(p.ips) != 0 {
		t.Fatalf("мерили до дома: %v", p.ips)
	}
}

// Проба DNS не ответила — «до дома» по задержке WireGuard из статуса; нет и её — -1.
func TestHomeFallsBackToWireGuardLatency(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT"}, hasBox: true}
	s := &fakeStatus{st: rootStatus("100.64.0.10", "Connected", 23*time.Millisecond), nets: rootNets("exit-1")}
	c, clk, p := newExitCtl(e, s)
	p.fail = true
	c.Init(context.Background())
	if pn := c.State().Panel; pn.HomeMs != 23 || !pn.Connected {
		t.Fatalf("%+v", pn)
	}
	clk.advance(probeEvery)
	s.set(rootStatus("100.64.0.10", "Connected", 0))
	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.HomeMs != -1 {
		t.Fatalf("%+v", pn)
	}
}

// Клиентская ссылка с e (один выход — коробка): экраны и щит как раньше; «Сменить» не нужен.
// Сразу после Up служба и management уже Connected, а карты ещё нет (ListNetworks — []):
// это «Подключаем…», а не «Под защитой без выхода».
func TestClientLinkUnchanged(t *testing.T) {
	e := &fakeEngine{res: flow.Result{ExitID: "sv-demo-exit"}}
	s := &fakeStatus{st: rootStatus("", "", 0), nets: []exitpick.Network{}}
	c, clk, p := newExitCtl(e, s)
	c.OnLink(context.Background(), demoLink)
	c.Connect(context.Background())
	got := c.State()
	if got.Screen != ScreenHome || got.ExitName != "sv-demo-exit" || got.Panel.Connected || got.Panel.ExitDown ||
		got.Panel.CanChangeExit || got.Panel.NoExit || got.Panel.ServiceState != "Connected" {
		t.Fatalf("карты ещё нет: %+v", got)
	}
	clk.advance(2 * time.Second)
	st := connected()
	st.FullStatus.Peers[0].ConnStatus = "Connecting"
	s.set(st)
	s.setNets(nil)
	c.Refresh(context.Background())
	got = c.State()
	if got.Panel.Connected || got.Panel.ExitDown || got.Panel.Exit != "sv-demo-exit" || got.Panel.NoExit {
		t.Fatalf("карта пришла, пир ещё нет: %+v", got.Panel)
	}
	clk.advance(3 * time.Second)
	s.set(connected())
	c.Refresh(context.Background())
	got = c.State()
	if !got.Panel.Connected || got.Panel.HomeMs != 7 || got.Panel.ExitDown || got.Panel.SinceUnix == 0 {
		t.Fatalf("%+v", got.Panel)
	}
	if !reflect.DeepEqual(p.ips, []string{"100.64.239.180"}) {
		t.Fatal(p.ips)
	}
}

// Карты нет, а в коробке выход — не под защитой; через 15 с «выход не отвечает».
func TestMapPendingWithBoxExit(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "Demo", ExitNode: "sv-demo-exit"}, hasBox: true}
	s := &fakeStatus{st: rootStatus("", "", 0), nets: []exitpick.Network{}}
	c, clk, _ := newExitCtl(e, s)
	c.Init(context.Background())
	if pn := c.State().Panel; pn.Connected || pn.ExitDown || pn.NoExit {
		t.Fatalf("сразу: %+v", pn)
	}
	clk.advance(16 * time.Second)
	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.Connected || !pn.ExitDown || pn.NoExit {
		t.Fatalf("16 с: %+v", pn)
	}
}

// ListNetworks при Connected отвечает ошибкой: через 15 с без выхода в коробке —
// «без выхода» (под защитой), с выходом в коробке — «выход не отвечает».
func TestNetworksErrorAfterGrace(t *testing.T) {
	for _, exit := range []string{"", "exit-1"} {
		e := &fakeEngine{box: flow.Box{Name: "ROOT", ExitNode: exit}, hasBox: true}
		s := &fakeStatus{st: rootStatus("", "", 0), netsErr: context.DeadlineExceeded}
		c, clk, _ := newExitCtl(e, s)
		c.Init(context.Background())
		if pn := c.State().Panel; pn.Connected || pn.ExitDown {
			t.Fatalf("%q сразу: %+v", exit, pn)
		}
		clk.advance(16 * time.Second)
		c.Refresh(context.Background())
		pn := c.State().Panel
		if exit == "" && (!pn.Connected || !pn.NoExit || pn.ExitDown || pn.SinceUnix == 0) {
			t.Fatalf("без выхода в коробке: %+v", pn)
		}
		if exit != "" && (pn.Connected || pn.NoExit || !pn.ExitDown) {
			t.Fatalf("с выходом в коробке: %+v", pn)
		}
	}
}

// «Без выхода» в коробке — под защитой сразу, даже если карта ещё не пришла.
func TestBoxNoExitProtectedImmediately(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT", NoExit: true}, hasBox: true}
	s := &fakeStatus{st: rootStatus("", "", 0), nets: []exitpick.Network{}}
	c, _, _ := newExitCtl(e, s)
	c.Init(context.Background())
	st := c.State()
	if !st.NoExit || !st.Panel.Connected || !st.Panel.NoExit || st.Panel.HomeMs != -1 {
		t.Fatalf("%+v", st)
	}
}

// Выход был поднят и упал: 15 с считаются от падения, а не от включения службы.
func TestExitFellRestartsGrace(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT", ExitNode: "exit-1"}, hasBox: true}
	s := &fakeStatus{st: rootStatus("100.64.0.10", "Connected", 0), nets: rootNets("exit-1")}
	c, clk, _ := newExitCtl(e, s)
	c.Init(context.Background())
	clk.advance(time.Minute)
	c.Refresh(context.Background())
	if pn := c.State().Panel; !pn.Connected {
		t.Fatalf("%+v", pn)
	}
	clk.advance(time.Minute)
	s.set(rootStatus("", "", 0)) // пир выхода пропал, выход по-прежнему выбран
	c.Refresh(context.Background())
	clk.advance(14 * time.Second)
	c.Refresh(context.Background())
	if pn := c.State().Panel; pn.ExitDown || pn.Connected {
		t.Fatalf("14 с после падения: %+v", pn)
	}
	clk.advance(2 * time.Second)
	c.Refresh(context.Background())
	if pn := c.State().Panel; !pn.ExitDown {
		t.Fatalf("16 с после падения: %+v", pn)
	}
}

// «Сменить» → «Пока не выбирать» — домой без изменений в службе.
func TestChangeExitSkipChangesNothing(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT", ExitNode: "exit-1"}, hasBox: true, exits: exitpick.Exits(rootNets("exit-1"))}
	s := &fakeStatus{st: rootStatus("100.64.0.10", "Connected", 0), nets: rootNets("exit-1")}
	c, _, _ := newExitCtl(e, s)
	c.Init(context.Background())
	c.ChangeExit(context.Background())
	if st := c.State(); st.Screen != ScreenChooseExit || st.Busy || !st.Candidates[0].Active {
		t.Fatalf("%+v", st)
	}
	c.SkipExit()
	if st := c.State(); st.Screen != ScreenHome || st.ExitName != "exit-1" || e.count("ChooseExit")+e.count("ChooseNoExit") != 0 {
		t.Fatalf("%+v %v", st, e.calls)
	}
}

// Список выходов не получен — ошибка выбора выхода, а не пустой экран.
func TestChangeExitListFails(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT"}, hasBox: true, exitsErr: &flow.Error{Kind: flow.KindUnknown, Detail: "список сетей"}}
	c, _, _ := newExitCtl(e, &fakeStatus{st: connected()})
	c.Init(context.Background())
	c.ChangeExit(context.Background())
	if st := c.State(); st.Screen != ScreenError || st.ErrorOp != OpChooseExit || st.Busy {
		t.Fatalf("%+v", st)
	}
}

// Опрос сверяет ядро с коробкой (не чаще reconcileEvery и только когда карта пришла).
func TestRefreshReconcilesThrottled(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT", NoExit: true}, hasBox: true}
	s := &fakeStatus{st: rootStatus("", "", 0), nets: []exitpick.Network{}}
	c, clk, _ := newExitCtl(e, s)
	c.Init(context.Background())
	if e.count("Reconcile") != 0 {
		t.Fatal("сверка без карты", e.calls)
	}
	s.setNets(rootNets("exit-1")) // ядро само включило выход
	c.Refresh(context.Background())
	c.Refresh(context.Background())
	if e.count("Reconcile") != 1 {
		t.Fatal(e.calls)
	}
	clk.advance(reconcileEvery)
	c.Refresh(context.Background())
	if e.count("Reconcile") != 2 {
		t.Fatal(e.calls)
	}
	// совпадает с коробкой — сверять нечего
	s.setNets(rootNets(""))
	clk.advance(reconcileEvery)
	c.Refresh(context.Background())
	if e.count("Reconcile") != 2 {
		t.Fatal(e.calls)
	}
}

// Запомненный выход есть в списке, а ядро включило другой — сверка; запомненного нет в списке — нет.
func TestRefreshReconcilesRememberedExit(t *testing.T) {
	e := &fakeEngine{box: flow.Box{Name: "ROOT", ExitNode: "exit-5"}, hasBox: true}
	s := &fakeStatus{st: rootStatus("100.64.0.10", "Connected", 0), nets: rootNets("exit-1")}
	c, clk, _ := newExitCtl(e, s)
	c.Init(context.Background())
	if e.count("Reconcile") != 1 {
		t.Fatal(e.calls)
	}
	e.mu.Lock()
	e.box.ExitNode = "gone"
	e.mu.Unlock()
	clk.advance(reconcileEvery)
	c.Refresh(context.Background())
	if e.count("Reconcile") != 1 {
		t.Fatal(e.calls)
	}
}

// «Пока не выбирать» во время действия ничего не делает.
func TestSkipExitIgnoredWhileBusy(t *testing.T) {
	e := &fakeEngine{res: flow.Result{ExitID: "sv-demo-exit"}, block: make(chan struct{}), entered: make(chan string, 4)}
	c, _, _ := newExitCtl(e, &fakeStatus{st: connected()})
	c.OnLink(context.Background(), demoLink)
	done := make(chan struct{})
	go func() { c.Connect(context.Background()); close(done) }()
	<-e.entered
	c.SkipExit()
	if st := c.State(); st.Screen != ScreenProgress || !st.Busy {
		t.Fatalf("%+v", st)
	}
	close(e.block)
	<-done
}
