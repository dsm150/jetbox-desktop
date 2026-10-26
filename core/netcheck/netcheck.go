// Package netcheck — «есть ли интернет вообще». Нужен, чтобы отличить
// «нет интернета» от «сервер JetBox недоступен»: служба NetBird в обоих
// случаях отдаёт лишь «context deadline exceeded».
package netcheck

import (
	"context"
	"net"
	"time"
)

// maxWait — потолок проверки, даже если у ctx срок дальше.
const maxWait = 3 * time.Second

// targets — адреса для TCP-проверки: только IP, DNS не участвует
// (Cloudflare, Яндекс, Google — хоть один доступен из любой сети, где есть интернет).
var targets = []string{"1.1.1.1:443", "77.88.8.8:443", "8.8.8.8:443"}

// Online — true, если хотя бы один адрес принял TCP-соединение за
// min(срок ctx, 3 с). Адреса набираются параллельно, ответ — по первому успеху.
func Online(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	list := targets
	ok := make(chan bool, len(list))
	for _, addr := range list {
		go func(addr string) {
			var d net.Dialer
			c, err := d.DialContext(ctx, "tcp", addr)
			if err == nil {
				_ = c.Close()
			}
			ok <- err == nil
		}(addr)
	}
	for range list {
		select {
		case good := <-ok:
			if good {
				return true
			}
		case <-ctx.Done():
			return false
		}
	}
	return false
}
