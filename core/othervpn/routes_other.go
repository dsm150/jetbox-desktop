//go:build !windows && !darwin

package othervpn

func internetTunnels(string) ([]string, error) { return nil, nil }
