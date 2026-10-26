package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/dsm150/jetbox-desktop/core/jetboxlink"
)

const (
	logName    = "app.log"
	logMaxSize = 1 << 20 // больше — при запуске уходит в app.log.1 (поверх старого)
)

// setupLog — журнал приложения <settingsDir>/app.log: std log и вывод паники рантайма
// (в том числе из чужих горутин Wails, которые recover не ловит). Ссылки jetbox://
// вырезаются из каждой строки — в них ключ установки. Ошибки не роняют запуск:
// без файла журнал идёт в stderr.
func setupLog(dir string) io.Writer {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	var out io.Writer = os.Stderr
	if err := os.MkdirAll(dir, 0o700); err == nil {
		path := filepath.Join(dir, logName)
		if fi, err := os.Stat(path); err == nil && fi.Size() > logMaxSize {
			_ = os.Rename(path, path+".1") // занят вторым экземпляром на Windows — пишем дальше в тот же
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_ = debug.SetCrashOutput(f, debug.CrashOptions{})
			out = io.MultiWriter(f, os.Stderr) // файл первым: у GUI-процесса Windows stderr может не быть
		}
	}
	w := redactWriter{out}
	log.SetOutput(w)
	return w
}

// redactWriter вырезает ссылки с ключом из всего, что пишется в журнал.
type redactWriter struct{ w io.Writer }

func (r redactWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(r.w, redactLinks(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// linkStarts — начала ссылок с ключом: своя схема и универсальная ссылка Кабинета.
var linkStarts = []string{
	jetboxlink.CustomScheme + ":",
	"https://" + jetboxlink.UniversalLinkHost + jetboxlink.UniversalLinkPath,
}

// redactLinks заменяет каждую ссылку с ключом (в любом регистре) сводкой linkSummary.
// Конец ссылки — пробел, кавычка, угловая скобка или конец строки.
func redactLinks(s string) string {
	var sb strings.Builder
	for {
		i := -1
		for _, start := range linkStarts {
			if j := indexFold(s, start); j >= 0 && (i < 0 || j < i) {
				i = j
			}
		}
		if i < 0 {
			sb.WriteString(s)
			return sb.String()
		}
		end := strings.IndexAny(s[i:], " \t\r\n\"'`<>")
		if end < 0 {
			end = len(s)
		} else {
			end += i
		}
		sb.WriteString(s[:i])
		sb.WriteString("<link " + linkSummary(s[i:end]) + ">")
		s = s[end:]
	}
}

// indexFold — strings.Index без учёта регистра для ASCII-образца.
func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

// linkSummary — что о ссылке можно писать в журнал: исход разбора, маска ключа, длина.
func linkSummary(raw string) string {
	p, outcome := jetboxlink.Parse(raw)
	switch outcome {
	case jetboxlink.OK:
		return fmt.Sprintf("ok key=%s len=%d", p.MaskedKey(), len(raw))
	case jetboxlink.Broken:
		return fmt.Sprintf("broken len=%d", len(raw))
	default:
		return fmt.Sprintf("not-ours len=%d", len(raw))
	}
}

// redactArgs — аргументы запуска для журнала.
func redactArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = redactLinks(a)
	}
	return out
}
