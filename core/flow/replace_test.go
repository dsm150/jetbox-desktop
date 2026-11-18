package flow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Замена подключения новой личностью (1.0.5): новый setup key при живой регистрации —
// новый профиль службы с новым ключом WireGuard; отказ входа — откат на прежний.
// Ключи и токены здесь выдуманные.

const otherKey = "1EF79C2F-1A34-4B21-9C0D-1BF529332998"

// Облачные ссылки: выхода нет, абонента различает токен устройства t.
const (
	cloudA = "jetbox://connect#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&n=Ivanov&p=windows&t=dev-token-aaaa"
	cloudB = "jetbox://connect#k=" + otherKey + "&m=https%3A%2F%2Fnetbird.jet-net.net&n=Petrov&p=windows&t=dev-token-bbbb"
)

// demoLink с другим ключом — Кабинет перевыпустил ссылку той же коробки.
var reissuedDemoLink = strings.Replace(demoLink, key, otherKey, 1)

// replaceCalls — порядок вызовов успешной замены до Up включительно.
var replaceCalls = []string{"Status", "Down", "AddProfile", "ConfigureProfile new1 https://netbird.jet-net.net JetBox", "LoginProfile new1 https://netbird.jet-net.net", "SwitchProfile new1", "Up"}

// registeredBox — коробка, записанная 1.0.5 после входа ссылкой raw.
func registeredBox(t *testing.T, raw string) Box {
	t.Helper()
	l := link(t, raw)
	return Box{Identity: l.BoxIdentity(), Name: l.DisplayName(), ManagementURL: l.ManagementURL,
		ExitNode: l.ExitNode, KeyFingerprint: l.KeyFingerprint(), AutoConnect: true,
		RegisteredAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
}

func hasCall(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestSameKeyNoNewProfile(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets}
	f, s := newFlow(t, d)
	old := registeredBox(t, demoLink)
	_ = s.Save(old)
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Status", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); b.KeyFingerprint != old.KeyFingerprint || !b.RegisteredAt.Equal(old.RegisteredAt) {
		t.Fatalf("box %+v", b)
	}
}

// LINK-CONTRACT 1.2 §2.11: новая ссылка той же коробки (то же удостоверение, другой
// ключ) — новая личность без вопроса, как iOS/Android.
func TestNewKeySameBoxReplacesWithoutAsking(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets}
	f, s := newFlow(t, d)
	old := registeredBox(t, demoLink)
	_ = s.Save(old)
	l := link(t, reissuedDemoLink)

	var stages []Stage
	res, err := f.Connect(context.Background(), l, Options{}, func(st Stage) { stages = append(stages, st) })
	if err != nil || res.ExitID != "sv-demo-exit" {
		t.Fatal(res, err)
	}
	want := append(append([]string{}, replaceCalls...), "SelectExit sv-demo-exit")
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if hasCall(d.calls, "RemoveProfile") || hasCall(d.calls, "Logout") {
		t.Fatalf("прежний профиль или пир тронут: %v", d.calls)
	}
	if d.active != "new1" || !reflect.DeepEqual(d.profiles, []string{"new1"}) || !reflect.DeepEqual(d.loginKeys, []string{otherKey}) {
		t.Fatalf("active=%q profiles=%v keys=%v", d.active, d.profiles, d.loginKeys)
	}
	if !reflect.DeepEqual(stages, []Stage{StageRegistering, StageConnecting, StageChoosingExit, StageProtected}) {
		t.Fatalf("stages %v", stages)
	}
	b, _, _ := s.Load()
	if b.KeyFingerprint != l.KeyFingerprint() || b.ProfileID != "new1" || b.ExitNode != "sv-demo-exit" || !b.AutoConnect ||
		b.RegisteredAt.Equal(old.RegisteredAt) {
		t.Fatalf("box %+v", b)
	}
}

func TestNewKeyLoginRejectedRollsBack(t *testing.T) {
	for _, wasOn := range []bool{true, false} {
		t.Run(fmt.Sprint("защита включена=", wasOn), func(t *testing.T) {
			st := "Idle"
			if wasOn {
				st = "Connected"
			}
			d := &fakeDaemon{status: st, nets: demoNets,
				loginErr: status.Error(codes.PermissionDenied, "couldn't add peer: setup key is invalid")}
			f, s := newFlow(t, d)
			old := registeredBox(t, demoLink)
			_ = s.Save(old)
			_, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil)
			if KindOf(err) != KindLinkExpired {
				t.Fatal(err)
			}
			want := append(append([]string{}, replaceCalls[:5]...), "Down", "SwitchProfile default")
			if wasOn {
				want = append(want, "Up")
			}
			want = append(want, "RemoveProfile new1")
			if !reflect.DeepEqual(d.calls, want) {
				t.Fatalf("calls %v", d.calls)
			}
			if d.activeID() != "default" || len(d.profiles) != 0 {
				t.Fatalf("active=%q profiles=%v", d.activeID(), d.profiles)
			}
			if !reflect.DeepEqual(d.configured, []string{"new1"}) {
				t.Fatalf("конфиг прежнего профиля тронут: %v", d.configured)
			}
			if b, _, _ := s.Load(); b != old {
				t.Fatalf("box.json изменён: %+v", b)
			}
		})
	}
}

func TestReplaceCancelDuringLoginRollsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &fakeDaemon{status: "Connected", nets: demoNets}
	d.loginHook = func(context.Context) error { cancel(); return status.Error(codes.Canceled, "context canceled") }
	var upCtx error = errors.New("Up не звали")
	d.upHook = func(c context.Context) error { upCtx = c.Err(); return nil }
	f, s := newFlow(t, d)
	old := registeredBox(t, demoLink)
	_ = s.Save(old)
	_, err := f.Connect(ctx, link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	want := append(append([]string{}, replaceCalls[:5]...), "Down", "SwitchProfile default", "Up", "RemoveProfile new1")
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if upCtx != nil {
		t.Fatalf("прежнее подключение поднимали отменённым контекстом: %v", upCtx)
	}
	if b, _, _ := s.Load(); b != old {
		t.Fatalf("box.json изменён: %+v", b)
	}
}

func TestReplaceBrowserLoginRollsBack(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, browser: true}
	f, s := newFlow(t, d)
	_ = s.Save(registeredBox(t, demoLink))
	_, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil)
	if KindOf(err) != KindNeedsBrowserLogin || !hasCall(d.calls, "RemoveProfile new1") || d.activeID() != "default" {
		t.Fatal(err, d.calls)
	}
}

func TestReplaceUpFailsAfterLoginKeepsNewProfile(t *testing.T) {
	d := &fakeDaemon{statuses: []string{"Connected", "Idle"}, nets: demoNets,
		upErr: status.Error(codes.Unavailable, "connection refused")}
	f, s := newFlow(t, d)
	_ = s.Save(registeredBox(t, demoLink))
	l := link(t, reissuedDemoLink)
	_, err := f.Connect(context.Background(), l, Options{ConfirmSwitch: true}, nil)
	if KindOf(err) != KindServerUnreachable {
		t.Fatal(err)
	}
	if hasCall(d.calls, "RemoveProfile") || d.active != "new1" {
		t.Fatalf("вошли новым ключом — откатывать нечего: %v", d.calls)
	}
	if b, _, _ := s.Load(); b.KeyFingerprint != l.KeyFingerprint() || b.ProfileID != "new1" {
		t.Fatalf("новая регистрация не запомнена: %+v", b)
	}
}

func TestReplaceStepFailuresRollBack(t *testing.T) {
	boom := errors.New("boom")
	for name, tc := range map[string]struct {
		setup func(d *fakeDaemon)
		want  []string
	}{
		// Служба с --disable-profiles отвечает Unavailable: нового профиля нет, удалять нечего.
		// Активный профиль не менялся (до Login его никто не переключал) — переключать назад нечего.
		"AddProfile": {func(d *fakeDaemon) { d.addErr = status.Error(codes.Unavailable, "profiles are disabled") },
			[]string{"Status", "Down", "AddProfile", "Down", "Up"}},
		"Configure": {func(d *fakeDaemon) { d.configureErr = boom },
			append(append([]string{}, replaceCalls[:4]...), "Down", "Up", "RemoveProfile new1")},
	} {
		t.Run(name, func(t *testing.T) {
			d := &fakeDaemon{status: "Connected", nets: demoNets}
			tc.setup(d)
			f, s := newFlow(t, d)
			old := registeredBox(t, demoLink)
			_ = s.Save(old)
			_, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil)
			if KindOf(err) != KindUnknown {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.calls, tc.want) {
				t.Fatalf("calls %v", d.calls)
			}
			if d.activeID() != "default" || len(d.profiles) != 0 {
				t.Fatalf("active=%q profiles=%v", d.activeID(), d.profiles)
			}
			if b, _, _ := s.Load(); b != old {
				t.Fatalf("box.json изменён: %+v", b)
			}
		})
	}
}

// Login уже сделал новый профиль активным и зарегистрировал пир: сбой явного
// SwitchProfile после входа — только журнал, не откат.
func TestReplaceSwitchAfterLoginFailsContinues(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, switchErr: map[string]error{"new1": errors.New("boom")}}
	f, s := newFlow(t, d)
	var logged []string
	f.cfg.Logf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	_ = s.Save(registeredBox(t, demoLink))
	if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	if hasCall(d.calls, "RemoveProfile") || len(logged) != 1 {
		t.Fatalf("calls %v log %v", d.calls, logged)
	}
	if b, _, _ := s.Load(); b.ProfileID != "new1" {
		t.Fatalf("box %+v", b)
	}
}

// Вернуться на прежний профиль не вышло — новый активен: удалить его служба не даст,
// а Up поднял бы незарегистрированный. Только журнал.
func TestRollbackSwitchBackFailsStops(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, loginErr: status.Error(codes.PermissionDenied, "invalid setup key"),
		switchErr: map[string]error{"default": errors.New("boom")}}
	f, s := newFlow(t, d)
	var logged []string
	f.cfg.Logf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	_ = s.Save(registeredBox(t, demoLink))
	if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil); KindOf(err) != KindLinkExpired {
		t.Fatal(err)
	}
	if hasCall(d.calls[5:], "RemoveProfile") || hasCall(d.calls[5:], "Up") || len(logged) != 1 {
		t.Fatalf("calls %v log %v", d.calls, logged)
	}
	// Запись о замене осталась — следующее действие доводит откат.
	if _, ok := s.LoadReplace(); !ok {
		t.Fatal("запись о замене удалена при неудачном откате")
	}
	d.switchErr, d.calls = nil, nil
	if err := f.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls[:3], []string{"Down", "SwitchProfile default", "RemoveProfile new1"}) || d.activeID() != "default" {
		t.Fatalf("calls %v", d.calls)
	}
	if _, ok := s.LoadReplace(); ok {
		t.Fatal("запись о замене не удалена после восстановления")
	}
}

// Запись до 1.0.5 без отпечатка: та же identity — прежний ключ, отпечаток дописывается;
// другая — вопрос и новый профиль.
func TestLegacyBoxWithoutFingerprint(t *testing.T) {
	t.Run("та же identity", func(t *testing.T) {
		d := &fakeDaemon{status: "Connected", nets: demoNets}
		f, s := newFlow(t, d)
		_ = s.Save(Box{Identity: "netbird.jet-net.net|sv-demo-exit", ManagementURL: "https://netbird.jet-net.net"})
		l := link(t, reissuedDemoLink)
		if _, err := f.Connect(context.Background(), l, Options{}, nil); err != nil {
			t.Fatal(err)
		}
		if hasCall(d.calls, "AddProfile") {
			t.Fatalf("calls %v", d.calls)
		}
		if b, _, _ := s.Load(); b.KeyFingerprint != l.KeyFingerprint() {
			t.Fatalf("отпечаток не записан: %+v", b)
		}
	})
	t.Run("другая identity", func(t *testing.T) {
		d := &fakeDaemon{status: "Connected", nets: demoNets}
		f, s := newFlow(t, d)
		// облачная запись ≤1.0.4: хост| — у ссылки с t удостоверение уже хост|t:…
		_ = s.Save(Box{Identity: "netbird.jet-net.net|", Name: "Ivanov", ManagementURL: "https://netbird.jet-net.net"})
		_, err := f.Connect(context.Background(), link(t, cloudA), Options{}, nil)
		var fe *Error
		if !errors.As(err, &fe) || fe.Kind != KindOtherBox {
			t.Fatal(err)
		}
		if _, err := f.Connect(context.Background(), link(t, cloudA), Options{ConfirmSwitch: true}, nil); err != nil {
			t.Fatal(err)
		}
		if !hasCall(d.calls, "AddProfile") || hasCall(d.calls, "RemoveProfile") {
			t.Fatalf("calls %v", d.calls)
		}
	})
}

// Баг 13.11.2026: облачные абоненты на одном management без выхода — раньше одно
// удостоверение хост|, вход старым ключом. Теперь разный t — другое подключение.
func TestCloudToCloudOtherDeviceTokenReplaces(t *testing.T) {
	d := &fakeDaemon{status: "Connected"}
	f, s := newFlow(t, d)
	_ = s.Save(registeredBox(t, cloudA))
	_, err := f.Connect(context.Background(), link(t, cloudB), Options{}, nil)
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindOtherBox || fe.Detail != "Ivanov" {
		t.Fatal(err)
	}
	if _, err := f.Connect(context.Background(), link(t, cloudB), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.loginKeys, []string{otherKey}) || d.active != "new1" {
		t.Fatalf("keys=%v active=%q calls=%v", d.loginKeys, d.active, d.calls)
	}
	b, _, _ := s.Load()
	if b.Identity != link(t, cloudB).BoxIdentity() || strings.Contains(b.Identity, "dev-token") {
		t.Fatalf("box %+v", b)
	}
}

// Пустое хранилище: принятая как есть регистрация (adopt) получает отпечаток ключа
// ссылки только после удачного входа.
func TestAdoptWritesFingerprintOnlyAfterLogin(t *testing.T) {
	d := &fakeDaemon{status: "Connected", mgmt: "https://netbird.jet-net.net:443", nets: demoNets,
		loginErr: status.Error(codes.Unavailable, "connection refused")}
	f, s := newFlow(t, d)
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err == nil {
		t.Fatal("ждали отказ входа")
	}
	if _, ok, _ := s.Load(); ok || hasCall(d.calls, "AddProfile") {
		t.Fatalf("calls %v", d.calls)
	}
	d.loginErr, d.calls = nil, nil
	l := link(t, demoLink)
	if _, err := f.Connect(context.Background(), l, Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if hasCall(d.calls, "AddProfile") {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); b.KeyFingerprint != l.KeyFingerprint() || b.ProfileID != "default" {
		t.Fatalf("box %+v", b)
	}
}

// Первый вход — как раньше, в текущем профиле, без нового.
func TestFirstLoginNoNewProfile(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	f, s := newFlow(t, d)
	l := link(t, demoLink)
	if _, err := f.Connect(context.Background(), l, Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if hasCall(d.calls, "AddProfile") || hasCall(d.calls, "ActiveProfile") {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); b.KeyFingerprint != l.KeyFingerprint() {
		t.Fatalf("box %+v", b)
	}
}

func TestSameConnection(t *testing.T) {
	l := link(t, demoLink)
	b := Box{Identity: l.BoxIdentity(), ManagementURL: "https://netbird.jet-net.net:443", KeyFingerprint: l.KeyFingerprint()}
	if !SameConnection(b, l) {
		t.Fatal("тот же ключ и management")
	}
	if SameConnection(b, link(t, reissuedDemoLink)) {
		t.Fatal("другой ключ")
	}
	b.ManagementURL = "https://other.example.net"
	if SameConnection(b, l) {
		t.Fatal("другой management")
	}
	if !SameConnection(Box{Identity: l.BoxIdentity()}, link(t, reissuedDemoLink)) {
		t.Fatal("запись без отпечатка — по удостоверению")
	}
}

// После неудачного входа служба в LoginFailed, и Up ответил бы «up already in progress»:
// откат сначала опускает её (Down → Idle), и прежнее подключение поднимается.
func TestRollbackDownBeforeUpAfterLoginFailed(t *testing.T) {
	d := &fakeDaemon{status: "Connected", strict: true, nets: demoNets,
		loginErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}
	f, s := newFlow(t, d)
	var logged []string
	f.cfg.Logf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	_ = s.Save(registeredBox(t, demoLink))
	if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil); KindOf(err) != KindServerUnreachable {
		t.Fatal(err)
	}
	if d.status != "Connected" || d.activeID() != "default" || len(logged) != 0 {
		t.Fatalf("status=%s active=%s log=%v calls=%v", d.status, d.activeID(), logged, d.calls)
	}
}

// Фейк честен: без Down после отказа входа Up не проходит.
func TestFakeRefusesUpInLoginFailed(t *testing.T) {
	d := &fakeDaemon{status: "Idle", strict: true, loginErr: errors.New("boom")}
	_, _ = d.Login(context.Background(), key, "https://netbird.jet-net.net")
	if err := d.Up(context.Background()); err == nil || !strings.Contains(err.Error(), "up already in progress") {
		t.Fatal(err)
	}
}

// replace.json живёт ровно с начала замены до сохранения коробки (или до отката).
func TestReplaceJournalLifetime(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, user: "ivan"}
	f, s := newFlow(t, d)
	_ = s.Save(registeredBox(t, demoLink))
	var during Replace
	d.loginHook = func(context.Context) error {
		during, _ = s.LoadReplace()
		return nil
	}
	if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil); err != nil {
		t.Fatal(err)
	}
	if during.OldID != "default" || during.OldUser != "ivan" || during.NewID != "new1" || during.NewUser != "ivan" ||
		!strings.HasPrefix(during.NewName, "jetbox-") || during.At.IsZero() {
		t.Fatalf("запись во время входа: %+v", during)
	}
	if _, ok := s.LoadReplace(); ok {
		t.Fatal("запись осталась после успешной замены")
	}
	if b, _, _ := s.Load(); b.ProfileID != "new1" || b.ProfileUser != "ivan" {
		t.Fatalf("box %+v", b)
	}
}

// Прерывание замены (выход, сбой питания) — восстанавливаем по replace.json.
func interrupted(t *testing.T, boxProfile string) (*fakeDaemon, *Flow, *Store) {
	t.Helper()
	d := &fakeDaemon{status: "Idle", nets: demoNets, active: "new1", user: "ivan",
		profiles: []string{"new1"}, names: map[string]string{"new1": "jetbox-1"}}
	f, s := newFlow(t, d)
	b := registeredBox(t, demoLink)
	b.ProfileID = boxProfile
	if boxProfile == "new1" {
		b.ProfileUser = "ivan"
	}
	_ = s.Save(b)
	_ = s.SaveReplace(Replace{OldID: "default", NewName: "jetbox-1", NewID: "new1", NewUser: "ivan", At: time.Now()})
	return d, f, s
}

func TestRecoverAfterLoginBeforeSwitch(t *testing.T) {
	// Login сделал new1 активным и зарегистрировал, до SwitchProfile и записи коробки не дошли.
	d, f, s := interrupted(t, "")
	if _, err := f.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"Down", "SwitchProfile default", "RemoveProfile new1"}) {
		t.Fatalf("calls %v", d.calls)
	}
	if _, ok := s.LoadReplace(); ok || d.activeID() != "default" || len(d.profiles) != 0 {
		t.Fatalf("active=%s profiles=%v", d.activeID(), d.profiles)
	}
}

func TestRecoverAfterSwitchBeforeSave(t *testing.T) {
	// SwitchProfile прошёл, коробка ещё старая — то же через Connect той же ссылкой:
	// сначала сверка, потом обычный вход прежним ключом.
	d, f, s := interrupted(t, "")
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Down", "SwitchProfile default", "RemoveProfile new1", "Status", "Login https://netbird.jet-net.net", "Up", "SelectExit sv-demo-exit"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls %v", d.calls)
	}
	if b, _, _ := s.Load(); b.ProfileID != "default" {
		t.Fatalf("box %+v", b)
	}
}

func TestRecoverAfterSaveBeforeJournalCleared(t *testing.T) {
	// Коробка уже в new1: он не недорегистрированный — не трогаем, только запись.
	d, f, s := interrupted(t, "new1")
	if _, err := f.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 0 || d.activeID() != "new1" || len(d.profiles) != 1 {
		t.Fatalf("calls %v", d.calls)
	}
	if _, ok := s.LoadReplace(); ok {
		t.Fatal("запись осталась")
	}
}

// AddProfile отменили после того, как служба создала профиль: ID нет — находим по имени.
func TestOrphanFromCanceledAddProfileRemovedByName(t *testing.T) {
	d := &fakeDaemon{status: "Idle", nets: demoNets, addErr: status.Error(codes.Canceled, "context canceled"), addCreates: true}
	f, s := newFlow(t, d)
	_ = s.Save(registeredBox(t, demoLink))
	if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !hasCall(d.calls, "RemoveProfile new1") || len(d.profiles) != 0 {
		t.Fatalf("calls %v profiles %v", d.calls, d.profiles)
	}
	if _, ok := s.LoadReplace(); ok {
		t.Fatal("запись осталась")
	}
}

// Записи о замене нет, а активен чужой профиль (второй пользователь Windows: служба
// одна, его профиль): службу не трогаем — только журнал.
func TestRecoverWithoutJournalLeavesService(t *testing.T) {
	d := &fakeDaemon{status: "Connected", active: "x9", user: "petr", profiles: []string{"p1", "x9"}}
	f, s := newFlow(t, d)
	var logged []string
	f.cfg.Logf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	b := registeredBox(t, demoLink)
	b.ProfileID, b.ProfileUser = "p1", "ivan"
	_ = s.Save(b)
	changed, err := f.Recover(context.Background())
	if err != nil || changed || len(d.calls) != 0 || d.activeID() != "x9" || len(logged) != 1 {
		t.Fatalf("changed=%v err=%v calls=%v log=%v", changed, err, d.calls, logged)
	}
	// Совпадает — ничего не делаем; старая коробка без ProfileID — это default.
	d2 := &fakeDaemon{status: "Connected"}
	f2, s2 := newFlow(t, d2)
	_ = s2.Save(Box{Identity: "x|y"})
	if changed, err := f2.Recover(context.Background()); err != nil || changed || len(d2.calls) != 0 {
		t.Fatal(err, d2.calls)
	}
}

// Второй пользователь Windows по своей ссылке: служба не переключается назад к профилю
// первого, Connect идёт в активном профиле.
func TestSecondWindowsUserConnectDoesNotSwitch(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets, active: "x9", user: "petr", profiles: []string{"p1", "x9"}}
	f, s := newFlow(t, d)
	b := registeredBox(t, demoLink)
	b.ProfileID, b.ProfileUser = "p1", "ivan"
	_ = s.Save(b)
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if hasCall(d.calls, "SwitchProfile") || hasCall(d.calls, "AddProfile") || d.activeID() != "x9" {
		t.Fatalf("calls %v", d.calls)
	}
}

// Переустановка: %APPDATA%\JetBox\box.json остался, а профиля из него в службе нет —
// коробка устарела, Connect по новой ссылке — первый вход без вопроса «другая коробка».
func TestStaleBoxAfterReinstall(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	f, s := newFlow(t, d)
	b := registeredBox(t, demoLink)
	b.ProfileID, b.ProfileUser = "gone1", "ivan"
	_ = s.Save(b)
	l := link(t, cloudB)
	if _, err := f.Connect(context.Background(), l, Options{}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"Status", "Configure https://netbird.jet-net.net JetBox", "Login https://netbird.jet-net.net", "Up"}
	if !reflect.DeepEqual(d.calls[:4], want) {
		t.Fatalf("calls %v", d.calls)
	}
	if got, _, _ := s.Load(); got.ProfileID != "default" || got.KeyFingerprint != l.KeyFingerprint() {
		t.Fatalf("box %+v", got)
	}
}

// Прерванная замена, а профиля коробки нет: возвращаем прежний из записи.
func TestRecoverJournalWithStaleBoxUsesOld(t *testing.T) {
	d, f, s := interrupted(t, "gone1")
	d.profiles = append(d.profiles, "old7")
	_ = s.SaveReplace(Replace{OldID: "old7", OldUser: "ivan", NewName: "jetbox-1", NewID: "new1", NewUser: "ivan"})
	changed, err := f.Recover(context.Background())
	if err != nil || !changed || !reflect.DeepEqual(d.calls, []string{"Down", "SwitchProfile old7", "RemoveProfile new1"}) {
		t.Fatalf("changed=%v err=%v calls=%v", changed, err, d.calls)
	}
}

// replace.json стирается, только когда нового профиля точно нет.
func TestJournalKeptUntilNewProfileGone(t *testing.T) {
	t.Run("RemoveProfile не прошёл", func(t *testing.T) {
		d, f, s := interrupted(t, "")
		d.removeErr = errors.New("management недоступен")
		if _, err := f.Recover(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.LoadReplace(); !ok {
			t.Fatal("запись стёрта, а профиль остался")
		}
		d.removeErr, d.calls = nil, nil
		if _, err := f.Recover(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.LoadReplace(); ok || len(d.profiles) != 0 || !reflect.DeepEqual(d.calls, []string{"RemoveProfile new1"}) {
			t.Fatalf("calls %v profiles %v", d.calls, d.profiles)
		}
	})
	t.Run("ListProfiles не прошёл", func(t *testing.T) {
		d := &fakeDaemon{status: "Idle", listErr: errors.New("boom")}
		f, s := newFlow(t, d)
		_ = s.SaveReplace(Replace{OldID: "default", NewName: "jetbox-1"})
		if _, err := f.Recover(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.LoadReplace(); !ok {
			t.Fatal("запись стёрта, хотя профиль не искали")
		}
	})
	t.Run("профиля уже нет", func(t *testing.T) {
		d, f, s := interrupted(t, "")
		d.profiles = nil
		if _, err := f.Recover(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.LoadReplace(); ok {
			t.Fatal("NotFound — профиля точно нет, запись должна быть стёрта")
		}
	})
	t.Run("откат: удаление не прошло", func(t *testing.T) {
		d := &fakeDaemon{status: "Connected", nets: demoNets, removeErr: errors.New("timeout"),
			loginErr: status.Error(codes.PermissionDenied, "invalid setup key")}
		f, s := newFlow(t, d)
		_ = s.Save(registeredBox(t, demoLink))
		if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil); KindOf(err) != KindLinkExpired {
			t.Fatal(err)
		}
		if _, ok := s.LoadReplace(); !ok {
			t.Fatal("запись стёрта, а новый профиль остался")
		}
	})
}

func TestRecoverSwitchFailsStopsConnect(t *testing.T) {
	d, f, s := interrupted(t, "")
	d.switchErr = map[string]error{"default": errors.New("boom")}
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); KindOf(err) != KindUnknown {
		t.Fatal(err)
	}
	if hasCall(d.calls, "Login") || hasCall(d.calls, "RemoveProfile") {
		t.Fatalf("calls %v", d.calls)
	}
	if _, ok := s.LoadReplace(); !ok {
		t.Fatal("запись удалена, хотя откат не доведён")
	}
}

// ProfileID пишется при каждом сохранении коробки: первый вход, тот же ключ, adopt.
func TestProfileRecordedOnEverySave(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets, active: "p7", user: "ivan"}
	f, s := newFlow(t, d)
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); b.ProfileID != "p7" || b.ProfileUser != "ivan" {
		t.Fatalf("первый вход: %+v", b)
	}
	d.status = "Connected"
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); b.ProfileID != "p7" {
		t.Fatalf("тот же ключ: %+v", b)
	}
}

func TestRecoverServiceDownIsServiceMissing(t *testing.T) {
	d := &fakeDaemon{activeErr: status.Error(codes.Unavailable, "pipe not found")}
	f, _ := newFlow(t, d)
	if _, err := f.Connect(context.Background(), link(t, demoLink), Options{}, nil); KindOf(err) != KindServiceMissing {
		t.Fatal(err)
	}
	if err := f.Reconnect(context.Background()); KindOf(err) != KindServiceMissing {
		t.Fatal(err)
	}
}

// Откат при недоступном management: RemoveProfile служба не дождалась (тайм-аут), но профиль
// удалила локально — переспрос ListProfiles показывает, что его нет, запись стирается сразу.
func TestRollbackRemoveTimeoutButDeletedLocally(t *testing.T) {
	d := &fakeDaemon{status: "Connected", nets: demoNets, removeErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
		removeLocal: true, loginErr: status.Error(codes.PermissionDenied, "invalid setup key")}
	f, s := newFlow(t, d)
	_ = s.Save(registeredBox(t, demoLink))
	if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{ConfirmSwitch: true}, nil); KindOf(err) != KindLinkExpired {
		t.Fatal(err)
	}
	if _, ok := s.LoadReplace(); ok || len(d.profiles) != 0 {
		t.Fatalf("профиль удалён службой — запись должна быть стёрта: profiles %v", d.profiles)
	}
}

// LINK-CONTRACT 1.2 §2.10: облачная ссылка с x=0 — выход не выбирается и не включается.
func TestCloudLinkNoDefaultExit(t *testing.T) {
	d := &fakeDaemon{status: "NeedsLogin", nets: demoNets}
	f, s := newFlow(t, d)
	l := link(t, cloudA+"&e=cl-exit&x=0")
	if !l.NoDefaultExit {
		t.Fatal("x=0 не разобран")
	}
	res, err := f.Connect(context.Background(), l, Options{}, nil)
	if err != nil || !res.NoExit || res.NeedsChoice {
		t.Fatal(res, err)
	}
	if hasCall(d.calls, "SelectExit") || hasCall(d.calls, "DeselectExit") {
		t.Fatalf("выход тронут: %v", d.calls)
	}
	if b, _, _ := s.Load(); !b.NoExit || b.ExitNode != "" {
		t.Fatalf("box %+v", b)
	}
	// Повтор своей ссылки уважает выбор человека, а не x=0.
	_ = s.Save(func() Box { b, _, _ := s.Load(); b.NoExit, b.ExitNode = false, "cl-exit"; return b }())
	d.calls = nil
	if _, err := f.Connect(context.Background(), l, Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if !hasCall(d.calls, "SelectExit") {
		t.Fatalf("своя ссылка — выход по сохранённому выбору: %v", d.calls)
	}
}
