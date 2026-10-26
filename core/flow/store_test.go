package flow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sub", "box.json"))
	if _, ok, err := s.Load(); ok || err != nil {
		t.Fatal("пустое хранилище", ok, err)
	}
	want := Box{Identity: "netbird.jet-net.net|sv-demo-exit", Name: "Дом на Лесной",
		ManagementURL: "https://netbird.jet-net.net", ExitNode: "sv-demo-exit",
		RegisteredAt: time.Date(2026, 10, 26, 14, 37, 0, 0, time.UTC)}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Load()
	if !ok || err != nil || got != want {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load(); ok {
		t.Fatal("после Clear коробка осталась")
	}
	if err := s.Clear(); err != nil {
		t.Fatal("повторный Clear", err)
	}
}

// «Без выхода» запоминается; старый файл без поля читается как «выход по правилам».
func TestStoreNoExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.json")
	s := NewStore(path)
	if err := s.Save(Box{Identity: "netbird.jet-net.net|", NoExit: true}); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.Load(); !b.NoExit || b.ExitNode != "" {
		t.Fatalf("%+v", b)
	}
	if err := os.WriteFile(path, []byte(`{"identity":"x|y","exit_node":"y"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, ok, _ := s.Load(); !ok || b.NoExit || b.ExitNode != "y" {
		t.Fatalf("%+v", b)
	}
}

func TestStoreNeverHoldsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.json")
	if err := NewStore(path).Save(Box{Identity: "a|b", Name: "x"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(strings.ToLower(string(raw)), "key") {
		t.Fatalf("в файле коробки есть поле ключа: %s", raw)
	}
}

func TestStoreCorruptedFileMeansNoBox(t *testing.T) {
	for _, raw := range []string{"", "{\"identity\": \"a|b", "\x00\x00garbage"} {
		path := filepath.Join(t.TempDir(), "box.json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		b, ok, err := NewStore(path).Load()
		if ok || err != nil || b != (Box{}) {
			t.Fatalf("%q: %+v %v %v", raw, b, ok, err)
		}
	}
}

func TestStoreSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "box.json"))
	for i := 0; i < 3; i++ {
		if err := s.Save(Box{Identity: "a|b"}); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "box.json" {
		t.Fatalf("лишние файлы: %v", ents)
	}
}

func TestStoreSaveFailureRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	// путь назначения — каталог: переименование не пройдёт
	target := filepath.Join(dir, "box.json")
	if err := os.MkdirAll(filepath.Join(target, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(target).Save(Box{Identity: "a|b"}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("временный файл остался: %v", ents)
	}
}

func TestRetryRename(t *testing.T) {
	failing := errors.New("access denied")
	for _, tc := range []struct {
		fails, wantCalls int
		wantErr          bool
	}{{0, 1, false}, {3, 4, false}, {5, 6, false}, {100, 6, true}} {
		calls := 0
		err := retryRename(func(string, string) error {
			calls++
			if calls <= tc.fails {
				return failing
			}
			return nil
		}, "a", "b", 5, time.Millisecond)
		if calls != tc.wantCalls || (err != nil) != tc.wantErr {
			t.Fatalf("fails=%d: calls=%d err=%v", tc.fails, calls, err)
		}
		if tc.wantErr && !errors.Is(err, failing) {
			t.Fatalf("ошибка потеряна: %v", err)
		}
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "box.json"))
	want := Box{Identity: "a|b", Name: "x"}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				switch (i + j) % 3 {
				case 0:
					if err := s.Save(want); err != nil {
						t.Error(err)
					}
				case 1:
					if b, ok, err := s.Load(); err != nil || (ok && b != want) {
						t.Error(b, ok, err)
					}
				default:
					if err := s.Clear(); err != nil {
						t.Error(err)
					}
				}
			}
		}(i)
	}
	wg.Wait()
}
