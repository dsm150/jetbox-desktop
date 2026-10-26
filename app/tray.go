package main

import (
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/dsm150/jetbox-desktop/app/controller"
	"github.com/dsm150/jetbox-desktop/app/icons"
)

type trayLook int

const (
	lookOff trayLook = iota
	lookOn
	lookConnecting
	lookAlert // «выход не отвечает», служба не отвечает: знак с точкой предупреждения
)

// blinkEvery — полпериода мигания «подключаем» (1 Гц).
const blinkEvery = 500 * time.Millisecond

// blinks — мигает ли значок этого вида.
func blinks(l trayLook) bool { return l == lookConnecting }

// trayIcon — значок вида l (кадр frame для мигающих) для светлой и тёмной панели.
// Как у официального клиента: под защитой — инверсионный след JB фирменным синим,
// выключено — монохром, подключаем — след приглушён и мигает, предупреждение — точка.
func trayIcon(l trayLook, frame int) (light, dark []byte) {
	switch {
	case l == lookOn:
		return icons.TrayOnLight, icons.TrayOnDark
	case l == lookAlert:
		return icons.TrayAlertLight, icons.TrayAlertDark
	case l == lookConnecting && frame%2 == 0:
		return icons.TrayConnectingLight, icons.TrayConnectingDark
	default:
		return icons.TrayOffLight, icons.TrayOffDark
	}
}

type tray struct {
	b      *bridge
	lang   string
	st     *application.SystemTray
	status *application.MenuItem
	toggle *application.MenuItem

	// mu держит модель и значок/меню вместе: состояние применяется целиком и только
	// если оно новее уже применённого. Брать mu только не на главном потоке UI:
	// SetLabel/SetIcon сами переходят на него.
	mu sync.Mutex
	m  trayModel

	blinkStop chan struct{} // закрыть — остановить мигание; под mu
}

// trayModel — что показывает трей, без Wails.
type trayModel struct {
	seq     uint64 // Seq последнего применённого состояния
	look    trayLook
	text    string
	off     bool // пункт щита предлагает «Выключить»
	enabled bool
	shown   bool // модель приводилась к состоянию хотя бы раз
}

// apply приводит модель к состоянию s. false — состояние не новее уже применённого.
func (m *trayModel) apply(s controller.State, lang string) bool {
	if m.shown && s.Seq <= m.seq {
		return false
	}
	look, text, on := lookOff, tr(lang, "off"), s.Panel.Connected
	// Служба включена (Connecting/Connected), а пир коробки ещё нет — «подключаем»,
	// как щит в окне; меню в этом состоянии предлагает «Выключить» (controller.Toggle).
	svcOn := s.Panel.ServiceState == "Connecting" || s.Panel.ServiceState == "Connected"
	// Выход не отвечает — не «подключаем»: значок с точкой, пункт «Выключить».
	switch {
	case s.Screen != controller.ScreenProgress && s.BoxName != "" && s.Panel.ServiceUp && s.Panel.ExitDown:
		look, text = lookAlert, tr(lang, "exitDown")
	case s.Screen == controller.ScreenProgress, s.BoxName != "" && !on && svcOn:
		look, text = lookConnecting, tr(lang, "connecting")
	case !s.Panel.ServiceUp:
		look, text = lookAlert, tr(lang, "service")
	case s.BoxName == "":
		text = tr(lang, "nobox")
	case on:
		look, text = lookOn, tr(lang, "on")
	}
	m.seq, m.shown = s.Seq, true
	m.look, m.text = look, text
	m.off = on || svcOn
	m.enabled = s.BoxName != "" && s.Panel.ServiceUp && !s.Panel.NeedsLink && !s.Busy
	return true
}

// intent — намерение пункта щита по его текущей надписи.
func (m *trayModel) intent() string {
	if m.off {
		return "off"
	}
	return "on"
}

func newTray(app *application.App, b *bridge, lang string) *tray {
	t := &tray{b: b, lang: lang} // модель: lookOff, ничего не применено
	t.st = app.SystemTray.New()
	t.st.SetTooltip(tr(lang, "tooltip"))

	menu := application.NewMenu()
	t.status = menu.Add(tr(lang, "nobox")).SetEnabled(false)
	menu.AddSeparator()
	t.toggle = menu.Add(tr(lang, "turnOn")).SetEnabled(false)
	t.toggle.OnClick(func(*application.Context) {
		go func() { // mu — не на главном потоке
			t.mu.Lock()
			intent := t.m.intent()
			t.mu.Unlock()
			b.exec("toggle", intent)
		}()
	})
	menu.Add(tr(lang, "open")).OnClick(func(*application.Context) { b.showWindow() })
	menu.AddSeparator()
	menu.Add(tr(lang, "quit")).OnClick(func(*application.Context) { app.Quit() })
	t.st.SetMenu(menu)

	// Windows: левый клик — окно, правый — меню (как у апстрима, tray_click_windows.go).
	// macOS: без обработчика клик открывает меню сам.
	if runtime.GOOS == "windows" {
		t.st.OnClick(func() { go b.showWindow() })
	}
	t.setIcon(lookOff)
	return t
}

// setIcon — значок вида l; мигающий вид крутит кадры, пока вид не сменится. Под mu
// (кроме первого вызова из newTray, когда трей ещё никому не виден).
func (t *tray) setIcon(l trayLook) {
	if t.blinkStop != nil {
		close(t.blinkStop)
		t.blinkStop = nil
	}
	if !t.show(l, 0) {
		go t.retryShow()
	}
	if !blinks(l) {
		return
	}
	stop := make(chan struct{})
	t.blinkStop = stop
	go func() {
		tick := time.NewTicker(blinkEvery)
		defer tick.Stop()
		for frame := 1; ; frame++ {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			t.mu.Lock()
			if t.blinkStop == stop {
				t.show(l, frame)
			}
			t.mu.Unlock()
		}
	}()
}

// show — кадр frame значка вида l; false — значок временный (macOS до появления кнопки
// строки меню), его надо поставить ещё раз.
func (t *tray) show(l trayLook, frame int) bool {
	light, dark := trayIcon(l, frame)
	return setTrayImage(t.st, light, dark)
}

// retryShow — повторять значок текущего вида, пока он не встанет (до ~15 с после запуска).
func (t *tray) retryShow() {
	for i := 0; i < 60; i++ {
		time.Sleep(250 * time.Millisecond)
		t.mu.Lock()
		ok := t.show(t.m.look, 0)
		t.mu.Unlock()
		if ok {
			return
		}
	}
}

// update — по каждому изменению состояния контроллера (по возрастанию Seq; устаревшее
// отбрасывается моделью).
func (t *tray) update(s controller.State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prev := t.m
	if !t.m.apply(s, t.lang) {
		return
	}
	m := t.m
	if m.look != prev.look {
		t.setIcon(m.look)
	}
	if prev.shown && m.text == prev.text && m.off == prev.off && m.enabled == prev.enabled {
		return
	}
	t.status.SetLabel(m.text)
	if m.off {
		t.toggle.SetLabel(tr(t.lang, "turnOff"))
	} else {
		t.toggle.SetLabel(tr(t.lang, "turnOn"))
	}
	t.toggle.SetEnabled(m.enabled)
	t.st.SetTooltip(m.text)
}
