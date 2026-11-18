package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/dsm150/jetbox-desktop/app/controller"
)

const (
	eventState = "jetbox:state"
	eventCmd   = "jetbox:cmd"
	privacyURL = "https://jet-net.net/privacy"
	siteURL    = "https://jet-net.net"

	cmdTimeout  = 2 * time.Minute
	pollEvery   = 3 * time.Second
	pollTimeout = 5 * time.Second
)

// command — команда окна (bridge.ts: Events.Emit("jetbox:cmd", {cmd, arg})).
type command struct {
	Cmd string `json:"cmd"`
	Arg string `json:"arg"`
}

// Типы событий: Wails сверяет данные при Emit и сам разбирает JSON команды окна.
func init() {
	application.RegisterEvent[controller.State](eventState)
	application.RegisterEvent[command](eventCmd)
}

// bridge — команды окна → контроллер, состояние → окно и трей.
type bridge struct {
	app  *application.App
	win  *application.WebviewWindow
	ctl  *controller.Controller
	tray *tray
	lang string
	auto autostarter // nil — автозапуск не трогаем (тесты)
	dir  string      // каталог настроек: метка автозапуска по умолчанию
	// sshOff выключает SSH-сервер NetBird в профилях службы; nil — не трогаем (тесты).
	sshOff func(ctx context.Context) error
	// recover доводит прерванную замену подключения (flow.Recover; changed — служба
	// тронута); nil — не трогаем.
	recover func(ctx context.Context) (changed bool, err error)

	// Действия со службой в работе: «Выход» отменяет их и ждёт, пока они (и откат
	// замены подключения) закончатся.
	actMu    sync.Mutex
	acts     map[int]context.CancelFunc
	actSeq   int
	actDone  sync.WaitGroup // Add — только под actMu и пока не quitting
	quitting bool           // «Выход» начался: новые действия получают отменённый контекст

	// Команды идут в контроллер напрямую, каждая в своей горутине: контроллер сам
	// не пускает второе действие, пока идёт первое (Busy), и сам откладывает ссылку.
	// Общая очередь здесь выполнила бы повтор (двойной клик по щиту) следом за первым.

	startMu   sync.Mutex
	started   bool   // Init прошёл: ссылки идут в контроллер сразу
	earlyLink string // ссылка, пришедшая до Init (холодный старт, ранний второй запуск)

	// Показ окна до готовности WebView2 роняет приложение: запасной таймер Wails (3 с)
	// зовёт chromium.Show(), пока контроллер WebView2 ещё nil. Поэтому до первого
	// события готовности просьба показать только запоминается.
	showMu   sync.Mutex
	ready    bool // веб-вью окна готово: Show безопасен
	wantShow bool // показ просили до готовности

	logMu      sync.Mutex
	lastScreen controller.Screen
	lastErr    string
}

// readyEvents — события окна, после которых веб-вью точно создано: страница загрузилась
// (Windows: NavigationCompleted, macOS: didFinishNavigation) или рантайм Wails в ней
// ответил. Каждое приходит только на своей платформе; берётся первое.
var readyEvents = []struct {
	name string
	ev   events.WindowEventType
}{
	{"WindowRuntimeReady", events.Common.WindowRuntimeReady},
	{"WebViewNavigationCompleted", events.Windows.WebViewNavigationCompleted},
	{"WebViewDidFinishNavigation", events.Mac.WebViewDidFinishNavigation},
}

// newBridge создаётся до application.New: обработчик второго запуска может сработать
// сразу. До attach и Init мост только запоминает: ссылку — в earlyLink, показ — в wantShow.
func newBridge(lang string) *bridge {
	return &bridge{lang: lang}
}

// attach — окно и приложение созданы. Вызывается до app.Run; ctl и tray — следом, тоже до Run.
func (b *bridge) attach(app *application.App, win *application.WebviewWindow) {
	b.app, b.win = app, win
	app.Event.On(eventCmd, b.onCmd)
	for _, r := range readyEvents {
		win.OnWindowEvent(r.ev, func(*application.WindowEvent) { b.markReady(r.name) })
	}
}

// onSecondInstance — второй запуск отдаёт ссылку первому; запуск без ссылки (ярлык,
// Finder) — открыть окно; второй запуск автозапуском — ничего. Зовётся в своей горутине.
func (b *bridge) onSecondInstance(args []string) {
	log.Printf("second instance: args=%q", redactArgs(args))
	if link := linkFromArgs(args); link != "" {
		b.openLink(link)
	} else if !hasArg(args, autostartFlag) {
		b.showWindow()
	}
}

// publish — onChange контроллера. Только рассылает: методы контроллера отсюда не вызывать.
func (b *bridge) publish(s controller.State) {
	b.logMu.Lock()
	if s.Screen != b.lastScreen || s.ErrorKind != b.lastErr {
		b.lastScreen, b.lastErr = s.Screen, s.ErrorKind
		log.Print(screenLogLine(s))
	}
	b.logMu.Unlock()
	b.app.Event.Emit(eventState, s)
	if b.tray != nil {
		b.tray.update(s)
	}
}

// screenLogLine — строка журнала о смене экрана. При отказе — с текстом причины: ErrorDetail
// контроллер уже очистил от ключа ссылки (scrub), а одного errorKind="Unknown" для разбора мало.
func screenLogLine(s controller.State) string {
	line := fmt.Sprintf("screen %s errorKind=%q", s.Screen, s.ErrorKind)
	if s.ErrorDetail != "" {
		line += fmt.Sprintf(" detail=%q", s.ErrorDetail)
	}
	return line
}

// showWindow — единственный путь показа окна (холодный старт, ссылка, трей, второй запуск).
func (b *bridge) showWindow() {
	b.showMu.Lock()
	if !b.ready {
		b.wantShow = true
		b.showMu.Unlock()
		log.Print("showWindow requested: webview not ready, deferred")
		return
	}
	b.showMu.Unlock()
	log.Print("showWindow requested")
	b.doShow()
}

// markReady — первое событие готовности веб-вью; отложенный показ выполняется здесь.
func (b *bridge) markReady(event string) {
	b.showMu.Lock()
	if b.ready {
		b.showMu.Unlock()
		return
	}
	b.ready = true
	want := b.wantShow
	b.wantShow = false
	b.showMu.Unlock()
	log.Printf("window ready (%s)", event)
	if want {
		b.doShow()
	}
}

// doShow — Show и Focus Wails сами переходят на главный поток (InvokeSync).
func (b *bridge) doShow() {
	b.win.Show()
	b.win.Focus()
	log.Print("showWindow performed")
}

// start — первый показ после запуска приложения: Init контроллера, затем ссылка
// (из argv или пришедшая событием раньше Init) либо окно — если это не автозапуск
// при входе в систему (тогда только значок). Ручной запуск показывает окно и на
// главном экране: на Windows 11 значок трея прячется в скрытые.
func (b *bridge) start(argLink string, autostart bool) {
	// Автозапуск — до Init: служба может молчать до cmdTimeout, а реестр её не ждёт.
	applyAutostartDefault(b.auto, b.dir)
	b.syncAutostart()
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	b.ctl.Init(ctx)
	cancel()
	b.syncAutoConnectSoon()
	log.Printf("init done: screen=%s autostart=%v", b.ctl.State().Screen, autostart)

	b.startMu.Lock()
	b.started = true
	link := b.earlyLink
	b.earlyLink = ""
	b.startMu.Unlock()
	if link == "" {
		link = argLink
	}

	switch {
	case link != "":
		b.openLink(link)
	case !autostart:
		b.showWindow()
	}
}

// openLink — ссылка jetbox:// (холодный старт, второй экземпляр, событие ОС).
// До Init (и до создания контроллера) ссылка запоминается — последняя побеждает:
// иначе Init перерисовал бы экран «Ваш JetBox».
func (b *bridge) openLink(raw string) {
	if raw == "" {
		return
	}
	b.startMu.Lock()
	if !b.started {
		b.earlyLink = raw
		b.startMu.Unlock()
		log.Printf("link received before init, kept: %s", linkSummary(raw))
		return
	}
	log.Printf("link received: %s", linkSummary(raw))
	b.startMu.Unlock()
	b.showWindow()
	go func() {
		ctx, done := b.action()
		defer done()
		b.ctl.OnLink(ctx, raw)
	}()
}

// exec — команда окна или трея: прямо в контроллер, без общей очереди.
func (b *bridge) exec(cmd, arg string) {
	ctx, done := b.action()
	defer done()
	b.dispatch(ctx, cmd, arg)
}

// action — контекст действия со службой (не дольше cmdTimeout), который отменит
// «Выход»; done — действие закончено. После начала выхода — уже отменённый контекст:
// действие сразу откажется, и выход его не ждёт.
func (b *bridge) action() (context.Context, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	b.actMu.Lock()
	if b.quitting {
		b.actMu.Unlock()
		cancel()
		return ctx, func() {}
	}
	if b.acts == nil {
		b.acts = map[int]context.CancelFunc{}
	}
	b.actSeq++
	id := b.actSeq
	b.acts[id] = cancel
	b.actDone.Add(1)
	b.actMu.Unlock()
	return ctx, func() {
		b.actMu.Lock()
		delete(b.acts, id)
		b.actMu.Unlock()
		cancel()
		b.actDone.Done()
	}
}

// quitWait — сколько «Выход» ждёт отменённые действия: откат замены подключения —
// Down, SwitchProfile, Up (до 20 с) и удаление нового профиля (до 5 с).
const quitWait = 45 * time.Second

// quit — «Выход» из трея: отменить действия со службой и дождаться их конца (откат
// замены успевает вернуть прежний профиль), но не дольше quitWait, затем выйти.
// Флаг quitting ставится под actMu вместе с отменой: после него actDone.Add не
// вызывается, и Wait не гоняется с Add.
func (b *bridge) quit() {
	b.actMu.Lock()
	b.quitting = true
	b.actMu.Unlock()
	b.cancelActions()
	waited := make(chan struct{})
	go func() { b.actDone.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(quitWait):
		log.Print("quit: действие не закончилось за отведённое время")
	}
	if b.app != nil {
		b.app.Quit()
	}
}

func (b *bridge) cancelActions() {
	b.actMu.Lock()
	defer b.actMu.Unlock()
	if len(b.acts) > 0 {
		log.Printf("quit: отменяем действий: %d", len(b.acts))
	}
	for _, cancel := range b.acts {
		cancel()
	}
}

func (b *bridge) onCmd(e *application.CustomEvent) {
	var c command
	switch d := e.Data.(type) {
	case command:
		c = d
	case *command:
		if d != nil {
			c = *d
		}
	case map[string]any: // на случай незарегистрированного события
		c.Cmd, _ = d["cmd"].(string)
		c.Arg, _ = d["arg"].(string)
	}
	switch c.Cmd {
	case "":
		return
	case "chooseExit", "toggle", "setAutostart", "setAutoConnect": // ID выхода и вкл/выкл не секрет
		log.Printf("cmd %s arg=%q", c.Cmd, c.Arg)
	default:
		log.Printf("cmd %s", c.Cmd)
	}
	switch c.Cmd {
	case "hello":
		// Только окну: трей получает состояния от контроллера строго по Seq.
		b.app.Event.Emit(eventState, b.ctl.State())
	case "openPrivacy":
		_ = b.app.Browser.OpenURL(privacyURL)
	case "openSite":
		_ = b.app.Browser.OpenURL(siteURL)
	case "uninstall":
		go b.uninstall()
	default:
		go b.exec(c.Cmd, c.Arg)
	}
}

// uninstall — «Удалить JetBox…»: штатное удаление ОС (Windows — деинсталлятор с UAC,
// macOS — скрипт от администратора, до ~20 с; окно тем временем «Удаляем…»). Отказ в окне
// прав — тихо назад в «Настройки»; ошибка — экран ошибки. Отвязку отдельно не зовём —
// удаление снимает компьютер с учёта само.
func (b *bridge) uninstall() {
	err := b.ctl.Uninstall(platformUninstall)
	switch {
	case errors.Is(err, errUninstallCanceled):
		log.Print("uninstall: canceled")
	case err != nil:
		log.Printf("uninstall: %v", err)
		b.ctl.Fail(err, controller.OpUninstall)
	default:
		log.Print("uninstall: started")
		// macOS: приложение закрывает сам скрипт удаления; Windows: деинсталлятор закрыл
		// бы его сам, чтобы удалить файлы, — выходим сразу.
		if runtime.GOOS == "windows" {
			b.app.Quit()
		}
	}
}

// toggleWanted — исполнять ли щит при намерении intent («on»/«off» — что предлагала
// кнопка, когда по ней кликнули). Повторный клик после того, как служба уже пришла
// в нужное состояние, ничего не делает. Без намерения — как раньше: Toggle решает сам.
func toggleWanted(intent string, s controller.State) bool {
	svcOn := s.Panel.ServiceState == "Connected" || s.Panel.ServiceState == "Connecting"
	switch intent {
	case "on":
		return !svcOn
	case "off":
		return svcOn
	}
	return true
}

// onOff — аргумент галочки: "on" или "off"; иное — не команда.
func onOff(arg string) (on, ok bool) {
	switch arg {
	case "on":
		return true, true
	case "off":
		return false, true
	}
	return false, false
}

func (b *bridge) dispatch(ctx context.Context, cmd, arg string) {
	c := b.ctl
	switch cmd {
	case "connect":
		c.Connect(ctx)
	case "confirmSwitch":
		c.ConfirmSwitch(ctx)
	case "cancelSwitch":
		c.CancelSwitch()
	case "ignoreOtherVpn":
		c.IgnoreOtherVPN(ctx)
	case "recheck":
		c.Recheck(ctx)
	case "chooseExit":
		c.ChooseExit(ctx, arg)
	case "noExit":
		c.NoExit(ctx)
	case "changeExit":
		c.ChangeExit(ctx)
	case "skipExit":
		c.SkipExit()
	case "toggle":
		if toggleWanted(arg, c.State()) {
			c.Toggle(ctx)
		}
	case "forget":
		c.Forget(ctx)
	case "setAutostart":
		if on, ok := onOff(arg); ok {
			b.setAutostart(on)
		}
	case "setAutoConnect":
		if on, ok := onOff(arg); ok {
			c.SetAutoConnect(ctx, on)
		}
	case "dismiss":
		c.Dismiss()
	}
}

// pollForever — панель дома: опрос службы раз в 3 с.
func (b *bridge) pollForever() {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for range t.C {
		if b.ctl.State().Busy {
			continue // идёт действие — оно само обновит панель
		}
		ctx, cancel := context.WithTimeout(context.Background(), pollTimeout)
		b.ctl.Refresh(ctx)
		cancel()
	}
}
