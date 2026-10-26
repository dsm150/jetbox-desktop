package controller

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/netbirdio/netbird/client/proto"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
	"github.com/dsm150/jetbox-desktop/core/flow"
	"github.com/dsm150/jetbox-desktop/core/jetboxlink"
)

// Engine — то, что контроллеру нужно от core/flow.Flow.
type Engine interface {
	Connect(ctx context.Context, link jetboxlink.Payload, opts flow.Options, progress func(flow.Stage)) (flow.Result, error)
	ChooseExit(ctx context.Context, id string) error
	ChooseNoExit(ctx context.Context) error
	Exits(ctx context.Context) ([]exitpick.Candidate, error)
	Reconcile(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Reconnect(ctx context.Context) error
	Forget(ctx context.Context) error
	Box() (flow.Box, bool, error)
}

// Statuser — состояние службы и её сети (daemonclient.Client).
type Statuser interface {
	Status(ctx context.Context) (*proto.StatusResponse, error)
	Networks(ctx context.Context) ([]exitpick.Network, error)
}

// ProbeFunc меряет «до дома» по адресу пира коробки.
type ProbeFunc func(ctx context.Context, boxIP string) (time.Duration, error)

// Options — необязательные зависимости контроллера.
type Options struct {
	// OtherVPN — детектор чужого VPN (core/othervpn) для щита: проверяется перед
	// Reconnect. nil — не проверять. «Подключить» по ссылке проверяет сам core/flow.
	OtherVPN func(ctx context.Context) (detail string, found bool)
	// Now — часы для «в сети с»; nil — time.Now.
	Now func() time.Time
}

const probeEvery = 10 * time.Second

// exitGrace — сколько ждать пир выбранного выхода (или карту от management)
// после того, как служба стала Connected, выход сменился или его пир упал,
// прежде чем показать «Выход не отвечает» (или решить, что выхода нет).
const exitGrace = 15 * time.Second

// reconcileEvery — как часто опрос панели сверяет выбор выхода в ядре с коробкой.
const reconcileEvery = 10 * time.Second

// Controller — экраны приложения. Методы блокирующие: мост вызывает их из горутин.
//
// Все действия со службой идут строго по одному: флаг Busy проверяется и ставится
// в одной критической секции. Ссылка, пришедшая во время Busy, откладывается и
// обрабатывается, когда действие закончится.
type Controller struct {
	mu        sync.Mutex
	st        State
	seq       uint64
	eng       Engine
	status    Statuser
	probe     ProbeFunc
	otherVPN  func(ctx context.Context) (string, bool)
	now       func() time.Time
	pending   *jetboxlink.Payload
	opts      flow.Options
	nextLink  string // ссылка, пришедшая во время Busy (последняя побеждает)
	lastProbe time.Time
	probeIP   string    // к какому пиру мерили «до дома» в прошлый раз
	exitWait  time.Time // с какого момента ждём пир выхода или карту; ноль — не ждём
	exitWas   string    // последний выбранный выход: смена — ждём заново
	lastSync  time.Time // последняя сверка выбора выхода с коробкой
	since     time.Time // «в сети с» по нашим часам; ноль — не в сети
	observed  bool      // служба уже ответила хотя бы раз
	failsRow  int       // опросов подряд, на которые служба не ответила

	notifyMu sync.Mutex // доставка onChange строго по возрастанию Seq
	sent     uint64
	onChange func(State)
}

// New — контроллер; onChange получает копию состояния после каждого изменения,
// строго по возрастанию Seq (устаревшие копии не доставляются). onChange не должен
// синхронно вызывать методы, меняющие состояние.
func New(eng Engine, status Statuser, probe ProbeFunc, onChange func(State), lang, version string, opts ...Options) *Controller {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Controller{
		eng: eng, status: status, probe: probe, onChange: onChange, otherVPN: o.OtherVPN, now: o.Now,
		st: State{Lang: lang, Version: version, Screen: ScreenNoBox, Panel: Panel{HomeMs: -1}},
	}
}

// State — копия текущего состояния.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.copyLocked()
}

func (c *Controller) copyLocked() State {
	s := c.st
	s.Candidates = append([]ExitCandidate(nil), c.st.Candidates...)
	return s
}

// mutate меняет состояние под c.mu. f возвращает false — изменения нет: Seq не
// растёт, рассылки нет. Внутри f можно трогать поля контроллера (pending и др.).
func (c *Controller) mutate(f func(s *State) bool) bool {
	c.mu.Lock()
	if !f(&c.st) {
		c.mu.Unlock()
		return false
	}
	c.seq++
	c.st.Seq = c.seq
	s := c.copyLocked()
	c.mu.Unlock()
	c.notify(s)
	return true
}

func (c *Controller) update(f func(s *State)) {
	c.mutate(func(s *State) bool { f(s); return true })
}

// notify доставляет состояние, если оно новее уже доставленного.
func (c *Controller) notify(s State) {
	if c.onChange == nil {
		return
	}
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	if s.Seq <= c.sent {
		return
	}
	c.sent = s.Seq
	c.onChange(s)
}

// begin атомарно занимает контроллер (Busy), если ok разрешает.
func (c *Controller) begin(ok func(s *State) bool) bool {
	return c.mutate(func(s *State) bool {
		if s.Busy || (ok != nil && !ok(s)) {
			return false
		}
		s.Busy = true
		return true
	})
}

// drainLink обрабатывает ссылку, отложенную на время Busy.
func (c *Controller) drainLink(ctx context.Context) {
	c.mu.Lock()
	raw := c.nextLink
	c.nextLink = ""
	c.mu.Unlock()
	if raw != "" {
		c.OnLink(ctx, raw)
	}
}

func clearError(s *State) {
	s.ErrorKind, s.ErrorOp, s.ErrorDetail, s.CanRetry, s.OtherVPN = "", "", "", false, ""
}

func setBox(s *State, b flow.Box, has bool) {
	if has {
		s.BoxName, s.ExitName, s.NoExit = b.Name, b.ExitNode, b.NoExit
	} else {
		s.BoxName, s.ExitName, s.NoExit = "", "", false
	}
}

// restOf — куда возвращаться, когда диалог закрыт: домой, если коробка есть.
func restOf(hasBox bool) Screen {
	if hasBox {
		return ScreenHome
	}
	return ScreenNoBox
}

// fillReady — экран «Ваш JetBox» по ссылке.
func fillReady(s *State, p jetboxlink.Payload) {
	host := p.ManagementURL
	if u, err := url.Parse(p.ManagementURL); err == nil && u.Host != "" {
		host = u.Host
	}
	clearError(s)
	s.Screen, s.LinkBoxName, s.Host, s.KeyMask = ScreenReady, p.DisplayName(), host, p.MaskedKey()
}

// scrub заменяет ключ ссылки (в любом регистре) маской.
func scrub(text string, links ...*jetboxlink.Payload) string {
	for _, l := range links {
		if l == nil || l.SetupKey == "" {
			continue
		}
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(l.SetupKey))
		text = re.ReplaceAllLiteralString(text, l.MaskedKey())
	}
	return text
}

// Init — первый показ: домой или «не подключён», затем состояние службы.
func (c *Controller) Init(ctx context.Context) {
	box, has, _ := c.eng.Box()
	c.update(func(s *State) {
		s.Screen = restOf(has)
		setBox(s, box, has)
	})
	c.Refresh(ctx)
}

// OnLink — пришла ссылка (холодный старт или второй экземпляр). Во время
// подключения/переключения ссылка откладывается до конца действия.
func (c *Controller) OnLink(ctx context.Context, raw string) {
	p, out := jetboxlink.Parse(raw)
	if out == jetboxlink.NotOurs {
		return
	}
	box, has, _ := c.eng.Box()
	connectNow := false
	c.mutate(func(s *State) bool {
		if s.Busy {
			if out == jetboxlink.OK && c.pending != nil && *c.pending == p {
				return false // эта ссылка уже в работе
			}
			c.nextLink = raw
			return false
		}
		c.nextLink = ""
		if out == jetboxlink.Broken {
			c.pending, c.opts = nil, flow.Options{}
			clearError(s)
			s.Screen, s.ErrorKind, s.ErrorOp = ScreenError, flow.KindLinkBroken.String(), OpConnect
			return true
		}
		c.pending, c.opts = &p, flow.Options{}
		switch {
		case has && box.Identity == p.BoxIdentity():
			connectNow = true
			return false
		case has:
			clearError(s)
			s.Screen, s.BoxName, s.LinkBoxName = ScreenSwitch, box.Name, p.DisplayName()
		default:
			fillReady(s, p)
		}
		return true
	})
	if connectNow {
		c.Connect(ctx)
	}
}

var stageNames = map[flow.Stage]string{
	flow.StageRegistering:  "registering",
	flow.StageConnecting:   "connecting",
	flow.StageChoosingExit: "choosing-exit",
}

// Connect — «Подключить» (или повтор) по ожидающей ссылке.
func (c *Controller) Connect(ctx context.Context) {
	var (
		link jetboxlink.Payload
		opts flow.Options
	)
	if !c.mutate(func(s *State) bool {
		if c.pending == nil || s.Busy {
			return false
		}
		link, opts = *c.pending, c.opts
		clearError(s)
		s.Screen, s.Stage, s.Busy = ScreenProgress, "", true
		return true
	}) {
		return
	}
	res, err := c.eng.Connect(ctx, link, opts, func(st flow.Stage) {
		if name, ok := stageNames[st]; ok {
			c.update(func(s *State) { s.Stage = name })
		}
	})
	if err != nil {
		c.fail(err, OpConnect, &link)
		c.drainLink(ctx)
		return
	}

	box, has, _ := c.eng.Box()
	c.update(func(s *State) {
		c.pending, c.opts = nil, flow.Options{}
		clearError(s)
		s.Busy, s.Stage, s.LinkBoxName, s.Host, s.KeyMask = false, "", "", "", ""
		setBox(s, box, has)
		if res.NeedsChoice {
			s.Screen, s.Candidates = ScreenChooseExit, candidates(res.Candidates)
			return
		}
		s.Screen, s.Candidates = ScreenHome, nil
		if res.ExitID != "" {
			s.ExitName = res.ExitID
		}
	})
	c.Refresh(ctx)
	c.drainLink(ctx)
}

func retryable(k flow.Kind) bool {
	return k != flow.KindLinkExpired && k != flow.KindLinkBroken && k != flow.KindNeedsBrowserLogin
}

// fail показывает отказ действия op и снимает Busy. link — ссылка, по которой
// подключались (для OpConnect), иначе nil.
func (c *Controller) fail(err error, op string, link *jetboxlink.Payload) {
	kind := flow.KindOf(err)
	vpn := ""
	var fe *flow.Error
	if errors.As(err, &fe) {
		vpn = fe.Detail
	}
	var (
		box flow.Box
		has bool
	)
	if kind == flow.KindOtherBox {
		box, has, _ = c.eng.Box() // до c.mu: чтение файла
	}
	c.update(func(s *State) {
		s.Busy, s.Stage = false, ""
		clearError(s)
		if op == OpConnect && !retryable(kind) {
			c.pending, c.opts = nil, flow.Options{} // ссылка негодна — повторять нечего
		}
		switch kind {
		case flow.KindOtherBox:
			s.Screen, s.BoxName, s.LinkBoxName = ScreenSwitch, "", ""
			if has {
				s.BoxName = box.Name // пусто — «другая JetBox», имя неизвестно
			}
			if link != nil {
				s.LinkBoxName = link.DisplayName()
			}
		case flow.KindOtherVPN:
			s.Screen, s.OtherVPN = ScreenOtherVPN, vpn
		case flow.KindServiceMissing:
			s.Screen = ScreenServiceMissing
		case flow.KindNeedsLink:
			s.Screen, s.Panel.NeedsLink = ScreenHome, true
		default:
			s.Screen, s.ErrorKind, s.ErrorOp = ScreenError, kind.String(), op
			s.ErrorDetail = scrub(err.Error(), link, c.pending)
			s.CanRetry = op == OpConnect && c.pending != nil && retryable(kind)
		}
	})
}

// ConfirmSwitch — «Переключить» на другую коробку.
func (c *Controller) ConfirmSwitch(ctx context.Context) {
	c.mu.Lock()
	c.opts.ConfirmSwitch = true
	c.mu.Unlock()
	c.Connect(ctx)
}

// CancelSwitch — «Отмена»: остаёмся со старой коробкой, ссылку забываем.
func (c *Controller) CancelSwitch() {
	box, has, _ := c.eng.Box()
	c.mutate(func(s *State) bool {
		if s.Busy {
			return false
		}
		c.pending, c.opts = nil, flow.Options{}
		s.Screen, s.LinkBoxName = restOf(has), ""
		setBox(s, box, has)
		return true
	})
}

// IgnoreOtherVPN — «Всё равно подключить»: по ссылке — Connect без проверки,
// без ссылки (щит) — Reconnect без проверки.
func (c *Controller) IgnoreOtherVPN(ctx context.Context) {
	c.mu.Lock()
	hasPending := c.pending != nil
	if hasPending {
		c.opts.IgnoreOtherVPN = true
	}
	c.mu.Unlock()
	if hasPending {
		c.Connect(ctx)
		return
	}
	if c.begin(func(s *State) bool { return s.Screen == ScreenOtherVPN }) {
		c.reconnect(ctx)
	}
}

// Recheck — «Проверить снова» на экранах other-vpn и service-missing.
func (c *Controller) Recheck(ctx context.Context) {
	if c.hasPending() {
		c.Connect(ctx)
		return
	}
	if c.begin(func(s *State) bool { return s.Screen == ScreenOtherVPN }) {
		c.shieldOn(ctx)
		return
	}
	_, has, _ := c.eng.Box()
	c.mutate(func(s *State) bool {
		if s.Busy {
			return false
		}
		s.Screen = restOf(has)
		return true
	})
	c.Refresh(ctx)
}

func (c *Controller) hasPending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending != nil
}

// ChooseExit — выбор на экране «Выберите выход». core/flow запоминает выбор в
// коробке — показываем то, что в ней.
func (c *Controller) ChooseExit(ctx context.Context, id string) {
	if !c.begin(nil) {
		return
	}
	if err := c.eng.ChooseExit(ctx, id); err != nil {
		c.fail(err, OpChooseExit, nil)
		c.drainLink(ctx)
		return
	}
	box, has, _ := c.eng.Box()
	c.update(func(s *State) {
		s.Busy = false
		clearError(s)
		setBox(s, box, has)
		if s.ExitName == "" {
			s.ExitName = id
		}
		s.Screen, s.Candidates = ScreenHome, nil
	})
	c.Refresh(ctx)
	c.drainLink(ctx)
}

// candidates — выходы для окна.
func candidates(list []exitpick.Candidate) []ExitCandidate {
	out := make([]ExitCandidate, 0, len(list))
	for _, c := range list {
		out = append(out, ExitCandidate{ID: c.ID, Active: c.Selected})
	}
	return out
}

// NoExit — «Без выхода — только сети» на экране «Выберите выход»; core/flow
// снимает выходы в службе и запоминает выбор в коробке.
func (c *Controller) NoExit(ctx context.Context) {
	if !c.begin(nil) {
		return
	}
	if err := c.eng.ChooseNoExit(ctx); err != nil {
		c.fail(err, OpChooseExit, nil)
		c.drainLink(ctx)
		return
	}
	box, has, _ := c.eng.Box()
	c.update(func(s *State) {
		s.Busy = false
		clearError(s)
		setBox(s, box, has)
		s.Screen, s.Candidates = ScreenHome, nil
	})
	c.Refresh(ctx)
	c.drainLink(ctx)
}

// ChangeExit — «Сменить» в подробностях дома: экран «Выберите выход» с
// актуальным списком (что включено ядром сейчас). «Пока не выбирать» — домой
// без изменений.
func (c *Controller) ChangeExit(ctx context.Context) {
	if !c.begin(func(s *State) bool { return s.Screen == ScreenHome }) {
		return
	}
	list, err := c.eng.Exits(ctx)
	if err != nil {
		c.fail(err, OpChooseExit, nil)
		c.drainLink(ctx)
		return
	}
	c.update(func(s *State) {
		s.Busy = false
		clearError(s)
		s.Screen, s.Candidates = ScreenChooseExit, candidates(list)
	})
	c.drainLink(ctx)
}

// SkipExit — «Пока не выбирать»: домой без изменений; во время действия — ничего.
func (c *Controller) SkipExit() {
	c.mutate(func(s *State) bool {
		if s.Busy {
			return false
		}
		s.Screen, s.Candidates = ScreenHome, nil
		return true
	})
}

// Toggle — щит. Решает по состоянию службы, а не пира коробки:
// Connected/Connecting — выключить; NeedsLogin и т. п. — ничего (нужна ссылка);
// иначе (Idle) — проверить чужой VPN и включить.
func (c *Controller) Toggle(ctx context.Context) {
	if _, has, _ := c.eng.Box(); !has {
		return
	}
	off := false
	if !c.begin(func(s *State) bool {
		if !s.Panel.ServiceUp || s.Panel.NeedsLink {
			return false
		}
		switch s.Panel.ServiceState {
		case "Connected", "Connecting":
			off = true
		case "NeedsLogin", "LoginFailed", "SessionExpired":
			return false
		}
		return true
	}) {
		return
	}
	if off {
		c.disconnect(ctx)
		return
	}
	c.shieldOn(ctx)
}

// shieldOn — включение щита; Busy уже занят вызывающим.
func (c *Controller) shieldOn(ctx context.Context) {
	if c.otherVPN != nil {
		if detail, found := c.otherVPN(ctx); found {
			c.update(func(s *State) {
				s.Busy = false
				clearError(s)
				s.Screen, s.OtherVPN = ScreenOtherVPN, detail
			})
			c.drainLink(ctx)
			return
		}
	}
	c.reconnect(ctx)
}

// reconnect — Reconnect службы; Busy уже занят вызывающим.
func (c *Controller) reconnect(ctx context.Context) {
	if err := c.eng.Reconnect(ctx); err != nil {
		c.fail(err, OpReconnect, nil)
		c.drainLink(ctx)
		return
	}
	c.update(func(s *State) {
		s.Busy = false
		clearError(s)
		s.Screen, s.Panel.NeedsLink = ScreenHome, false
	})
	c.Refresh(ctx)
	c.drainLink(ctx)
}

// disconnect — Disconnect службы; Busy уже занят вызывающим.
func (c *Controller) disconnect(ctx context.Context) {
	if err := c.eng.Disconnect(ctx); err != nil {
		c.fail(err, OpDisconnect, nil)
		c.drainLink(ctx)
		return
	}
	c.update(func(s *State) {
		s.Busy = false
		if s.Screen == ScreenError || s.Screen == ScreenOtherVPN {
			s.Screen = ScreenHome
		}
		clearError(s)
	})
	c.Refresh(ctx)
	c.drainLink(ctx)
}

// Forget — «Отвязать компьютер». core/flow забывает коробку даже при ошибке
// (нет сети) — тогда показываем ошибку, но коробки уже нет.
func (c *Controller) Forget(ctx context.Context) {
	if !c.begin(nil) {
		return
	}
	err := c.eng.Forget(ctx)
	box, has, _ := c.eng.Box()
	c.update(func(s *State) {
		s.Busy = false
		clearError(s)
		setBox(s, box, has)
		if !has {
			c.since = time.Time{}
			s.Panel = Panel{ServiceUp: s.Panel.ServiceUp, HomeMs: -1}
		}
		s.Screen = restOf(has)
	})
	if err != nil {
		c.fail(err, OpForget, nil)
	}
	c.drainLink(ctx)
}

// Uninstall — «Удалить JetBox…»: run — штатное удаление ОС; пока оно идёт, Busy (окно
// показывает «Удаляем…»). Занят другим действием — ничего не делает.
func (c *Controller) Uninstall(run func() error) error {
	if !c.begin(nil) {
		return nil
	}
	err := run()
	c.update(func(s *State) { s.Busy = false })
	return err
}

// Fail — отказ действия вне контроллера (удаление приложения): экран ошибки по op.
func (c *Controller) Fail(err error, op string) {
	c.fail(err, op, nil)
}

// Dismiss — «Закрыть» на экране ошибки: к ссылке (заполненный ready) или домой.
func (c *Controller) Dismiss() {
	_, has, _ := c.eng.Box()
	c.mutate(func(s *State) bool {
		if s.Busy {
			return false
		}
		clearError(s)
		if c.pending != nil {
			fillReady(s, *c.pending)
			return true
		}
		s.Screen = restOf(has)
		return true
	})
}

// missAfter — сколько опросов подряд служба может не укладываться в тайм-аут, пока
// экран не сменится на «служба не отвечает» (опрос раз в 3 с, тайм-аут 5 с).
const missAfter = 3

// timedOut — служба не ответила вовремя, но канал к ней есть.
func timedOut(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || grpcstatus.Code(err) == codes.DeadlineExceeded
}

func needsLink(status string) bool {
	switch status {
	case "NeedsLogin", "LoginFailed", "SessionExpired":
		return true
	}
	return false
}

// Refresh — опрос службы для панели (раз в 3 с из моста).
func (c *Controller) Refresh(ctx context.Context) {
	st, err := c.status.Status(ctx)
	if err != nil {
		// Служба на месте, но не уложилась в тайм-аут (смена сети, переподключение сотни
		// пиров) — держим прежний экран; «служба не отвечает» — на missAfter-м таком опросе
		// подряд. Сокета нет (Unavailable и прочее) — сразу.
		c.mu.Lock()
		c.failsRow++
		hold := timedOut(err) && c.failsRow < missAfter && c.st.Panel.ServiceUp
		c.mu.Unlock()
		if hold {
			return
		}
		c.update(func(s *State) {
			c.since, c.exitWait, c.exitWas = time.Time{}, time.Time{}, ""
			s.Panel = Panel{HomeMs: -1}
			if s.Screen == ScreenHome || s.Screen == ScreenNoBox {
				s.Screen = ScreenServiceMissing
			}
		})
		return
	}
	c.mu.Lock()
	c.failsRow = 0
	c.mu.Unlock()
	// Сети нужны, только когда служба Connected: иначе ListNetworks всё равно
	// отвечает «not connected». Ошибка — сети неизвестны (nil).
	var nets []exitpick.Network
	if st.GetStatus() == "Connected" {
		if n, err := c.status.Networks(ctx); err == nil {
			nets = n
			if nets == nil {
				nets = []exitpick.Network{}
			}
		}
	}
	box, has, _ := c.eng.Box()
	snap := flow.SnapshotFrom(st, nets, has && box.NoExit)

	// «До дома» — только к поднятому пиру выхода: DNS-проба (раз в probeEvery
	// или сразу, если пир другой), не ответила — задержка WireGuard из статуса.
	homeMs := int64(-1)
	measure := snap.ExitUp && snap.BoxIP != ""
	c.mu.Lock()
	prev := c.st.Panel.HomeMs
	due := snap.BoxIP != c.probeIP || c.now().Sub(c.lastProbe) >= probeEvery
	switch {
	case !measure:
		c.probeIP = ""
	case due:
		c.lastProbe, c.probeIP = c.now(), snap.BoxIP
	}
	c.mu.Unlock()
	switch {
	case !measure:
	case due:
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if d, err := c.probe(pctx, snap.BoxIP); err == nil {
			homeMs = d.Milliseconds()
		} else if snap.Latency > 0 {
			homeMs = snap.Latency.Milliseconds()
		}
		cancel()
	default:
		homeMs = prev
	}

	c.update(func(s *State) {
		p := Panel{
			ServiceUp: true, ServiceState: snap.State, Connected: snap.Connected, Relayed: snap.Relayed,
			RxBytes: snap.RxBytes, TxBytes: snap.TxBytes, HomeMs: homeMs, NeedsLink: needsLink(snap.State),
			Exit: snap.ExitID, NoExit: snap.State == "Connected" && !snap.ExitSelected && !snap.Pending,
			CanChangeExit: snap.Exits > 1,
		}
		// Ждём, пока служба Connected, а пир выхода не поднят: выход выбран или карты
		// (сетей) ещё нет. Время — от начала ожидания: включение службы, смена выхода,
		// падение пира. Не дождались за exitGrace — выбранный выход «не отвечает»;
		// карты нет — решает коробка: выход в ней есть — «не отвечает», нет — «без выхода».
		waiting := snap.State == "Connected" && !snap.ExitUp && (snap.ExitSelected || snap.Pending)
		switch {
		case !waiting:
			c.exitWait = time.Time{}
		case c.exitWait.IsZero(), snap.ExitID != "" && c.exitWas != "" && snap.ExitID != c.exitWas:
			c.exitWait = c.now()
		}
		switch {
		case snap.State != "Connected":
			c.exitWas = ""
		case snap.ExitID != "":
			c.exitWas = snap.ExitID
		}
		if waiting && c.now().Sub(c.exitWait) >= exitGrace {
			switch {
			case snap.ExitSelected, has && box.ExitNode != "":
				p.ExitDown = true
			default:
				p.Connected = snap.ManagementConnected
				p.NoExit = true
			}
		}
		// «В сети с» — по нашим часам: время службы сбрасывается при смене
		// ретранслятор↔напрямую. Только при первом ответе службы (запуск
		// приложения при уже включённой защите) берём её время.
		switch {
		case !p.Connected:
			c.since = time.Time{}
		case c.since.IsZero() && !c.observed && !snap.Since.IsZero():
			c.since = snap.Since
		case c.since.IsZero():
			c.since = c.now()
		}
		c.observed = true
		if !c.since.IsZero() {
			p.SinceUnix = c.since.Unix()
		}
		s.Panel = p
		if s.Screen == ScreenServiceMissing && c.pending == nil {
			s.Screen = restOf(has)
		}
	})
	if has && snap.State == "Connected" && exitDiffers(box, snap.ExitID, nets) {
		c.mu.Lock()
		due := c.now().Sub(c.lastSync) >= reconcileEvery
		if due {
			c.lastSync = c.now()
		}
		c.mu.Unlock()
		if due {
			_ = c.eng.Reconcile(ctx) // поправленный выбор покажет следующий опрос
		}
	}
}

// exitDiffers — выбор выхода в ядре (selected) расходится с запомненным в
// коробке, и это можно поправить: карта пришла (выходы в списке есть) и
// запомненный выход в нём есть.
func exitDiffers(box flow.Box, selected string, nets []exitpick.Network) bool {
	exits := exitpick.Exits(nets)
	if box.NoExit {
		return selected != "" && len(exits) > 0
	}
	if box.ExitNode == "" || box.ExitNode == selected {
		return false
	}
	for _, c := range exits {
		if c.ID == box.ExitNode {
			return true
		}
	}
	return false
}
