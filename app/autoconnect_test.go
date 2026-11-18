package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Переход с ≤1.0.3: флаг службы выставляется по её состоянию один раз; метка — только
// после успешного SetConfig, иначе повтор при следующем запуске.
func TestSyncAutoConnectOnce(t *testing.T) {
	b, eng := newTestBridge(t, "Connected")
	b.dir = t.TempDir()
	eng.syncErr = errors.New("служба не отвечает")
	b.syncAutoConnectOnce(context.Background())
	if _, err := os.Stat(filepath.Join(b.dir, autoConnectMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("метка после отказа службы:", err)
	}
	eng.syncErr = nil
	b.syncAutoConnectOnce(context.Background())
	if _, err := os.Stat(filepath.Join(b.dir, autoConnectMarker)); err != nil {
		t.Fatal("нет метки:", err)
	}
	b.syncAutoConnectOnce(context.Background())
	if n := eng.count("SyncAutoConnect"); n != 2 {
		t.Fatalf("SyncAutoConnect=%d, want 2", n)
	}
}

// SSH-сервер NetBird выключается один раз; метка — только после успешного SetConfig.
func TestDisableSSHOnce(t *testing.T) {
	b, _ := newTestBridge(t, "Connected")
	b.dir = t.TempDir()
	calls := 0
	var fail error = errors.New("служба не отвечает")
	b.sshOff = func(context.Context) error { calls++; return fail }
	b.disableSSHOnce(context.Background())
	if _, err := os.Stat(filepath.Join(b.dir, sshOffMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("метка после отказа службы:", err)
	}
	fail = nil
	b.disableSSHOnce(context.Background())
	if _, err := os.Stat(filepath.Join(b.dir, sshOffMarker)); err != nil {
		t.Fatal("нет метки:", err)
	}
	b.disableSSHOnce(context.Background())
	if calls != 2 {
		t.Fatalf("DisableServerSSH=%d, want 2", calls)
	}
}
