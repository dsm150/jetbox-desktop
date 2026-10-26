// Package daemonclient — клиент службы JetBox (апстрим NetBird v0.81.4) по её gRPC.
// Контракт — client/proto тега v0.81.4 (служба собирается ровно из него).
package daemonclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
	"github.com/dsm150/jetbox-desktop/core/platform"
)

// Client — тонкая обёртка над DaemonServiceClient с теми вызовами, что нужны «одной кнопке».
type Client struct {
	conn *grpc.ClientConn
	c    proto.DaemonServiceClient
}

// New оборачивает готовое соединение (в тестах — bufconn).
func New(conn *grpc.ClientConn) *Client {
	return &Client{conn: conn, c: proto.NewDaemonServiceClient(conn)}
}

// Dial подключается к службе: npipe://<имя> на Windows, unix:// или tcp:// на остальных ОС.
// Соединение ленивое: недоступность службы проявится первой ошибкой вызова.
func Dial(addr string) (*Client, error) {
	conn, err := dialConn(addr)
	if err != nil {
		return nil, err
	}
	return New(conn), nil
}

// Close закрывает соединение.
func (c *Client) Close() error { return c.conn.Close() }

// Status — полное состояние службы.
func (c *Client) Status(ctx context.Context) (*proto.StatusResponse, error) {
	return c.c.Status(ctx, &proto.StatusRequest{GetFullPeerStatus: true})
}

// Configure пишет в активный профиль адрес management, имя интерфейса и порт WireGuard.
// Служба 0.81.4 берёт интерфейс и порт только отсюда: одноимённые поля LoginRequest она
// молча игнорирует (проверено на Win11 в спайке — оставались wt0/51820, как у официального NetBird).
func (c *Client) Configure(ctx context.Context, managementURL, iface string, port int64) error {
	prof, err := c.c.GetActiveProfile(ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		return err
	}
	req := &proto.SetConfigRequest{
		ProfileName:   prof.GetProfileName(),
		Username:      prof.GetUsername(),
		ManagementUrl: managementURL,
	}
	if iface != "" {
		req.InterfaceName = &iface
	}
	if port != 0 {
		req.WireguardPort = &port
	}
	_, err = c.c.SetConfig(ctx, req)
	return err
}

// Login регистрирует устройство ключом. needsBrowser — сервер просит вход через браузер
// (для ссылки с ключом такого быть не должно). Имя устройства передаётся всегда, в том числе
// при повторном входе без ключа: служба не отправляет системное имя компьютера.
func (c *Client) Login(ctx context.Context, setupKey, managementURL string) (needsBrowser bool, err error) {
	resp, err := c.c.Login(ctx, &proto.LoginRequest{SetupKey: setupKey, ManagementUrl: managementURL, Hostname: platform.DeviceName})
	if err != nil {
		return false, err
	}
	return resp.GetNeedsSSOLogin(), nil
}

// Up поднимает туннель.
func (c *Client) Up(ctx context.Context) error {
	_, err := c.c.Up(ctx, &proto.UpRequest{})
	return err
}

// Down опускает туннель, регистрация остаётся.
func (c *Client) Down(ctx context.Context) error {
	_, err := c.c.Down(ctx, &proto.DownRequest{})
	return err
}

// Logout снимает устройство с учёта на management (deregister).
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.c.Logout(ctx, &proto.LogoutRequest{})
	return err
}

// Networks — сети, которые management раздал этому устройству.
func (c *Client) Networks(ctx context.Context) ([]exitpick.Network, error) {
	resp, err := c.c.ListNetworks(ctx, &proto.ListNetworksRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]exitpick.Network, 0, len(resp.GetRoutes()))
	for _, n := range resp.GetRoutes() {
		out = append(out, exitpick.Network{ID: n.GetID(), Range: n.GetRange(), Selected: n.GetSelected()})
	}
	return out, nil
}

// SelectExit включает выход, не трогая выбор остальных сетей (append).
func (c *Client) SelectExit(ctx context.Context, id string) error {
	_, err := c.c.SelectNetworks(ctx, &proto.SelectNetworksRequest{NetworkIDs: []string{id}, Append: true})
	return err
}

// DeselectExits снимает выходы ids («без выхода — только сети»). Только по списку:
// all=true снял бы и сети клиентов. Явный отказ ядро запоминает и само выход
// обратно не включит (routemanager updateRouteSelectorFromManagement).
func (c *Client) DeselectExits(ctx context.Context, ids []string) error {
	_, err := c.c.DeselectNetworks(ctx, &proto.SelectNetworksRequest{NetworkIDs: ids})
	return err
}

// maxRecvMsgSize — как у апстрима (client/internal/daemonaddr/grpc.go): полный статус
// сети в несколько тысяч пиров не влезает в стандартные для gRPC 4 МБ.
const maxRecvMsgSize = 16 << 20

// dialOptions — общие опции всех путей подключения к службе.
func dialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxRecvMsgSize)),
	}
}
