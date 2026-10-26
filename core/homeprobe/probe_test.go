package homeprobe

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func listen(t *testing.T) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc
}

// echoDNS отвечает на любой запрос тем же ID с флагом ответа.
func echoDNS(t *testing.T, delay time.Duration) string {
	t.Helper()
	pc := listen(t)
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			time.Sleep(delay)
			resp := append([]byte(nil), buf[:n]...)
			resp[2] |= 0x80 // QR=1
			pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func TestMeasure(t *testing.T) {
	addr := echoDNS(t, 20*time.Millisecond)
	d, err := Measure(context.Background(), addr)
	if err != nil || d < 20*time.Millisecond || d > 2*time.Second {
		t.Fatal(d, err)
	}
}

func TestMeasureTimeout(t *testing.T) {
	pc := listen(t) // молчит
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Measure(ctx, pc.LocalAddr().String()); err == nil {
		t.Fatal("ждали таймаут")
	}
}

// Отмена контекста без дедлайна не должна ждать внутренних 3 секунд.
func TestMeasureCancelledWhileWaiting(t *testing.T) {
	pc := listen(t) // молчит
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := Measure(ctx, pc.LocalAddr().String())
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("отмена заняла %v", el)
	}
}

// Ответ с чужим ID пропускается, ждём свой: время — до своего, а не до чужого.
func TestMeasureSkipsForeignID(t *testing.T) {
	pc := listen(t)
	go func() {
		buf := make([]byte, 512)
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		foreign := append([]byte(nil), buf[:n]...)
		foreign[0] ^= 0xFF
		foreign[2] |= 0x80
		pc.WriteTo(foreign, addr)
		time.Sleep(150 * time.Millisecond)
		own := append([]byte(nil), buf[:n]...)
		own[2] |= 0x80
		pc.WriteTo(own, addr)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d, err := Measure(ctx, pc.LocalAddr().String())
	if err != nil || d < 150*time.Millisecond {
		t.Fatal(d, err)
	}
}

func TestQueryEncoding(t *testing.T) {
	q, id := query()
	if len(q) < 12 {
		t.Fatalf("короткий запрос: %x", q)
	}
	if q[0] != id[0] || q[1] != id[1] {
		t.Fatalf("ID %x, в заголовке %x", id, q[:2])
	}
	if q[2] != 0x01 || q[3] != 0x00 { // RD=1
		t.Fatalf("флаги %x", q[2:4])
	}
	if !bytes.Equal(q[4:12], []byte{0, 1, 0, 0, 0, 0, 0, 0}) { // QDCOUNT=1
		t.Fatalf("счётчики %x", q[4:12])
	}
	want := append([]byte("\x07jet-net\x03net\x00"), 0, 1, 0, 1) // A, IN
	if !bytes.Equal(q[12:], want) {
		t.Fatalf("вопрос %q", q[12:])
	}
}

func TestAddrFor(t *testing.T) {
	for in, want := range map[string]string{
		"100.64.239.180":    "100.64.239.180:53",
		"100.64.239.180/16": "100.64.239.180:53",
		" 100.64.1.1/16":    "100.64.1.1:53",
		"100.64.1.1/16 \n":  "100.64.1.1:53",
	} {
		if got := AddrFor(in); got != want {
			t.Errorf("AddrFor(%q) = %q", in, got)
		}
	}
}
