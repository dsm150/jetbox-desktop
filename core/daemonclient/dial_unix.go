//go:build !windows

package daemonclient

import (
	"fmt"
	"strings"

	"google.golang.org/grpc"
)

func dialConn(addr string) (*grpc.ClientConn, error) {
	switch {
	case strings.HasPrefix(addr, "unix://"):
		// gRPC понимает unix:///абсолютный/путь сам.
		return grpc.NewClient(addr, dialOptions()...)
	case strings.HasPrefix(addr, "tcp://"):
		return grpc.NewClient("passthrough:///"+strings.TrimPrefix(addr, "tcp://"), dialOptions()...)
	}
	return nil, fmt.Errorf("адрес службы %q: нужен unix:// или tcp://", addr)
}
