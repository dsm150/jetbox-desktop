package netcheck

import (
	"context"
	"net"
	"testing"
	"time"
)

func withTargets(t *testing.T, list ...string) {
	t.Helper()
	old := targets
	targets = list
	t.Cleanup(func() { targets = old })
}

func listening(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

func closedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestOnlineWhenAnyTargetAnswers(t *testing.T) {
	withTargets(t, closedPort(t), listening(t), closedPort(t))
	if !Online(context.Background()) {
		t.Fatal("ожидали онлайн")
	}
}

func TestOfflineWhenNoTargetAnswers(t *testing.T) {
	withTargets(t, closedPort(t), closedPort(t))
	start := time.Now()
	if Online(context.Background()) {
		t.Fatal("ожидали офлайн")
	}
	if time.Since(start) > 3500*time.Millisecond {
		t.Fatal("дольше 3 с")
	}
}

func TestCancelledContextIsOffline(t *testing.T) {
	withTargets(t, listening(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Online(ctx) {
		t.Fatal("отменённый ctx — не онлайн")
	}
}

func TestDefaultTargetsAreIPs(t *testing.T) {
	if len(targets) != 3 {
		t.Fatal(targets)
	}
	for _, a := range targets {
		host, _, err := net.SplitHostPort(a)
		if err != nil || net.ParseIP(host) == nil {
			t.Fatalf("%q — не IP:порт (DNS не должен участвовать)", a)
		}
	}
}
