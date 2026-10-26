//go:build windows

package daemonclient

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"google.golang.org/grpc"
)

// dialConn: только npipe://<имя> в защищённом пространстве ProtectedPrefix\Administrators.
// tcp:// на Windows не поддерживается сознательно, как и в апстриме: порт на loopback может
// занять любой локальный процесс и получить setup-ключ. SECURITY_IDENTIFICATION обязателен:
// без него служба не прочитает токен звонящего и порвёт канал
// («failed to write client preface: The pipe has been ended»).
func dialConn(addr string) (*grpc.ClientConn, error) {
	path, err := pipePath(addr)
	if err != nil {
		return nil, err
	}
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
		return winio.DialPipeAccessImpLevel(ctx, path, access, winio.PipeImpLevelIdentification)
	}
	return grpc.NewClient("passthrough:///jetbox-daemon", append(dialOptions(), grpc.WithContextDialer(dialer))...)
}
