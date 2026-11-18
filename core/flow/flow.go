// Package flow — сценарий «одной кнопки» поверх службы JetBox.
// Порядок вызовов и правила — LINK-CONTRACT §4 и живой прогон спайка Windows 26.10.2026.
package flow

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/netbirdio/netbird/client/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dsm150/jetbox-desktop/core/daemonclient"
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
	// Профили службы: новая личность устройства (ключ WireGuard) — новый профиль.
	ActiveProfile(ctx context.Context) (id, username string, err error)
	AddProfile(ctx context.Context, name string) (id, username string, err error)
	// ConfigureProfile — Configure адресно в новый профиль id, активным он не
	// становится; служба не поднимает его сама при старте (DisableAutoConnect).
	ConfigureProfile(ctx context.Context, id, username, managementURL, iface string, port int64) error
	// LoginProfile — Login в профиль id: служба делает его активным сама, до входа,
	// и при отказе не возвращает прежний.
	LoginProfile(ctx context.Context, id, username, setupKey, managementURL string) (needsBrowser bool, err error)
	SwitchProfile(ctx context.Context, id, username string) error
	RemoveProfile(ctx context.Context, id, username string) error
	// ListProfiles — профили пользователя username (пусто — пользователя активного).
	ListProfiles(ctx context.Context, username string) ([]daemonclient.Profile, error)
	Networks(ctx context.Context) ([]exitpick.Network, error)
	SelectExit(ctx context.Context, id string) error
	DeselectExits(ctx context.Context, ids []string) error
	SetDisableAutoConnect(ctx context.Context, disable bool) error
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
	// Logf — журнал сбоев, которые действию не мешают (флаг автоподключения); nil — молча.
	Logf func(format string, args ...any)
	// CurrentUser — пользователь ОС, на которого служба заводит новый профиль замены
	// (daemonclient.AddProfile); nil — daemonclient.CurrentUser.
	CurrentUser func() (string, error)
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
// Connect, ChooseExit, ChooseNoExit, Exits, Reconcile, Disconnect, Reconnect, SetAutoConnect, SyncAutoConnect и Forget выполняются строго по
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
// Up не дождался подключения (тайм-аут службы), а служба подключается дальше —
// защита включена: флаг автоподключения по галочке, как после удачного Up.
func (f *Flow) upError(ctx context.Context, err error, onNeedsLogin Kind) *Error {
	if st, serr := f.d.Status(ctx); serr == nil {
		switch s := st.GetStatus(); {
		case needsLogin(s):
			return &Error{Kind: onNeedsLogin, Detail: "подключение", Err: err}
		case s == "Connected" || s == "Connecting":
			f.syncAutoConnect(ctx, true)
		}
	}
	return &Error{Kind: f.refine(ctx, classifyLogin(err)), Detail: "подключение", Err: err}
}

// normManagement приводит адрес management к виду для сравнения: схема и хост
// в нижнем регистре, порт по умолчанию (443 для https, 80 для http), завершающая точка
// хоста и хвостовой слэш убраны. Служба отдаёт адрес из профиля с портом
// (`https://netbird.jet-net.net:443`), ссылка — без (`https://netbird.jet-net.net`).
func normManagement(raw string) string {
	s := strings.TrimSpace(raw)
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimRight(strings.ToLower(s), "/")
	}
	scheme := strings.ToLower(u.Scheme)
	host, port := strings.TrimSuffix(strings.ToLower(u.Hostname()), "."), u.Port()
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

// SameConnection — ссылка ведёт к тому же подключению, что запомнено: тот же
// management и тот же setup key (по отпечатку, как JetBoxIdentity.plan в iOS).
// Запись до 1.0.5 без отпечатка — по удостоверению (SameBox). Отпечаток, когда он
// есть, решает раньше удостоверения. Не то же — нужна новая личность устройства и вопрос
// «заменить подключение?».
func SameConnection(b Box, link jetboxlink.Payload) bool {
	if b.KeyFingerprint == "" {
		return SameBox(b, link)
	}
	return sameManagement(b.ManagementURL, link.ManagementURL) && b.KeyFingerprint == link.KeyFingerprint()
}

// SameBox — ссылка той же коробки (того же облачного абонента), что запомнена: по
// удостоверению (LINK-CONTRACT 1.3 §2.11, jetboxlink.SameIdentity). Записи любого формата
// (`хост|e` до 1.0.6, `хост|t:…` облака 1.0.5) разбираются из Identity: ExitNode и Name
// коробки — выбор человека и показное имя, не поля ссылки. Да — без вопроса о замене.
func SameBox(b Box, link jetboxlink.Payload) bool {
	return jetboxlink.SameIdentity(jetboxlink.ParseIdentity(b.Identity), link.IdentityFields())
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

// Connect — нажатие «Подключить» по ссылке.
//
// Порядок: сверка профиля службы (recover) → Status → чужой VPN → запомненная
// коробка. Личность устройства в NetBird — ключ WireGuard в профиле службы: вход
// новым setup key со старым ключом management не применит (узнает пир по ключу).
// Поэтому, как JetBoxIdentity.plan в iOS:
//   - та же ссылка (SameConnection) — Login тем же ключом в текущем профиле: служба
//     вернёт OK без ключа, если пир жив, и перерегистрирует, если его удалили;
//     служба не зарегистрирована — Configure и Login там же;
//   - другая ссылка — только с подтверждением и всегда новой личностью: новый
//     профиль службы (replace) с откатом при неудаче, каков бы ни был статус службы;
//   - пустое хранилище: служба не зарегистрирована — первый вход в текущем профиле;
//     зарегистрирована на другой management — новый профиль без вопроса; тот же
//     management и выход ссылки виден службе — та же коробка, принимаем как есть;
//     иначе «другая JetBox» с подтверждением и новым профилем.
//
// Logout при замене не зовётся: прежний пир убирает Кабинет. Коробка запоминается
// сразу после Login, с профилем службы. Отмена ctx после Up опускает службу (abortUp).
func (f *Flow) Connect(ctx context.Context, link jetboxlink.Payload, opts Options, progress func(Stage)) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if progress == nil {
		progress = func(Stage) {}
	}
	rec, err := f.recover(ctx)
	if err != nil {
		return Result{}, err
	}
	cur := rec.cur
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
	if rec.staleBox {
		// Профиля коробки в службе нет (переустановка): как будто коробки нет.
		stored, has = Box{}, false
	}
	daemonStatus := st.GetStatus()
	registered := !needsLogin(daemonStatus)
	wasOn := daemonStatus == "Connected" || daemonStatus == "Connecting"
	same := has && SameConnection(stored, link)
	sameBox := has && SameBox(stored, link)
	if same && daemonStatus == "LoginFailed" {
		// LoginFailed — не прошла проверка входа (сеть, сбой management;
		// server.go:769-772), а не отказ в регистрации: она, скорее всего, жива.
		registered = true
	}
	adopt, replace := false, false // adopt — своя коробка, забытая хранилищем
	switch {
	case has && !same:
		// LINK-CONTRACT 1.2 §2.11: то же удостоверение (та же коробка, новая ссылка) —
		// без вопроса, как iOS/Android; другое — только с подтверждением.
		if !sameBox && !opts.ConfirmSwitch {
			return Result{}, &Error{Kind: KindOtherBox, Detail: stored.Name}
		}
		replace = true
	case !has && registered:
		// Служба зарегистрирована, а мы ничего не помним. Служба называет свой
		// management (из профиля; server.go:1898); пусто — неизвестен, считаем тем же.
		daemonMgmt := st.GetFullStatus().GetManagementState().GetURL()
		switch {
		case daemonMgmt != "" && !sameManagement(daemonMgmt, link.ManagementURL):
			// Другой management (например, api.netbird.io:443 свежего профиля):
			// спрашивать не о чем, регистрируемся в новом профиле.
			replace = true
		case f.hasExit(ctx, link.ExitNode):
			// Тот же management и выход коробки виден службе — та же коробка
			// (второй пользователь Windows, потерянный %APPDATA%): принимаем
			// регистрацию как есть, без Configure.
			adopt = true
		default:
			if !opts.ConfirmSwitch {
				return Result{}, &Error{Kind: KindOtherBox, Detail: ""}
			}
			replace = true
		}
	}

	if replace {
		prof, j, err := f.replace(ctx, link, cur, wasOn, progress)
		if err != nil {
			return Result{}, err
		}
		return f.finish(ctx, link, stored, has, sameBox, true, prof, &j, progress)
	}

	// Новая регистрация — всё, кроме своей (или принятой) коробки на живой регистрации.
	fresh := !(same || adopt) || !registered
	if fresh {
		progress(StageRegistering)
		if err := f.d.Configure(ctx, link.ManagementURL, f.cfg.Iface, f.cfg.Port); err != nil {
			return Result{}, &Error{Kind: configKind(err), Detail: "настройка службы", Err: err}
		}
	} else {
		progress(StageConnecting)
	}
	if err := f.login(ctx, link); err != nil {
		return Result{}, err
	}
	return f.finish(ctx, link, stored, has, sameBox, fresh, cur, nil, progress)
}

// configKind — вид отказа SetConfig: отказ службы по правам (патч 0008: management вне
// маски) — KindNeedsAdmin, прочее — Unknown.
func configKind(err error) Kind {
	if NeedsPrivilege(err) {
		return KindNeedsAdmin
	}
	return KindUnknown
}

// profileRef — профиль службы: ID (у старой службы — имя) и пользователь.
type profileRef struct{ ID, User string }

// defaultProfile — профиль службы, который есть всегда; им считается коробка без
// ProfileID (до 1.0.5 профили были выключены).
const defaultProfile = "default"

// boxProfile — профиль службы, в котором зарегистрирована коробка.
func boxProfile(b Box) profileRef {
	if b.ProfileID == "" {
		return profileRef{ID: defaultProfile}
	}
	return profileRef{ID: b.ProfileID, User: b.ProfileUser}
}

// Recover доводит прерванную замену подключения (см. recover). Для запуска
// приложения; Connect и Reconnect делают это сами. changed — служба тронута
// (переключён профиль или удалён новый): экран стоит обновить.
func (f *Flow) Recover(ctx context.Context) (changed bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.recover(ctx)
	return r.changed, err
}

// recovered — итог сверки: активный профиль после неё, коробка устарела (её профиля
// в службе нет — переустановка с оставшимся %APPDATA%), служба тронута.
type recovered struct {
	cur      profileRef
	staleBox bool
	changed  bool
}

// recover сверяет активный профиль службы с коробкой и доводит прерванную замену.
//
// Без replace.json службу НЕ трогает: другой активный профиль — это может быть
// второй пользователь Windows (служба одна на компьютер) или переустановка; только
// пишет расхождение в журнал. Профиля коробки в службе нет — коробка устарела
// (staleBox: Connect считает, что коробки нет).
//
// С replace.json (замену прервал выход приложения или сбой питания — этот же
// пользователь) — довести откат: активен не профиль коробки (коробки нет или она
// устарела — прежний из записи; и его нет — default) — Down и SwitchProfile на него;
// защита была включена до замены (WasOn) — Up; затем удалить недорегистрированный новый
// профиль (по ID, или по имени из ListProfiles, если AddProfile прервали), кроме профиля
// коробки.
// Запись стирается, только когда нового профиля точно нет.
//
// Служба не отвечает — KindServiceMissing; не удалось переключить — KindUnknown,
// запись остаётся до следующей попытки.
func (f *Flow) recover(ctx context.Context) (recovered, error) {
	id, user, err := f.d.ActiveProfile(ctx)
	if err != nil {
		return recovered{}, &Error{Kind: KindServiceMissing, Detail: "служба не отвечает", Err: err}
	}
	r := recovered{cur: profileRef{ID: id, User: user}}
	j, hasJ := f.store.LoadReplace()
	box, hasBox, _ := f.store.Load()
	boxProf := boxProfile(box)
	if hasBox && boxProf.ID != r.cur.ID {
		if found, known := f.profileExists(ctx, boxProf); known && !found {
			f.logf("сверка профиля: профиля коробки %s в службе нет — коробка устарела", boxProf.ID)
			r.staleBox = true
		} else if !hasJ {
			f.logf("сверка профиля: активен %s (%s), коробка в %s (%s) — службу не трогаем",
				r.cur.ID, r.cur.User, boxProf.ID, boxProf.User)
		}
	}
	if !hasJ {
		return r, nil
	}

	target := profileRef{ID: j.OldID, User: j.OldUser}
	if hasBox && !r.staleBox {
		target = boxProf
	} else if found, known := f.profileExists(ctx, target); known && !found {
		target = profileRef{ID: defaultProfile}
	}
	if r.cur.ID != target.ID {
		f.logf("сверка профиля: прерванная замена — активен %s, возвращаем %s", r.cur.ID, target.ID)
		_ = f.d.Down(ctx)
		if err := f.d.SwitchProfile(ctx, target.ID, target.User); err != nil {
			return r, &Error{Kind: KindUnknown, Detail: "возврат профиля службы", Err: err}
		}
		r.cur, r.changed = target, true
	}
	if j.WasOn {
		// Любой обрыв возвращает состояние до замены: защита была включена — поднять.
		uctx, ucancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackUp)
		if err := f.d.Up(uctx); err != nil {
			f.logf("сверка профиля: включить прежнее подключение: %v", err)
		}
		ucancel()
		r.changed = true
	}
	f.logf("сверка профиля: доводим прерванную замену (новый профиль %s %s)", j.NewName, j.NewID)
	removed, gone := f.removeNew(ctx, j, r.cur.ID, target.ID)
	r.changed = r.changed || removed
	if gone {
		if err := f.store.ClearReplace(); err != nil {
			f.logf("сверка профиля: запись о замене: %v", err)
		}
	} else if j.WasOn {
		j.WasOn = false // поднято; следующая сверка только уберёт новый профиль
		if err := f.store.SaveReplace(j); err != nil {
			f.logf("сверка профиля: запись о замене: %v", err)
		}
	}
	return r, nil
}

// profileExists — есть ли профиль в службе (ListProfiles его пользователя). known=false —
// узнать не удалось. default есть всегда.
func (f *Flow) profileExists(ctx context.Context, p profileRef) (found, known bool) {
	if p.ID == defaultProfile {
		return true, true
	}
	list, err := f.d.ListProfiles(ctx, p.User)
	if err != nil {
		f.logf("сверка профиля: список профилей: %v", err)
		return false, false
	}
	for _, q := range list {
		if q.ID == p.ID {
			return true, true
		}
	}
	return false, true
}

// removeNew удаляет новый профиль незаконченной замены — его пир не зарегистрирован
// (или регистрация брошена). По ID, а если AddProfile прервали до ответа — по имени из
// ListProfiles. Профили keep и default не трогает. RemoveProfile ходит в management
// снимать пир, поэтому — своим коротким контекстом (5 с), не держа замену; ошибки —
// в журнал. removed — что-то удалено; gone — нового профиля точно нет (удалён, не
// найден или его оставили как профиль коробки) или удалить его нельзя без прав
// (зарегистрирован — служба отказала с PRIVILEGE_REQUIRED, повтор не поможет): запись о
// замене можно стереть.
func (f *Flow) removeNew(ctx context.Context, j Replace, keep ...string) (removed, gone bool) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	// Новый профиль заводится на вызывающего (daemonclient.AddProfile), не на пользователя
	// прежнего: запись без NewUser (1.0.5) — ищем у текущего пользователя ОС.
	user := j.NewUser
	if user == "" {
		user = f.currentUser()
	}
	if user == "" {
		user = j.OldUser
	}
	var ids []string
	switch {
	case j.NewID != "":
		ids = []string{j.NewID}
	case j.NewName != "":
		list, err := f.d.ListProfiles(rctx, user)
		if err != nil {
			f.logf("замена: найти профиль %s: %v", j.NewName, err)
			return false, false
		}
		for _, p := range list {
			if p.Name == j.NewName {
				ids = append(ids, p.ID)
			}
		}
	}
	gone = true
	for _, id := range ids {
		if id == defaultProfile || slices.Contains(keep, id) {
			continue
		}
		err := f.d.RemoveProfile(rctx, id, user)
		switch {
		case err == nil:
			removed = true
		case status.Code(err) == codes.NotFound:
		case NeedsPrivilege(err):
			// Служба (патч 0008) удаляет без прав только свой незарегистрированный профиль:
			// этот успел зарегистрироваться (замену прервали после входа). Повтор ничего не
			// даст — оставляем профиль (лишний файл, пир уберёт Кабинет), запись о замене
			// стирается: иначе сверка повторяла бы удаление на каждом запуске.
			f.logf("замена: профиль %s зарегистрирован, удалить без прав администратора нельзя — оставляем", id)
		case f.profileGone(ctx, id, user):
			// RemoveProfile ждёт management, чтобы снять пир, и при отмене удаляет профиль
			// локально (server.go RemoveProfile): тайм-аут ещё не значит, что профиль остался.
			removed = true
		default:
			f.logf("замена: удалить профиль %s: %v", id, err)
			gone = false
		}
	}
	return removed, gone
}

// profileGone — профиля id у службы больше нет (переспрос после неудачного RemoveProfile).
func (f *Flow) profileGone(ctx context.Context, id, user string) bool {
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	list, err := f.d.ListProfiles(lctx, user)
	if err != nil {
		return false
	}
	return !slices.ContainsFunc(list, func(p daemonclient.Profile) bool { return p.ID == id })
}

// Тайм-ауты отката: Down/SwitchProfile, Up прежнего, удаление нового профиля.
const (
	rollbackTimeout = 15 * time.Second
	rollbackUp      = 20 * time.Second
	removeTimeout   = 5 * time.Second
)

// login — вход ключом ссылки в активный профиль.
func (f *Flow) login(ctx context.Context, link jetboxlink.Payload) error {
	browser, err := f.d.Login(ctx, link.SetupKey, link.ManagementURL)
	return f.loginError(ctx, browser, err)
}

// loginError сводит итог Login к отказу (classifyLogin, refine); nil — вход прошёл.
func (f *Flow) loginError(ctx context.Context, browser bool, err error) error {
	if err != nil {
		if canceled(ctx, err) {
			return cancelError(ctx, "регистрация прервана", err)
		}
		return &Error{Kind: f.refine(ctx, classifyLogin(err)), Detail: "регистрация", Err: err}
	}
	if browser {
		return &Error{Kind: KindNeedsBrowserLogin, Detail: "сервер просит вход через браузер"}
	}
	return nil
}

// replace — замена подключения новой личностью: новый профиль службы (служба
// создаёт его сразу с новым ключом WireGuard), Configure адресно в него, Login
// ключом ссылки в него, и только после удачного входа — SwitchProfile на него.
// cur — активный профиль до замены. Возвращает новый профиль.
//
// Login с именем профиля служба выполняет, сделав его активным (server.go
// authorizeAndPrepareLogin → switchProfileIfNeeded), и при отказе не возвращает
// прежний: это делает откат. Конфиг прежнего профиля не меняется ни на одном шаге.
//
// Перед AddProfile пишется replace.json (Replace): прервут замену — откат доведёт
// recover. Удаляется после сохранения коробки (finish) или после отката.
//
// Прежний профиль и его пир не трогаем: ни Logout, ни RemoveProfile (служба при
// удалении профиля снимает его пир с учёта) — пир уберёт Кабинет.
//
// Точка фиксации (LINK-CONTRACT 1.3 §2.11) — сохранение коробки в finish. Любой обрыв
// до неё (отказ или отмена шага, сбой SwitchProfile после входа, сбой Save) — откат к
// состоянию до замены (rollback): прежний профиль, Up, если защита была включена,
// box.json прежний. После фиксации — обычная ошибка подключения, без отката.
// Возвращает новый профиль и запись о замене (для отката из finish).
func (f *Flow) replace(ctx context.Context, link jetboxlink.Payload, cur profileRef, wasOn bool, progress func(Stage)) (profileRef, Replace, error) {
	progress(StageRegistering)
	j := Replace{OldID: cur.ID, OldUser: cur.User, NewName: fmt.Sprintf("jetbox-%d", time.Now().Unix()),
		WasOn: wasOn, At: time.Now().UTC()}
	// Пользователь нового — заранее: прервут AddProfile до ответа — профиль ищут по имени у него.
	j.NewUser = f.currentUser()
	if err := f.store.SaveReplace(j); err != nil {
		return profileRef{}, j, &Error{Kind: KindUnknown, Detail: "запись о замене", Err: err}
	}
	_ = f.d.Down(ctx)
	newID, newUser, err := f.d.AddProfile(ctx, j.NewName)
	if err != nil {
		f.rollback(ctx, j, false)
		return profileRef{}, j, f.stepError(ctx, "новый профиль службы", err)
	}
	j.NewID, j.NewUser = newID, newUser
	if err := f.store.SaveReplace(j); err != nil {
		f.logf("замена: запись о замене: %v", err) // без ID новый найдётся по имени
	}
	if err := f.d.ConfigureProfile(ctx, newID, newUser, link.ManagementURL, f.cfg.Iface, f.cfg.Port); err != nil {
		f.rollback(ctx, j, false)
		return profileRef{}, j, f.stepError(ctx, "настройка службы", err)
	}
	browser, err := f.d.LoginProfile(ctx, newID, newUser, link.SetupKey, link.ManagementURL)
	if err := f.loginError(ctx, browser, err); err != nil {
		f.rollback(ctx, j, true)
		return profileRef{}, j, err
	}
	// Login уже сделал новый профиль активным; явный SwitchProfile — чтобы служба
	// перечитала его конфиг. Сбой — до фиксации, значит откат: новый пир уже
	// зарегистрирован, removeNew его не удалит без прав (журнал), уберёт Кабинет.
	if err := f.d.SwitchProfile(ctx, newID, newUser); err != nil {
		f.logf("замена: переключение на профиль %s после входа: %v — откат", newID, err)
		f.rollback(ctx, j, true)
		return profileRef{}, j, f.stepError(ctx, "переключение на новый профиль", err)
	}
	return profileRef{ID: newID, User: newUser}, j, nil
}

// stepError — отказ шага замены: отмена видна через errors.Is, прочее — Unknown.
func (f *Flow) stepError(ctx context.Context, detail string, err error) error {
	if canceled(ctx, err) {
		return cancelError(ctx, "замена подключения прервана", err)
	}
	return &Error{Kind: configKind(err), Detail: detail, Err: err}
}

// rollback возвращает всё, как было до замены j, контекстами, которые отмена не
// задела:
//  1. Down — после неудачного Login служба остаётся в LoginFailed, и Up ответил бы
//     «up already in progress» (server.go:770-799, 1197); Down сбрасывает в Idle;
//  2. SwitchProfile на прежний — если служба могла сделать активным новый (switchBack:
//     был Login). Не вышло — стоп: запись о замене остаётся, откат доведёт recover;
//  3. Up прежнего со своим тайм-аутом — если защита была включена (j.WasOn);
//  4. RemoveProfile нового последним, коротким контекстом (removeNew).
//
// Ошибки — только в журнал: человек увидит отказ самой замены. Запись о замене остаётся,
// пока новый профиль не убран (тогда — уже без WasOn: прежнее подключение поднято).
func (f *Flow) rollback(ctx context.Context, j Replace, switchBack bool) {
	base := context.WithoutCancel(ctx)
	rctx, cancel := context.WithTimeout(base, rollbackTimeout)
	defer cancel()
	_ = f.d.Down(rctx)
	if switchBack {
		if err := f.d.SwitchProfile(rctx, j.OldID, j.OldUser); err != nil {
			f.logf("откат замены: прежний профиль %s: %v", j.OldID, err)
			return
		}
	}
	if j.WasOn {
		uctx, ucancel := context.WithTimeout(base, rollbackUp)
		if err := f.d.Up(uctx); err != nil {
			f.logf("откат замены: включить прежнее подключение: %v", err)
		}
		ucancel()
	}
	if _, gone := f.removeNew(ctx, j, j.OldID); !gone {
		// Новый профиль остался — запись нужна следующей сверке; прежнее уже поднято.
		j.WasOn = false
		if err := f.store.SaveReplace(j); err != nil {
			f.logf("откат замены: запись о замене: %v", err)
		}
		return
	}
	if err := f.store.ClearReplace(); err != nil {
		f.logf("откат замены: запись о замене: %v", err)
	}
}

// currentUser — пользователь ОС вызывающего (Config.CurrentUser); не узнать — пусто.
func (f *Flow) currentUser() string {
	get := f.cfg.CurrentUser
	if get == nil {
		get = daemonclient.CurrentUser
	}
	u, err := get()
	if err != nil {
		f.logf("замена: пользователь ОС: %v", err)
		return ""
	}
	return u
}

func (f *Flow) logf(format string, args ...any) {
	if f.cfg.Logf != nil {
		f.cfg.Logf(format, args...)
	}
}

// finish — после успешного входа: запомнить коробку (с отпечатком ключа и профилем
// службы prof), поднять туннель и выбрать выход. keepChoice — та же коробка
// (удостоверение): выбор выхода человека сохраняется. fresh — новая регистрация (дата —
// сейчас). rep — запись о замене (nil — не замена): сохранение коробки — точка фиксации;
// сбой Save — откат замены (box.json прежний), после Save запись удаляется, и дальше
// (Up, выбор выхода) — обычные ошибки подключения, без отката.
func (f *Flow) finish(ctx context.Context, link jetboxlink.Payload, stored Box, has, keepChoice, fresh bool,
	prof profileRef, rep *Replace, progress func(Stage)) (Result, error) {
	box := Box{
		Identity:      link.BoxIdentity(),
		Name:          link.DisplayName(),
		ManagementURL: link.ManagementURL,
		ExitNode:      link.ExitNode,
		RegisteredAt:  time.Now().UTC(),
		// Галочка — выбор человека для компьютера: переживает и смену коробки.
		AutoConnect:    !has || stored.AutoConnect,
		KeyFingerprint: link.KeyFingerprint(),
		ProfileID:      prof.ID,
		ProfileUser:    prof.User,
	}
	saved := exitpick.Saved{}
	if keepChoice {
		saved = exitpick.Saved{ID: stored.ExitNode, NoExit: stored.NoExit}
		if stored.ExitNode != "" {
			box.ExitNode = stored.ExitNode
		}
		box.NoExit = stored.NoExit
	}
	if has && !fresh {
		box.RegisteredAt = stored.RegisteredAt
	}
	if err := f.store.Save(box); err != nil {
		if rep != nil {
			f.logf("замена: не сохранить коробку: %v — откат", err)
			f.rollback(ctx, *rep, true)
		}
		return Result{}, &Error{Kind: KindUnknown, Detail: "не сохранить коробку", Err: err}
	}
	if rep != nil {
		if err := f.store.ClearReplace(); err != nil {
			f.logf("замена: запись о замене: %v", err)
		}
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
	f.syncAutoConnect(ctx, true)

	if link.NoDefaultExit && !keepChoice {
		// LINK-CONTRACT 1.2 §2.10: x=0 — выход не включать и не выбирать (облако:
		// выход заводится с skip_auto_apply и выбирается позже, в окне).
		f.rememberNoExit()
		progress(StageProtected)
		return Result{NoExit: true}, nil
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
	if f.d.Down(dctx) == nil {
		f.syncAutoConnect(dctx, false)
	}
	return cancelError(ctx, "подключение прервано", err)
}

// syncAutoConnect держит флаг службы «не подключаться при старте» равным
// НЕ(AutoConnect коробки И защита включена человеком): on — защиту только что
// включили (Up), иначе её выключили. Нет коробки — флаг ставится. Ошибка действию
// не мешает: только в журнал (служба при старте поступит по прежнему флагу).
func (f *Flow) syncAutoConnect(ctx context.Context, on bool) {
	if err := f.setAutoConnectFlag(ctx, on); err != nil {
		f.logf("автоподключение службы (защита включена=%v): %v", on, err)
	}
}

func (f *Flow) setAutoConnectFlag(ctx context.Context, on bool) error {
	disable := true
	if on {
		b, ok, err := f.store.Load()
		disable = err != nil || !ok || !b.AutoConnect
	}
	return f.d.SetDisableAutoConnect(ctx, disable)
}

// SyncAutoConnect выставляет флаг службы по тому, что есть сейчас: служба
// Connected/Connecting — по галочке, иначе «не подключаться». Только для перехода с
// ≤1.0.3, где флаг не ставился: при загрузке служба может быть ещё Idle, поэтому
// каждый запуск так делать нельзя. Ошибку возвращает — вызывающий повторит позже.
func (f *Flow) SyncAutoConnect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setAutoConnectFlag(ctx, f.protectionOn(ctx))
}

// protectionOn — служба включена (Connected или Connecting).
func (f *Flow) protectionOn(ctx context.Context) bool {
	st, err := f.d.Status(ctx)
	if err != nil {
		return false
	}
	s := st.GetStatus()
	return s == "Connected" || s == "Connecting"
}

// SetAutoConnect — галочка «Подключаться автоматически». Выбор хранится в коробке;
// флаг службы меняется сразу, только если защита сейчас включена, — выключенная
// щитом остаётся выключенной и после перезагрузки. Ошибка службы не мешает.
func (f *Flow) SetAutoConnect(ctx context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok, err := f.store.Load()
	if err != nil {
		return &Error{Kind: KindUnknown, Detail: "не прочитать сохранённую коробку", Err: err}
	}
	if !ok {
		return &Error{Kind: KindUnknown, Detail: "коробки нет"}
	}
	if b.AutoConnect != on {
		b.AutoConnect = on
		if err := f.store.Save(b); err != nil {
			return &Error{Kind: KindUnknown, Detail: "не сохранить коробку", Err: err}
		}
	}
	f.syncAutoConnect(ctx, f.protectionOn(ctx))
	return nil
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

// Disconnect опускает туннель, регистрация и коробка остаются. Выключение
// запоминается: служба не включит защиту сама и после перезагрузки.
func (f *Flow) Disconnect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.d.Down(ctx); err != nil {
		return &Error{Kind: f.refine(ctx, classifyLogin(err)), Detail: "отключение", Err: err}
	}
	f.syncAutoConnect(ctx, false)
	return nil
}

// Reconnect поднимает туннель к запомненной коробке без ссылки. Сначала сверка
// профиля службы с коробкой (recover).
//
// NeedsLogin/SessionExpired — management отверг регистрацию: KindNeedsLink
// (нужна ссылка), Up не вызывается. LoginFailed — прошлая проверка входа не
// прошла (сеть): повторяем Login без ключа — служба проверит прежнюю
// регистрацию и вернёт OK, если пир жив (server.go:769-777); если он не
// принят — просит браузер или сообщает, что SSO нет: тогда KindNeedsLink.
func (f *Flow) Reconnect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.recover(ctx); err != nil {
		return err
	}
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
	f.syncAutoConnect(ctx, true)
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
	// До Logout: не пройдёт он (нет сети) — служба после перезагрузки не подключится
	// к забытой коробке.
	f.syncAutoConnect(ctx, false)
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
