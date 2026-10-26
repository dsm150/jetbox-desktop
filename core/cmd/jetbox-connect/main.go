// jetbox-connect — «одна кнопка» без интерфейса, для живых прогонов.
//
//	jetbox-connect --link-file <путь>   подключить по ссылке из файла (файл удаляется)
//	jetbox-connect --status             состояние
//	jetbox-connect --choose <id>        выбрать выход
//	jetbox-connect --down | --up        отключить / подключить запомненную коробку
//	jetbox-connect --forget             снять компьютер с учёта и забыть коробку
//
// Флаги: --yes (переключить на другую коробку), --ignore-other-vpn, --addr, --store.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dsm150/jetbox-desktop/core/daemonclient"
	"github.com/dsm150/jetbox-desktop/core/flow"
	"github.com/dsm150/jetbox-desktop/core/homeprobe"
	"github.com/dsm150/jetbox-desktop/core/jetboxlink"
	"github.com/dsm150/jetbox-desktop/core/netcheck"
	"github.com/dsm150/jetbox-desktop/core/othervpn"
	"github.com/dsm150/jetbox-desktop/core/platform"
)

func say(format string, a ...any) {
	fmt.Printf("%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

func defaultStore() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "JetBox", "box.json")
}

var stageText = map[flow.Stage]string{
	flow.StageRegistering:  "Регистрируем компьютер…",
	flow.StageConnecting:   "Подключаем…",
	flow.StageChoosingExit: "Выбираем выход…",
	flow.StageProtected:    "Вы под защитой JetBox",
}

// detailOf достаёт Detail из отказа сценария (пусто, если это не flow.Error).
func detailOf(err error) string {
	var fe *flow.Error
	if errors.As(err, &fe) {
		return fe.Detail
	}
	return ""
}

// report печатает отказ сценария с подсказкой по его виду. Возвращает true, если err != nil.
func report(what string, err error) bool {
	if err == nil {
		say("%s: готово", what)
		return false
	}
	k := flow.KindOf(err)
	say("%s: отказ %s (%v)", what, k, err)
	switch k {
	case flow.KindOtherBox:
		if d := detailOf(err); d != "" {
			say("Компьютер уже подключён к другой коробке: %s. Чтобы переключить, запустите снова с --yes", d)
		} else {
			say("Служба зарегистрирована на другую JetBox. Чтобы переключить, запустите снова с --yes")
		}
	case flow.KindOtherVPN:
		say("Включён другой VPN: %s. Выключите его или запустите снова с --ignore-other-vpn", detailOf(err))
	case flow.KindNeedsLink:
		say("Нужна ссылка подключения: jetbox-connect --link-file <путь>")
	}
	return true
}

func main() {
	addr := flag.String("addr", platform.DaemonAddr, "адрес службы")
	store := flag.String("store", defaultStore(), "файл запомненной коробки")
	linkFile := flag.String("link-file", "", "файл со ссылкой подключения (удаляется после чтения)")
	status := flag.Bool("status", false, "состояние")
	choose := flag.String("choose", "", "выбрать выход")
	down := flag.Bool("down", false, "отключить")
	up := flag.Bool("up", false, "подключить запомненную коробку")
	forget := flag.Bool("forget", false, "снять с учёта и забыть коробку")
	yes := flag.Bool("yes", false, "переключить на другую коробку")
	ignoreVPN := flag.Bool("ignore-other-vpn", false, "подключать, даже если включён другой VPN")
	flag.Parse()

	c, err := daemonclient.Dial(*addr)
	if err != nil {
		say("служба: %v", err)
		os.Exit(2)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	det := othervpn.New(platform.ForeignDaemonAddr, platform.InterfaceName)
	f := flow.New(c, flow.NewStore(*store), flow.Config{
		Iface:    platform.InterfaceName,
		Port:     platform.WireGuardPort,
		OtherVPN: det.Hook(),
		Online:   netcheck.Online,
	})

	failed := false
	switch {
	case *status:
		printStatus(ctx, c, f)
	case *choose != "":
		failed = report("выбор выхода "+*choose, f.ChooseExit(ctx, *choose))
	case *down:
		failed = report("отключение", f.Disconnect(ctx))
	case *up:
		failed = report("подключение", f.Reconnect(ctx))
	case *forget:
		// Forget всегда забывает коробку локально; ошибка — только про снятие на сервере.
		err := f.Forget(ctx)
		if err == nil {
			say("снятие с учёта: готово; коробка забыта")
		} else {
			say("снятие с учёта: отказ %s (%v)", flow.KindOf(err), err)
			if _, has, _ := f.Box(); !has {
				say("коробка забыта локально; запись компьютера на сервере могла остаться")
			} else {
				say("коробку забыть не удалось")
			}
			failed = true
		}
	case *linkFile != "":
		raw, err := os.ReadFile(*linkFile)
		_ = os.Remove(*linkFile)
		if err != nil {
			say("файл ссылки: %v", err)
			os.Exit(2)
		}
		link, out := jetboxlink.Parse(strings.TrimSpace(string(raw)))
		if out != jetboxlink.OK {
			say("Ссылка повреждена. Запросите новую ссылку подключения")
			os.Exit(2)
		}
		say("Ваш JetBox: %s · %s · ключ %s", link.DisplayName(), link.ManagementURL, link.MaskedKey())
		t0 := time.Now()
		res, err := f.Connect(ctx, link, flow.Options{ConfirmSwitch: *yes, IgnoreOtherVPN: *ignoreVPN},
			func(s flow.Stage) { say("%s (%s)", stageText[s], time.Since(t0).Round(time.Millisecond)) })
		if report("подключение", err) {
			os.Exit(1)
		}
		if res.NeedsChoice {
			say("Выберите выход: %v (jetbox-connect --choose <id>)", res.Candidates)
		}
		time.Sleep(3 * time.Second)
		printStatus(ctx, c, f)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if failed {
		cancel()
		c.Close()
		os.Exit(1)
	}
}

func printStatus(ctx context.Context, c *daemonclient.Client, f *flow.Flow) {
	st, err := c.Status(ctx)
	if err != nil {
		say("служба не отвечает: %v", err)
		return
	}
	nets, _ := c.Networks(ctx) // ошибка — сети неизвестны (nil)
	box, _, _ := f.Box()
	s := flow.SnapshotFrom(st, nets, box.NoExit)
	path := "напрямую"
	if s.Relayed {
		path = "ретранслятор"
	}
	since := "—"
	if !s.Since.IsZero() {
		since = s.Since.Local().Format("15:04:05")
	}
	say("служба %s: %s, под защитой=%v, выход %q (поднят=%v), коробка %s, %s, rx %d tx %d, с %s",
		st.GetDaemonVersion(), s.State, s.Connected, s.ExitID, s.ExitUp, s.BoxIP, path, s.RxBytes, s.TxBytes, since)
	if b, ok, _ := f.Box(); ok {
		say("запомнена коробка: %s (%s)", b.Name, b.Identity)
	}
	if s.BoxIP != "" {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		d, err := homeprobe.Measure(pctx, homeprobe.AddrFor(s.BoxIP))
		say("до дома: %s %v", d.Round(time.Millisecond), err)
	}
}
