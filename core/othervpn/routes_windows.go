//go:build windows

package othervpn

import (
	"errors"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// internetTunnels — тонкий адаптер: снимает с системы маршруты /1 и лучший интерфейс
// до 1.1.1.1, решает foreignTunnels (decide.go).
func internetTunnels(own string) ([]string, error) {
	routes, routesErr := halfDefaultRoutes()
	best, bestErr := bestInterface()
	if routesErr != nil && bestErr != nil {
		return nil, errors.Join(routesErr, bestErr)
	}
	return foreignTunnels(own, routes, best), nil
}

func halfDefaultRoutes() ([]routeIface, error) {
	rows, err := winipcfg.GetIPForwardTable2(winipcfg.AddressFamily(windows.AF_INET))
	if err != nil {
		return nil, err
	}
	var out []routeIface
	for i := range rows {
		bits := rows[i].DestinationPrefix.Prefix().Bits()
		if bits != 1 {
			continue
		}
		ifRow, err := rows[i].InterfaceLUID.Interface()
		if err != nil {
			continue
		}
		out = append(out, routeIface{Alias: ifRow.Alias(), Bits: bits})
	}
	return out, nil
}

func bestInterface() (*bestIface, error) {
	var idx uint32
	if err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}}, &idx); err != nil {
		return nil, err
	}
	luid, err := winipcfg.LUIDFromIndex(idx)
	if err != nil {
		return nil, err
	}
	row, err := luid.Interface()
	if err != nil {
		return nil, err
	}
	return &bestIface{Alias: row.Alias(), Type: uint32(row.Type)}, nil
}
