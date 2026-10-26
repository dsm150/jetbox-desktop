// Package flow — сценарий «одной кнопки» поверх службы JetBox.
// Порядок вызовов и правила — LINK-CONTRACT §4 и живой прогон спайка Windows 26.10.2026.
package flow

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/netbirdio/netbird/client/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
	"github.com/dsm150/jetbox-desktop/core/jetboxlink"
)

// Daemon — то, что сценарию нужно от службы (реализация — daemonclient.Client).
type Daemon interface {
	Status(ctx context.Context) (*proto.StatusResponse, error)
	Configure(ctx context.Context, managementURL, iface string, port int64) error
	Login(ctx context.Context, setupKey, managementURL string) (needsBrowser bool, err error)
	Up(ctx context.Context) error
	Down(ctx context.Context) error
	Logout(ctx context.Context) error
	Networks(ctx context.Context) ([]exitpick.Network, error)
	SelectExit(ctx context.Context, id string) error
	DeselectExits(ctx context.Context, ids []string) error
}

// Stage — шаг, который показывает экран.
type Stage int

const (
	StageRegistering  Stage = iota // «Регистрируем компьютер…»
	StageConnecting                // «Подключаем…»
	StageChoosingExit              // выбор выхода (обычно мгновенно)
	StageProtected                 // «Вы под защитой JetBox»
)

// Config — платформенные значения (core/platform) и тайминги.
type Config struct {
	Iface    string
	Port     int64
	ExitWait time.Duration // сколько ждать маршруты от management (по умолчанию 20 с)
	ExitPoll time.Duration // как часто спрашивать (по умолчанию 1 с)
	// OtherVPN — детектор чужого VPN (core/othervpn); nil — не проверять.
	OtherVPN func(ctx context.Context) (detail string, found bool)
	// Online — «есть ли интернет вообще» (core/netcheck.Online); nil — не проверять.
	// Уточняет ServerUnreachable до NoInternet.
	Online func(ctx context.Context) bool
}

// Options — решения человека для этого нажатия.
type Options struct {
	ConfirmSwitch  bool // «Переключить на другую коробку?» — да
	IgnoreOtherVPN bool // «Всё равно подключить»
}

// Result — чем кончилось подключение.
type Result struct {
	ExitID      string               // выбранный выход
	NoExit      bool                 // применено запомненное «без выхода»
	NeedsChoice bool                 // показать «Выберите выход»
	Candidates  []exitpick.Candidate // варианты (пусто — выходов не видно)
}

// Flow — сценарий для одной службы и одного хранилища коробки.
// Connect, ChooseExit, ChooseNoExit, Exits, Reconcile, Disconnect, Reconnect и Forget выполняются строго по
// одному (mu): Disconnect во время Connect ждёт его конца — чтобы прервать
// подключение, отмените ctx у Connect (после Up служба будет опущена). Box
// не ждёт mu: хранилище защищено своей блокировкой.
type Flow struct {
	mu    sync.Mutex
	d     Daemon
	store *Store
	cfg   Config
}

// New собирает сценарий.
func New(d Daemon, s *Store, cfg Config) *Flow {
	if cfg.ExitWait == 0 {
		cfg.ExitWait = 20 * time.Second
	}
	if cfg.ExitPoll == 0 {
		cfg.ExitPoll = time.Second
	}
	return &Flow{d: d, store: s, cfg: cfg}
}

// needsLogin — служба не зарегистрирована (статусы службы: Idle, Connecting,
// Connected, NeedsLogin, LoginFailed, SessionExpired).
func needsLogin(status string) bool {
	switch status {
	case "NeedsLogin", "LoginFailed", "SessionExpired":
		return true
	}
	return false
}

// refine уточняет ServerUnreachable до NoInternet, если интернета нет вообще.
func (f *Flow) refine(ctx context.Context, k Kind) Kind {
	if k != KindServerUnreachable || f.cfg.Online == nil {
		return k
	}
	probe := ctx
	if ctx.Err() != nil {
		probe = context.WithoutCancel(ctx) // Online сам ограничен 3 с
	}
	if !f.cfg.Online(probe) {
		return KindNoInternet
	}
	return k
}

// netError — отказ, где важна только сеть: ServerUnreachable/NoInternet, прочее Unknown.
func (f *Flow) netError(ctx context.Context, detail string, err error) *Error {
	k := f.refine(ctx, classifyLogin(err))
	if k != KindServerUnreachable && k != KindNoInternet {
		k = KindUnknown
	}
	return &Error{Kind: k, Detail: detail, Err: err}
}

// upError разбирает отказ Up: если служба после него просит вход — регистрацию
// отвергли (onNeedsLogin), иначе классифицируем как отказ Login.
func (f *Flow) upError(ctx context.Context, err error, onNeedsLogin Kind) *Error {
	if st, serr := f.d.Status(ctx); serr == nil && needsLogin(st.GetStatus()) {
		return &Error{Kind: onNeedsLogin, Detail: "подключение", Err: err}
	}
	return &Error{Kind: f.refine(ctx, classifyLogin(err)), Detail: "подключение", Err: err}
}

// normManagement приводит адрес management к виду для сравнения: схема и хост
// в нижнем регистре, порт по умолчанию (443 для https, 80 для http) и хвостовой
// слэш убраны. Служба отдаёт адрес из профиля с портом
// (`https://netbird.jet-net.net:443`), ссылка — без (`https://netbird.jet-net.net`).
func normManagement(raw string) string {
	s := strings.TrimSpace(raw)
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimRight(strings.ToLower(s), "/")
	}
	scheme := strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	switch {
	case port != "":
		host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		host = "[" + host + "]" // IPv6 без порта
	}
	return scheme + "://" + host + strings.TrimRight(u.Path, "/")
}

func sameManagement(a, b string) bool { return normManagement(a) == normManagement(b) }

// rejectedByManagement — management уже отверг регистрацию службы: старая
// запись пира недействительна, и неудачный Logout ничего не оставляет.
func rejectedByManagement(status string) bool {
	return status == "NeedsLogin" || status == "SessionExpired"
}

// hasExit — выход из ссылки виден службе (значит, она зарегистрирована в той
// же коробке). Служба отдаёт сети только при запущенном движке
// (server/network.go:36-43), иначе — нет.
func (f *Flow) hasExit(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	nets, err := f.d.Networks(ctx)
	if err != nil {
		return false
	}
	for _, n := range nets {
		if n.ID == id {
			return true
		}
	}
	return false
}

// leave снимает с учёта прежнюю регистрацию перед новой. Если Logout не прошёл,
// а strict (management тот же и регистрацию он не отверг) — останавливаемся:
// Login на тот же management вернёт OK, не потратив новый ключ, и компьютер
// остался бы на старой коробке. Иначе Logout не важен: новый Configure+Login
// его перекроет.
func (f *Flow) leave(ctx context.Context, strict bool) error {
	_ = f.d.Down(ctx)
	if err := f.d.Logout(ctx); err != nil && strict {
		return f.netError(ctx, "не снять старую коробку", err)
	}
	_ = f.store.Clear()
	return nil
}

// Connect — нажатие «Подключить» по ссылке.
//
// Порядок: Status → чужой VPN → запомненная коробка. Другая коробка — только с
// подтверждением: Down, Logout, затем регистрация. Пустое хранилище при
// зарегистрированной службе: другой management — без вопроса (Down и Logout по
// возможности); тот же management и выход ссылки виден службе — та же коробка,
// принимаем как есть; иначе «другая JetBox» с подтверждением. Своя коробка:
// Configure не нужен, но Login вызывается всегда — служба вернёт OK без ключа,
// если пир жив, и перерегистрирует, если его удалили. Коробка запоминается
// сразу после Login. Отмена ctx после Up опускает службу (abortUp).
func (f *Flow) Connect(ctx context.Context, link jetboxlink.Payload, opts Options, progress func(Stage)) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if progress == nil {
		progress = func(Stage) {}
	}
	st, err := f.d.Status(ctx)
	if err != nil {
		return Result{}, &Error{Kind: KindServiceMissing, Detail: "служба не отвечает", Err: err}
	}
	if f.cfg.OtherVPN != nil && !opts.IgnoreOtherVPN {
		if detail, found := f.cfg.OtherVPN(ctx); found {
			return Result{}, &Error{Kind: KindOtherVPN, Detail: detail}
		}
	}

	stored, has, err := f.store.Load()
	if err != nil {
		return Result{}, &Error{Kind: KindUnknown, Detail: "не прочитать сохранённую коробку", Err: err}
	}
	daemonStatus := st.GetStatus()
	registered := !needsLogin(daemonStatus)
	same := has && stored.Identity == link.BoxIdentity()
	if same && daemonStatus == "LoginFailed" {
		// LoginFailed — не прошла проверка входа (сеть, сбой management;
		// server.go:769-772), а не отказ в регистрации: она, скорее всего, жива.
		registered = true
	}
	adopt := false // своя коробка, забытая хранилищем
	switch {
	case has && !same:
		if !opts.ConfirmSwitch {
			return Result{}, &Error{Kind: KindOtherBox, Detail: stored.Name}
		}
		strict := sameManagement(stored.ManagementURL, link.ManagementURL) && !rejectedByManagement(daemonStatus)
		if err := f.leave(ctx, strict); err != nil {
			return Result{}, err
		}
		registered = false
	case !has && registered:
		// Служба зарегистрирована, а мы ничего не помним. Служба называет свой
		// management (из профиля; server.go:1898); пусто — неизвестен, считаем тем же.
		daemonMgmt := st.GetFullStatus().GetManagementState().GetURL()
		switch {
		case daemonMgmt != "" && !sameManagement(daemonMgmt, link.ManagementURL):
			// Другой management (например, api.netbird.io:443 свежего профиля):
			// спрашивать не о чем, снимаем по возможности и регистрируемся.
			_ = f.leave(ctx, false)
			registered = false
		case f.hasExit(ctx, link.ExitNode):
			// Тот же management и выход коробки виден службе — та же коробка
			// (второй пользователь Windows, потерянный %APPDATA%): принимаем
			// регистрацию как есть, без Logout и Configure.
			adopt = true
		default:
			if !opts.ConfirmSwitch {
				return Result{}, &Error{Kind: KindOtherBox, Detail: ""}
			}
			if err := f.leave(ctx, true); err != nil {
				return Result{}, err
			}
			registered = false
		}
	}

	// Новая регистрация — всё, кроме своей (или принятой) коробки на живой регистрации.
	fresh := !(same || adopt) || !registered
	if fresh {
		progress(StageRegistering)
		if err := f.d.Configure(ctx, link.ManagementURL, f.cfg.Iface, f.cfg.Port); err != nil {
			return Result{}, &Error{Kind: KindUnknown, Detail: "настройка службы", Err: err}
		}
	} else {
		progress(StageConnecting)
	}
	browser, err := f.d.Login(ctx, link.SetupKey, link.ManagementURL)
	if err != nil {
		if canceled(ctx, err) {
			return Result{}, cancelError(ctx, "регистрация прервана", err)
		}
		return Result{}, &Error{Kind: f.refine(ctx, classifyLogin(err)), Detail: "регистрация", Err: err}
	}
	if browser {
		return Result{}, &Error{Kind: KindNeedsBrowserLogin, Detail: "сервер просит вход через браузер"}
	}

	box := Box{
		Identity:      link.BoxIdentity(),
		Name:          link.DisplayName(),
		ManagementURL: link.ManagementURL,
		ExitNode:      link.ExitNode,
		RegisteredAt:  time.Now().UTC(),
	}
	saved := exitpick.Saved{}
	if same {
		saved = exitpick.Saved{ID: stored.ExitNode, NoExit: stored.NoExit}
		if stored.ExitNode != "" {
			box.ExitNode = stored.ExitNode
		}
		box.NoExit = stored.NoExit
		if !fresh {
			box.RegisteredAt = stored.RegisteredAt
		}
	}
	if err := f.store.Save(box); err != nil {
		return Result{}, &Error{Kind: KindUnknown, Detail: "не сохранить коробку", Err: err}
	}

	if fresh {
		progress(StageConnecting)
	}
	if err := f.d.Up(ctx); err != nil {
		if canceled(ctx, err) {
			return Result{}, f.abortUp(ctx, err)
		}
		return Result{}, f.upError(ctx, err, KindLinkExpired)
	}

	progress(StageChoosingExit)
	res, err := f.pickExit(ctx, link.ExitNode, saved)
	if err != nil {
		if canceled(ctx, err) {
			return Result{}, f.abortUp(ctx, err)
		}
		return Result{}, err
	}
	switch {
	case res.NeedsChoice:
	case res.NoExit:
		f.rememberNoExit()
		progress(StageProtected)
	default:
		f.rememberExit(res.ExitID)
		progress(StageProtected)
	}
	return res, nil
}

// canceled — подключение отменено: вызывающим (ctx) или самой службой (gRPC Canceled).
func canceled(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled
}

// cancelError — отказ вида Unknown, по которому отмену видно через
// errors.Is(err, context.Canceled) (или DeadlineExceeded, если истёк срок ctx).
func cancelError(ctx context.Context, detail string, err error) *Error {
	cause := ctx.Err()
	switch {
	case cause == nil && errors.Is(err, context.Canceled):
		cause = err
	case cause == nil:
		cause = fmt.Errorf("%w: %v", context.Canceled, err)
	}
	return &Error{Kind: KindUnknown, Detail: detail, Err: cause}
}

// abortUp — отмена после Up: служба продолжала бы подключаться в фоне, поэтому
// опускаем её контекстом, который отмена не задела (не дольше 10 с).
func (f *Flow) abortUp(ctx context.Context, err error) *Error {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = f.d.Down(dctx)
	return cancelError(ctx, "подключение прервано", err)
}

// rememberExit записывает выбранный выход в запомненную коробку (если она есть)
// и сбрасывает «без выхода». Ошибка записи не мешает: выход уже выбран в службе.
func (f *Flow) rememberExit(id string) {
	if b, ok, err := f.store.Load(); err == nil && ok && (b.ExitNode != id || b.NoExit) {
		b.ExitNode, b.NoExit = id, false
		_ = f.store.Save(b)
	}
}

// rememberNoExit запоминает «без выхода — только сети».
func (f *Flow) rememberNoExit() {
	if b, ok, err := f.store.Load(); err == nil && ok && (b.ExitNode != "" || !b.NoExit) {
		b.ExitNode, b.NoExit = "", true
		_ = f.store.Save(b)
	}
}

// pickExit ждёт маршруты от management и выбирает выход по §4.3; saved —
// выбор человека, запомненный в коробке. Запомненное «без выхода» ждёт, пока
// выходы появятся, и снимает их все (только их ID — сети клиентов остаются).
// Если служба так и не отдала список сетей — это отказ, а не «выберите выход».
func (f *Flow) pickExit(ctx context.Context, e string, saved exitpick.Saved) (Result, error) {
	deadline := time.Now().Add(f.cfg.ExitWait)
	var (
		choice  exitpick.Choice
		gotList bool
		lastErr error
	)
	for {
		nets, err := f.d.Networks(ctx)
		if err == nil {
			gotList = true
			choice = exitpick.Pick(nets, e, saved)
			if choice.ID != "" || len(choice.Candidates) > 1 || (choice.NoExit && len(choice.Exits) > 0) {
				break
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, &Error{Kind: KindUnknown, Detail: "ожидание выхода прервано", Err: ctx.Err()}
		case <-time.After(f.cfg.ExitPoll):
		}
	}
	if !gotList {
		return Result{}, &Error{Kind: KindUnknown, Detail: "список сетей", Err: lastErr}
	}
	if choice.NoExit {
		if len(choice.Exits) > 0 {
			if err := f.d.DeselectExits(ctx, choice.Exits); err != nil {
				return Result{}, &Error{Kind: KindUnknown, Detail: "выбор выхода", Err: err}
			}
		}
		return Result{NoExit: true}, nil
	}
	if choice.ID == "" {
		return Result{NeedsChoice: true, Candidates: choice.Candidates}, nil
	}
	if err := f.d.SelectExit(ctx, choice.ID); err != nil {
		return Result{}, &Error{Kind: KindUnknown, Detail: "выбор выхода", Err: err}
	}
	return Result{ExitID: choice.ID}, nil
}

// ChooseExit — человек выбрал выход на экране «Выберите выход»; выбор запоминается.
func (f *Flow) ChooseExit(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.d.SelectExit(ctx, id); err != nil {
		return &Error{Kind: KindUnknown, Detail: "выбор выхода", Err: err}
	}
	f.rememberExit(id)
	return nil
}

// ChooseNoExit — «Без выхода — только сети»: снимаем все выходы (только их ID,
// не all — сети клиентов остаются) и запоминаем выбор.
func (f *Flow) ChooseNoExit(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	nets, err := f.d.Networks(ctx)
	if err != nil {
		return &Error{Kind: KindUnknown, Detail: "список сетей", Err: err}
	}
	var ids []string
	for _, c := range exitpick.Exits(nets) {
		ids = append(ids, c.ID)
	}
	if len(ids) > 0 {
		if err := f.d.DeselectExits(ctx, ids); err != nil && !f.allExitsOff(ctx) {
			// Ядро снимает допустимую часть списка и при ошибке (ID мог исчезнуть
			// между Networks и Deselect): отказ — только если выходы правда остались.
			return &Error{Kind: KindUnknown, Detail: "выбор выхода", Err: err}
		}
	}
	f.rememberNoExit()
	return nil
}

// allExitsOff — по свежему списку сетей ни один выход не включён.
func (f *Flow) allExitsOff(ctx context.Context) bool {
	nets, err := f.d.Networks(ctx)
	if err != nil {
		return false
	}
	for _, c := range exitpick.Exits(nets) {
		if c.Selected {
			return false
		}
	}
	return true
}

// Exits — выходы для экрана «Выберите выход» из главного: с пометкой, какой
// включён ядром сейчас.
func (f *Flow) Exits(ctx context.Context) ([]exitpick.Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nets, err := f.d.Networks(ctx)
	if err != nil {
		return nil, &Error{Kind: KindUnknown, Detail: "список сетей", Err: err}
	}
	return exitpick.Exits(nets), nil
}

// Disconnect опускает туннель, регистрация и коробка остаются.
func (f *Flow) Disconnect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.d.Down(ctx); err != nil {
		return &Error{Kind: f.refine(ctx, classifyLogin(err)), Detail: "отключение", Err: err}
	}
	return nil
}

// Reconnect поднимает туннель к запомненной коробке без ссылки.
//
// NeedsLogin/SessionExpired — management отверг регистрацию: KindNeedsLink
// (нужна ссылка), Up не вызывается. LoginFailed — прошлая проверка входа не
// прошла (сеть): повторяем Login без ключа — служба проверит прежнюю
// регистрацию и вернёт OK, если пир жив (server.go:769-777); если он не
// принят — просит браузер или сообщает, что SSO нет: тогда KindNeedsLink.
func (f *Flow) Reconnect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.d.Status(ctx)
	if err != nil {
		return &Error{Kind: KindServiceMissing, Detail: "служба не отвечает", Err: err}
	}
	switch st.GetStatus() {
	case "LoginFailed":
		box, _, _ := f.store.Load() // нет коробки — пустой адрес: служба возьмёт свой из профиля
		browser, err := f.d.Login(ctx, "", box.ManagementURL)
		if err != nil {
			if canceled(ctx, err) {
				return cancelError(ctx, "проверка входа прервана", err)
			}
			if noSSO(err) {
				return &Error{Kind: KindNeedsLink, Detail: "регистрация не принята", Err: err}
			}
			return f.netError(ctx, "проверка входа", err)
		}
		if browser {
			return &Error{Kind: KindNeedsLink, Detail: "регистрация не принята"}
		}
	default:
		if needsLogin(st.GetStatus()) {
			return &Error{Kind: KindNeedsLink, Detail: "служба не зарегистрирована"}
		}
	}
	if err := f.d.Up(ctx); err != nil {
		if canceled(ctx, err) {
			return f.abortUp(ctx, err)
		}
		return f.upError(ctx, err, KindNeedsLink)
	}
	// Защита включена; если карта уже пришла — вернуть запомненный выбор. Не
	// пришла — сверит опрос панели (Reconcile). Ошибка сверки включению не мешает.
	_ = f.reconcile(ctx)
	return nil
}

// Reconcile сверяет выбор выхода в ядре с запомненным в коробке: «без выхода»,
// а ядро включило выход — снять выходы; в коробке выход, он есть в списке, а
// включён другой (или никакой) — включить его. Нужно, когда ядро выбрало само
// (новый выход без SkipAutoApply), потерян его state.json или выбор изменили
// из другого клиента. Карты нет (выходов в списке нет) — ничего не делает.
func (f *Flow) Reconcile(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reconcile(ctx)
}

func (f *Flow) reconcile(ctx context.Context) error {
	box, ok, err := f.store.Load()
	if err != nil || !ok || (!box.NoExit && box.ExitNode == "") {
		return err
	}
	nets, err := f.d.Networks(ctx)
	if err != nil {
		return &Error{Kind: KindUnknown, Detail: "список сетей", Err: err}
	}
	exits := exitpick.Exits(nets)
	var ids []string
	anySelected, have, haveSelected := false, false, false
	for _, c := range exits {
		ids = append(ids, c.ID)
		anySelected = anySelected || c.Selected
		if c.ID == box.ExitNode {
			have, haveSelected = true, c.Selected
		}
	}
	switch {
	case box.NoExit && anySelected:
		err = f.d.DeselectExits(ctx, ids)
	case !box.NoExit && have && !haveSelected:
		err = f.d.SelectExit(ctx, box.ExitNode)
	}
	if err != nil {
		return &Error{Kind: KindUnknown, Detail: "выбор выхода", Err: err}
	}
	return nil
}

// noSSO — Login без ключа: management не принял пира и не умеет вход через
// браузер (client/internal/auth/oauth.go:145-151). Нужна ссылка с ключом.
func noSSO(err error) bool {
	msg := strings.ToLower(err.Error())
	return containsAny(msg, "no sso provider", "does not support sso")
}

// Forget снимает устройство с учёта и забывает коробку. Коробка забывается
// ВСЕГДА, даже если Logout не прошёл (нет сети и т. п.): вызывающий считает её
// забытой, а ошибка (ServerUnreachable/NoInternet) лишь сообщает, что запись
// пира на сервере могла остаться.
func (f *Flow) Forget(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.d.Down(ctx)
	logoutErr := f.d.Logout(ctx)
	if err := f.store.Clear(); err != nil {
		return &Error{Kind: KindUnknown, Detail: "не забыть коробку", Err: err}
	}
	if logoutErr != nil {
		return f.netError(ctx, "снятие с учёта", logoutErr)
	}
	return nil
}

// Box — запомненная коробка (для экрана без ссылки).
func (f *Flow) Box() (Box, bool, error) { return f.store.Load() }
