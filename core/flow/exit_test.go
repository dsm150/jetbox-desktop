package flow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
)

// Ссылка аккаунта ROOT — без e: выходов много, их выбирает человек.
const rootLink = "jetbox://connect#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&n=ROOT"

const allExits = "exit-1,exit-2,exit-3,exit-4,exit-5,exit-6,exit-7,exit-8"

// rootNets — 8 выходов и сети клиентов; ядро само включило sel (наименьший NetID и т. п.).
func rootNets(sel string) []exitpick.Network { return exitNets(sel) }

func TestRootLinkAsksWithActiveExitMarked(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: rootNets("exit-1")}
	f, _ := newFlow(t, d)
	res, err := f.Connect(context.Background(), link(t, rootLink), Options{}, nil)
	if err != nil || !res.NeedsChoice || len(res.Candidates) != 8 {
		t.Fatal(res, err)
	}
	if res.Candidates[0] != (exitpick.Candidate{ID: "exit-1", Selected: true}) || res.Candidates[1].Selected {
		t.Fatalf("%+v", res.Candidates)
	}
}

// «Без выхода»: DeselectNetworks ровно с ID выходов (сети клиентов не трогаем, не all),
// выбор запомнен и при следующем подключении применяется молча.
func TestChooseNoExitRememberedAndAppliedSilently(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: rootNets("exit-1")}
	f, s := newFlow(t, d)
	if _, err := f.Connect(context.Background(), link(t, rootLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	d.calls = nil
	if err := f.ChooseNoExit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"DeselectExits " + allExits}) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); !b.NoExit || b.ExitNode != "" {
		t.Fatalf("box %+v", b)
	}

	// переподключение той же ссылкой: ядро снова включило выход — снимаем молча
	d.status, d.calls = "Idle", nil
	var stages []Stage
	res, err := f.Connect(context.Background(), link(t, rootLink), Options{}, func(st Stage) { stages = append(stages, st) })
	if err != nil || res.NeedsChoice || !res.NoExit || res.ExitID != "" {
		t.Fatal(res, err)
	}
	want := []string{"Status", "Login https://netbird.jet-net.net", "Up", "DeselectExits " + allExits}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if stages[len(stages)-1] != StageProtected {
		t.Fatalf("stages %v", stages)
	}
	if b, _, _ := s.Load(); !b.NoExit {
		t.Fatalf("box %+v", b)
	}
}

// Выбор выхода сбрасывает «без выхода»; запомненный выход применяется молча.
func TestChooseExitClearsNoExitAndIsAppliedSilently(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: rootNets("exit-1")}
	f, s := newFlow(t, d)
	if _, err := f.Connect(context.Background(), link(t, rootLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.ChooseNoExit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.ChooseExit(context.Background(), "exit-5"); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); b.NoExit || b.ExitNode != "exit-5" {
		t.Fatalf("box %+v", b)
	}
	d.status, d.calls = "Idle", nil
	res, err := f.Connect(context.Background(), link(t, rootLink), Options{}, nil)
	if err != nil || res.NeedsChoice || res.ExitID != "exit-5" {
		t.Fatal(res, err)
	}
	if d.calls[len(d.calls)-1] != "SelectExit exit-5" {
		t.Fatalf("calls %v", d.calls)
	}
}

// Выходов ещё нет — снимать нечего, DeselectNetworks не зовём (и не all).
func TestChooseNoExitWithoutExits(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: []exitpick.Network{{ID: "lan", Range: "10.0.0.0/8"}}}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|"})
	if err := f.ChooseNoExit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 0 {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); !b.NoExit {
		t.Fatalf("box %+v", b)
	}
}

// Список для «Сменить»: выходы с пометкой, какой включён ядром сейчас.
func TestExitsListsActive(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: rootNets("exit-4")}
	f, _ := newFlow(t, d)
	got, err := f.Exits(context.Background())
	if err != nil || len(got) != 8 || !got[3].Selected || got[3].ID != "exit-4" || got[0].Selected {
		t.Fatal(got, err)
	}
}

// Клиентская ссылка с e: запомненное «без выхода» не мешает — e сильнее, как в контракте.
func TestClientLinkExitWinsOverNoExit(t *testing.T) {
	d := &fakeDaemon{status: "Idle", nets: demoNets}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit", NoExit: true})
	res, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil)
	if err != nil || res.ExitID != "sv-demo-exit" || res.NoExit {
		t.Fatal(res, err)
	}
	if b, _, _ := s.Load(); b.NoExit || b.ExitNode != "sv-demo-exit" {
		t.Fatalf("box %+v", b)
	}
}

// Включение щитом: ядро само включило выход (новый выход без SkipAutoApply,
// потерян state.json, выбор изменили в другом клиенте), а в коробке «без
// выхода» — снимаем ровно выходы.
func TestReconnectAppliesNoExit(t *testing.T) {
	d := &fakeDaemon{status: "Idle", nets: rootNets("exit-1")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|", NoExit: true})
	if err := f.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Up", "DeselectExits " + allExits}) {
		t.Fatalf("calls %v", d.calls)
	}
}

// В коробке выход, ядро включило другой — возвращаем запомненный.
func TestReconnectAppliesRememberedExit(t *testing.T) {
	d := &fakeDaemon{status: "Idle", nets: rootNets("exit-1")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|", ExitNode: "exit-5"})
	if err := f.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Up", "SelectExit exit-5"}) {
		t.Fatalf("calls %v", d.calls)
	}
}

// Сверять нечего: выбор совпадает, запомненного выхода нет в списке, «без выхода»
// и выходы сняты, карты ещё нет — службу не трогаем.
func TestReconcileNoop(t *testing.T) {
	cases := []struct {
		box  Box
		nets []exitpick.Network
	}{
		{Box{ExitNode: "exit-1"}, rootNets("exit-1")},
		{Box{ExitNode: "gone"}, rootNets("exit-1")},
		{Box{NoExit: true}, rootNets("")},
		{Box{NoExit: true}, nil},
		{Box{ExitNode: "exit-1"}, nil},
		{Box{}, rootNets("exit-2")},
	}
	for i, c := range cases {
		d := &fakeDaemon{status: "Connected", nets: c.nets}
		f, s := newFlow(t, d)
		c.box.Identity = "netbird.jet-net.net|"
		_ = s.Save(c.box)
		if err := f.Reconcile(context.Background()); err != nil || len(d.calls) != 0 {
			t.Fatalf("%d: %v %v", i, err, d.calls)
		}
	}
}

// Частичная ошибка DeselectNetworks (один ID исчез между Networks и Deselect):
// если после неё ни один выход не включён — выбор принят и запомнен.
func TestChooseNoExitPartialErrorButAllOff(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: rootNets("exit-1"), afterDeselect: rootNets(""),
		deselectErr: errors.New("route 'exit-9' is not available")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|", ExitNode: "exit-1"})
	if err := f.ChooseNoExit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); !b.NoExit || b.ExitNode != "" {
		t.Fatalf("box %+v", b)
	}
}

// Ошибка и выход остался включён — это отказ, «без выхода» не запоминается.
func TestChooseNoExitErrorExitStillOn(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: rootNets("exit-1"), deselectErr: errors.New("boom")}
	f, s := newFlow(t, d)
	_ = s.Save(Box{Identity: "netbird.jet-net.net|", ExitNode: "exit-1"})
	if err := f.ChooseNoExit(context.Background()); KindOf(err) != KindUnknown {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); b.NoExit || b.ExitNode != "exit-1" {
		t.Fatalf("box %+v", b)
	}
}
