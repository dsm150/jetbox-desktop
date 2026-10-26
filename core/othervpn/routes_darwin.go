//go:build darwin

package othervpn

import (
	"os/exec"
	"strings"
)

// internetTunnels — интерфейс, через который macOS отправит пакет в интернет, если это туннель.
// Свой интерфейс отсеивает Detector.Check.
func internetTunnels(string) ([]string, error) {
	out, err := exec.Command("/sbin/route", "-n", "get", "1.1.1.1").Output()
	if err != nil {
		return nil, err
	}
	return tunnelFromRouteGet(string(out)), nil
}

func tunnelFromRouteGet(out string) []string {
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || k != "interface" {
			continue
		}
		v = strings.TrimSpace(v)
		for _, prefix := range []string{"utun", "ipsec", "ppp", "tun", "wg"} {
			if strings.HasPrefix(v, prefix) {
				return []string{v}
			}
		}
	}
	return nil
}
