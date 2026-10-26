// Package homeprobe меряет «до дома»: время ответа DNS коробки через туннель.
package homeprobe

import (
	"context"
	"crypto/rand"
	"net"
	"net/netip"
	"strings"
	"time"
)

// AddrFor превращает адрес пира коробки (может прийти с маской) в адрес DNS.
func AddrFor(ip string) string {
	ip = strings.TrimSpace(ip)
	if p, err := netip.ParsePrefix(ip); err == nil {
		ip = p.Addr().String()
	}
	return net.JoinHostPort(ip, "53")
}

// query — запрос A jet-net.net с рекурсией; ID случайный.
func query() ([]byte, [2]byte) {
	var id [2]byte
	_, _ = rand.Read(id[:])
	q := []byte{id[0], id[1], 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range []string{"jet-net", "net"} {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0, 0, 1, 0, 1) // корень, тип A, класс IN
	return q, id
}

// Measure отправляет один DNS-запрос на addr ("ip:53") и возвращает время до ответа.
func Measure(ctx context.Context, addr string) (time.Duration, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(3 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	// Отмена контекста без дедлайна прерывает ожидание сразу, а не через 3 секунды.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	q, id := query()
	start := time.Now()
	if _, err := conn.Write(q); err != nil {
		return 0, ctxErrOr(ctx, err)
	}
	buf := make([]byte, 512)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return 0, ctxErrOr(ctx, err)
		}
		if n >= 4 && buf[0] == id[0] && buf[1] == id[1] && buf[2]&0x80 != 0 {
			return time.Since(start), nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
	}
}

// ctxErrOr — ошибка контекста, если ввод-вывод прервала его отмена или дедлайн.
func ctxErrOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
