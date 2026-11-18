// Package daemonclient — клиент службы JetBox (апстрим NetBird v0.81.4) по её gRPC.
// Контракт — client/proto тега v0.81.4 (служба собирается ровно из него).
package daemonclient

import (
	"context"
	"errors"
	"fmt"
	"os/user"

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

// defaultProfile — профиль службы, который есть всегда и не удаляется (profilemanager).
const defaultProfile = "default"

// currentUser — имя пользователя ОС, как его берёт апстрим (cmd/profile.go): к нему
// служба привязывает каталог профилей. В тестах подменяется.
var currentUser = func() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

// handle — как назвать активный профиль в запросе: ID (служба ищет сначала точное
// совпадение ID, и одинаковые имена не мешают), у старой службы без ID — имя.
func handle(p *proto.GetActiveProfileResponse) string {
	if id := p.GetId(); id != "" {
		return id
	}
	return p.GetProfileName()
}

// userFor — пользователь для запроса о профиле id: свой, если известен; иначе текущий
// пользователь ОС. Профилю default пользователь не нужен.
func userFor(id, username string) (string, error) {
	if username != "" || id == defaultProfile {
		return username, nil
	}
	u, err := currentUser()
	if err != nil {
		return "", fmt.Errorf("пользователь ОС: %w", err)
	}
	return u, nil
}

// Configure пишет в активный профиль адрес management, имя интерфейса и порт WireGuard,
// предварительно выключив SSH-сервер NetBird (см. configure).
func (c *Client) Configure(ctx context.Context, managementURL, iface string, port int64) error {
	prof, err := c.c.GetActiveProfile(ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		return err
	}
	return c.configure(ctx, handle(prof), prof.GetUsername(), managementURL, iface, port, nil)
}

// ConfigureProfile настраивает новый профиль id адресно (активным он не становится:
// служба пишет файл найденного профиля — server.go setConfigInputFromRequest), как
// Configure, и ставит ему «не подключаться при старте службы» (DisableAutoConnect):
// до удачного входа служба не должна поднимать его сама; флаг по галочке выставит
// приложение после Up.
func (c *Client) ConfigureProfile(ctx context.Context, id, username, managementURL, iface string, port int64) error {
	noAuto := true
	return c.configure(ctx, id, username, managementURL, iface, port, &noAuto)
}

// configure — двумя SetConfig: сначала только ServerSSHAllowed=false, затем management,
// интерфейс и порт. Проверка прав (server/ssh_gate.go requirePrivilegeForConfigChange)
// смотрит СОХРАНЁННЫЙ конфиг: пока в нём SSH-сервер включён, смену management
// обычному пользователю служба запрещает, даже если тот же запрос SSH выключает.
// Выключение прав не требует.
// Служба 0.81.4 берёт интерфейс и порт только отсюда: одноимённые поля LoginRequest она
// молча игнорирует (проверено на Win11 в спайке — оставались wt0/51820, как у официального NetBird).
func (c *Client) configure(ctx context.Context, id, username, managementURL, iface string, port int64, disableAuto *bool) error {
	if err := c.setSSH(ctx, id, username, false); err != nil {
		return err
	}
	req := &proto.SetConfigRequest{
		ProfileName:        id,
		Username:           username,
		ManagementUrl:      managementURL,
		DisableAutoConnect: disableAuto,
	}
	if iface != "" {
		req.InterfaceName = &iface
	}
	if port != 0 {
		req.WireguardPort = &port
	}
	_, err := c.c.SetConfig(ctx, req)
	return err
}

// Profile — профиль службы из ListProfiles.
type Profile struct {
	ID     string
	Name   string
	Active bool
}

// ListProfiles — профили службы пользователя username (пусто — пользователь
// активного профиля, у default — пользователь ОС), включая default.
func (c *Client) ListProfiles(ctx context.Context, username string) ([]Profile, error) {
	if username == "" {
		prof, err := c.c.GetActiveProfile(ctx, &proto.GetActiveProfileRequest{})
		if err != nil {
			return nil, err
		}
		username = prof.GetUsername()
	}
	username, err := userFor("", username)
	if err != nil {
		return nil, err
	}
	resp, err := c.c.ListProfiles(ctx, &proto.ListProfilesRequest{Username: username})
	if err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(resp.GetProfiles()))
	for _, p := range resp.GetProfiles() {
		id := p.GetId()
		if id == "" {
			id = p.GetName()
		}
		out = append(out, Profile{ID: id, Name: p.GetName(), Active: p.GetIsActive()})
	}
	return out, nil
}

// DisableServerSSH выключает SSH-сервер NetBird (ServerSSHAllowed=false, только этот
// флаг) в активном профиле, затем адресно во всех остальных профилях службы, видимых
// его пользователю (ListProfiles; профили других пользователей ОС служба не покажет).
// err — не вышло с активным; others — сбои по остальным профилям (только для журнала).
func (c *Client) DisableServerSSH(ctx context.Context) (others, err error) {
	prof, err := c.c.GetActiveProfile(ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		return nil, err
	}
	active := handle(prof)
	if err := c.setSSH(ctx, active, prof.GetUsername(), false); err != nil {
		return nil, err
	}
	username, err := userFor("", prof.GetUsername())
	if err != nil {
		return fmt.Errorf("список профилей: %w", err), nil
	}
	list, err := c.c.ListProfiles(ctx, &proto.ListProfilesRequest{Username: username})
	if err != nil {
		return fmt.Errorf("список профилей: %w", err), nil
	}
	var errs []error
	for _, p := range list.GetProfiles() {
		id := p.GetId()
		if id == "" {
			id = p.GetName()
		}
		if p.GetIsActive() || id == active {
			continue
		}
		if err := c.setSSH(ctx, id, username, false); err != nil {
			errs = append(errs, fmt.Errorf("профиль %s: %w", id, err))
		}
	}
	return errors.Join(errs...), nil
}

func (c *Client) setSSH(ctx context.Context, id, username string, allowed bool) error {
	_, err := c.c.SetConfig(ctx, &proto.SetConfigRequest{
		ProfileName:      id,
		Username:         username,
		ServerSSHAllowed: &allowed,
	})
	return err
}

// ActiveProfile — активный профиль службы: ID (у старой службы — имя) и пользователь
// (у default — пусто).
func (c *Client) ActiveProfile(ctx context.Context) (id, username string, err error) {
	prof, err := c.c.GetActiveProfile(ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		return "", "", err
	}
	return handle(prof), prof.GetUsername(), nil
}

// AddProfile заводит профиль службы с отображаемым именем name для пользователя
// активного профиля; у default он пуст (служба так хранит default) — тогда для
// текущего пользователя ОС, как `netbird profile add`: служба без пользователя
// профиль не создаст. Служба создаёт его сразу с новым ключом WireGuard
// (profilemanager.AddProfile → createProvisionedConfig) — это и есть новая личность.
// Возвращает ID нового профиля и пользователя, для которого он заведён.
func (c *Client) AddProfile(ctx context.Context, name string) (id, username string, err error) {
	prof, err := c.c.GetActiveProfile(ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		return "", "", err
	}
	username, err = userFor("", prof.GetUsername())
	if err != nil {
		return "", "", err
	}
	resp, err := c.c.AddProfile(ctx, &proto.AddProfileRequest{ProfileName: name, Username: username})
	if err != nil {
		return "", "", err
	}
	id = resp.GetId()
	if id == "" {
		id = name // служба без ID в ответе находит профиль по имени
	}
	return id, username, nil
}

// LoginProfile — Login ключом в профиль id. Служба сама делает его активным до входа
// (server.go authorizeAndPrepareLogin → switchProfileIfNeeded) и не возвращает
// прежний при отказе — это забота вызывающего. Конфиг прежнего профиля не трогает.
func (c *Client) LoginProfile(ctx context.Context, id, username, setupKey, managementURL string) (needsBrowser bool, err error) {
	username, err = userFor(id, username)
	if err != nil {
		return false, err
	}
	resp, err := c.c.Login(ctx, &proto.LoginRequest{SetupKey: setupKey, ManagementUrl: managementURL,
		Hostname: platform.DeviceName, ProfileName: &id, Username: &username})
	if err != nil {
		return false, err
	}
	return resp.GetNeedsSSOLogin(), nil
}

// SwitchProfile делает активным профиль id пользователя username (пусто — текущего
// пользователя ОС). Служба только меняет активный профиль и перечитывает его конфиг:
// туннель перед этим опускает вызывающий (Down). Login, SetConfig активного профиля
// и Up дальше идут в новый профиль.
func (c *Client) SwitchProfile(ctx context.Context, id, username string) error {
	username, err := userFor(id, username)
	if err != nil {
		return err
	}
	_, err = c.c.SwitchProfile(ctx, &proto.SwitchProfileRequest{ProfileName: &id, Username: &username})
	return err
}

// RemoveProfile удаляет профиль id пользователя username (пусто — текущего
// пользователя ОС). Внимание: служба сначала пробует снять его пир с учёта
// (logoutFromProfile, без прав — молча оставляет), поэтому звать только для профиля,
// чей пир не зарегистрирован. Активный профиль и default служба удалить не даст.
func (c *Client) RemoveProfile(ctx context.Context, id, username string) error {
	username, err := userFor(id, username)
	if err != nil {
		return err
	}
	_, err = c.c.RemoveProfile(ctx, &proto.RemoveProfileRequest{ProfileName: id, Username: username})
	return err
}

// SetDisableAutoConnect пишет в активный профиль только флаг «не подключаться при старте
// службы» (с патчем 0006 он больше ничего не значит). Пустой ManagementUrl и не заданные
// поля SetConfig оставляет как есть (server.go setConfigInputFromRequest, UpdateConfig).
func (c *Client) SetDisableAutoConnect(ctx context.Context, disable bool) error {
	prof, err := c.c.GetActiveProfile(ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		return err
	}
	_, err = c.c.SetConfig(ctx, &proto.SetConfigRequest{
		ProfileName:        handle(prof),
		Username:           prof.GetUsername(),
		DisableAutoConnect: &disable,
	})
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
