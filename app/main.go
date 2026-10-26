// JetBox — приложение для компьютера: окно «одной кнопки» и значок в трее/строке меню.
// Говорит со службой JetBox (апстрим NetBird) через core/; связь с окном — события Wails.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/dsm150/jetbox-desktop/app/controller"
	"github.com/dsm150/jetbox-desktop/app/icons"
	"github.com/dsm150/jetbox-desktop/core/daemonclient"
	"github.com/dsm150/jetbox-desktop/core/flow"
	"github.com/dsm150/jetbox-desktop/core/homeprobe"
	"github.com/dsm150/jetbox-desktop/core/netcheck"
	"github.com/dsm150/jetbox-desktop/core/othervpn"
	"github.com/dsm150/jetbox-desktop/core/platform"
)

// version задаётся при сборке: -ldflags "-X main.version=1.0.0".
var version = "dev"

//go:embed all:frontend/dist
var frontend embed.FS

// settingsDir — %APPDATA%\JetBox или ~/Library/Application Support/JetBox (таблица постановки).
func settingsDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "JetBox")
}

// autostartFlag — запуск при входе в систему. Свой Login Item / SMAppService / Run-ключ
// приложение не регистрирует: автозапуск ставят установщики (.pkg — LaunchAgent, NSIS — HKLM Run).
const autostartFlag = "--autostart"

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// linkFromArgs — ссылка jetbox:// из аргументов (Windows передаёт её в argv).
func linkFromArgs(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a)), "jetbox:") {
			return strings.TrimSpace(a)
		}
	}
	return ""
}

func main() {
	logOut := setupLog(settingsDir())
	dist, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	lang := systemLang()
	args := os.Args[1:]
	argLink := linkFromArgs(args)
	log.Printf("start: version=%s os=%s/%s lang=%s args=%q",
		version, runtime.GOOS, runtime.GOARCH, lang, redactArgs(args))

	client, err := daemonclient.Dial(platform.DaemonAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	det := othervpn.New(platform.ForeignDaemonAddr, platform.InterfaceName)
	engine := flow.New(client, flow.NewStore(filepath.Join(settingsDir(), "box.json")), flow.Config{
		Iface:    platform.InterfaceName,
		Port:     platform.WireGuardPort,
		OtherVPN: det.Hook(),
		Online:   netcheck.Online,
	})
	probe := func(ctx context.Context, ip string) (time.Duration, error) {
		return homeprobe.Measure(ctx, homeprobe.AddrFor(ip))
	}

	// macOS: Options.Icon перебивает значок из бандла (JetBox.icns) — как у апстрима, не задаём.
	appIcon := icons.App
	if runtime.GOOS == "darwin" {
		appIcon = nil
	}

	// Мост создаётся до application.New: второй запуск может прийти, пока main ещё
	// не дошёл до контроллера. До Init мост только запоминает ссылку и просьбу показа.
	b := newBridge(lang)
	app := application.New(application.Options{
		Name:        "JetBox",
		Description: "JetBox",
		Icon:        appIcon,
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: platform.SingleInstanceID,
			// Своя горутина: на Windows обработчик зовётся из оконной процедуры (WM_COPYDATA),
			// а показ окна ждёт главный поток — синхронно была бы взаимная блокировка
			// (так же делает апстрим, client/ui/main.go).
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				go b.onSecondInstance(d.Args)
			},
		},
		// Предупреждения и ошибки Wails — в тот же журнал (в production-сборке иначе в никуда).
		Logger: slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                "main",
		Title:               "JetBox",
		Width:               380,
		Height:              580,
		DisableResize:       true,
		Hidden:              true,
		InitialPosition:     application.WindowCentered,
		MinimiseButtonState: application.ButtonHidden,
		MaximiseButtonState: application.ButtonHidden,
		URL:                 "/",
	})
	// Закрытие окна прячет его: JetBox живёт в трее. Хук отменяет штатный обработчик,
	// который уничтожил бы окно; при app.Quit() хуки не вызываются.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		win.Hide()
	})

	b.attach(app, win)
	ctl := controller.New(engine, client, probe, b.publish, lang, version,
		controller.Options{OtherVPN: det.Hook()})
	b.ctl = ctl
	b.tray = newTray(app, b, lang)

	// macOS отдаёт ссылку только этим событием; Windows — и в argv, и этим же событием
	// (Wails сам смотрит argv при одном аргументе) — ссылку из argv берём один раз, ниже.
	app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(e *application.ApplicationEvent) {
		if raw := e.Context().URL(); raw != "" && raw != argLink {
			log.Print("ApplicationLaunchedWithUrl")
			b.openLink(raw)
		}
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		log.Print("ApplicationStarted")
		go func() {
			b.start(argLink, hasArg(args, autostartFlag))
			b.pollForever()
		}()
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
	log.Print("exit")
}
