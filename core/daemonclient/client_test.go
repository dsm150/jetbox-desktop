package daemonclient

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/platform"
)

type fakeServer struct {
	proto.UnimplementedDaemonServiceServer
	setConfig           *proto.SetConfigRequest
	login               *proto.LoginRequest
	selected            *proto.SelectNetworksRequest
	deselected          *proto.SelectNetworksRequest
	status              *proto.StatusRequest
	ups, downs, logouts int

	profileErr error  // GetActiveProfile отвечает этой ошибкой
	activeID   string // Id в ответе GetActiveProfile (пусто — как у старой службы)
	noUser     bool   // GetActiveProfile без пользователя (так служба отвечает про default)
	setConfigs []*proto.SetConfigRequest
	setErr     map[string]error // ответ SetConfig по профилю
	profiles   []*proto.Profile // ответ ListProfiles
	listUser   string
	added      *proto.AddProfileRequest
	switched   *proto.SwitchProfileRequest
	removed    *proto.RemoveProfileRequest
	needsSSO   bool // Login просит вход через браузер
	bigPeers   int  // Status отдаёт столько пиров по 1 КБ (проверка лимита сообщения)
}

func (f *fakeServer) Status(_ context.Context, r *proto.StatusRequest) (*proto.StatusResponse, error) {
	f.status = r
	resp := &proto.StatusResponse{Status: "Connected", DaemonVersion: "0.81.4+jetbox.1"}
	if f.bigPeers > 0 {
		pad := strings.Repeat("x", 1024)
		peers := make([]*proto.PeerState, f.bigPeers)
		for i := range peers {
			peers[i] = &proto.PeerState{Fqdn: pad}
		}
		resp.FullStatus = &proto.FullStatus{Peers: peers}
	}
	return resp, nil
}
func (f *fakeServer) GetActiveProfile(context.Context, *proto.GetActiveProfileRequest) (*proto.GetActiveProfileResponse, error) {
	if f.profileErr != nil {
		return nil, f.profileErr
	}
	user := "ivan"
	if f.noUser {
		user = ""
	}
	return &proto.GetActiveProfileResponse{ProfileName: "default", Username: user, Id: f.activeID}, nil
}
func (f *fakeServer) ListProfiles(_ context.Context, r *proto.ListProfilesRequest) (*proto.ListProfilesResponse, error) {
	f.listUser = r.GetUsername()
	return &proto.ListProfilesResponse{Profiles: f.profiles}, nil
}
func (f *fakeServer) AddProfile(_ context.Context, r *proto.AddProfileRequest) (*proto.AddProfileResponse, error) {
	f.added = r
	return &proto.AddProfileResponse{Id: "0123abcd"}, nil
}
func (f *fakeServer) SwitchProfile(_ context.Context, r *proto.SwitchProfileRequest) (*proto.SwitchProfileResponse, error) {
	f.switched = r
	return &proto.SwitchProfileResponse{}, nil
}
func (f *fakeServer) RemoveProfile(_ context.Context, r *proto.RemoveProfileRequest) (*proto.RemoveProfileResponse, error) {
	f.removed = r
	return &proto.RemoveProfileResponse{}, nil
}
func (f *fakeServer) SetConfig(_ context.Context, r *proto.SetConfigRequest) (*proto.SetConfigResponse, error) {
	f.setConfig = r
	f.setConfigs = append(f.setConfigs, r)
	if err := f.setErr[r.GetProfileName()]; err != nil {
		return nil, err
	}
	return &proto.SetConfigResponse{}, nil
}
func (f *fakeServer) Login(_ context.Context, r *proto.LoginRequest) (*proto.LoginResponse, error) {
	f.login = r
	return &proto.LoginResponse{NeedsSSOLogin: f.needsSSO}, nil
}
func (f *fakeServer) Up(context.Context, *proto.UpRequest) (*proto.UpResponse, error) {
	f.ups++
	return &proto.UpResponse{}, nil
}
func (f *fakeServer) Down(context.Context, *proto.DownRequest) (*proto.DownResponse, error) {
	f.downs++
	return &proto.DownResponse{}, nil
}
func (f *fakeServer) Logout(context.Context, *proto.LogoutRequest) (*proto.LogoutResponse, error) {
	f.logouts++
	return &proto.LogoutResponse{}, nil
}
func (f *fakeServer) ListNetworks(context.Context, *proto.ListNetworksRequest) (*proto.ListNetworksResponse, error) {
	return &proto.ListNetworksResponse{Routes: []*proto.Network{
		{ID: "office-lan", Range: "192.168.1.0/24"},
		{ID: "sv-demo-exit", Range: "0.0.0.0/0, ::/0", Selected: true},
	}}, nil
}
func (f *fakeServer) SelectNetworks(_ context.Context, r *proto.SelectNetworksRequest) (*proto.SelectNetworksResponse, error) {
	f.selected = r
	return &proto.SelectNetworksResponse{}, nil
}

func (f *fakeServer) DeselectNetworks(_ context.Context, r *proto.SelectNetworksRequest) (*proto.SelectNetworksResponse, error) {
	f.deselected = r
	return &proto.SelectNetworksResponse{}, nil
}

func newTestClient(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	fs := &fakeServer{}
	proto.RegisterDaemonServiceServer(srv, fs)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	opts := append(dialOptions(),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return New(conn), fs
}

func TestConfigureSendsInterfaceAndPortThroughSetConfig(t *testing.T) {
	c, fs := newTestClient(t)
	if err := c.Configure(context.Background(), "https://netbird.jet-net.net", "JetBox", 51830); err != nil {
		t.Fatal(err)
	}
	r := fs.setConfig
	if r == nil || r.GetProfileName() != "default" || r.GetUsername() != "ivan" ||
		r.GetManagementUrl() != "https://netbird.jet-net.net" || r.GetInterfaceName() != "JetBox" || r.GetWireguardPort() != 51830 {
		t.Fatalf("SetConfig = %+v", r)
	}
	if r.DisableAutoConnect != nil {
		t.Fatalf("Configure активного трогает автоподключение: %+v", r)
	}
	assertSSHFirst(t, fs)
}

// Сначала отдельный SetConfig только с ServerSSHAllowed=false (ssh_gate смотрит
// сохранённый конфиг), потом management — уже без поля SSH.
func assertSSHFirst(t *testing.T, fs *fakeServer) {
	t.Helper()
	if len(fs.setConfigs) != 2 {
		t.Fatalf("SetConfig %d раз, ждали 2", len(fs.setConfigs))
	}
	first, second := fs.setConfigs[0], fs.setConfigs[1]
	if first.ServerSSHAllowed == nil || *first.ServerSSHAllowed || first.GetManagementUrl() != "" || first.InterfaceName != nil ||
		first.GetProfileName() != second.GetProfileName() || first.GetUsername() != second.GetUsername() {
		t.Fatalf("первый SetConfig = %+v", first)
	}
	if second.ServerSSHAllowed != nil {
		t.Fatalf("второй SetConfig снова про SSH: %+v", second)
	}
}

func TestLoginSendsKeyAndManagementOnly(t *testing.T) {
	c, fs := newTestClient(t)
	needsBrowser, err := c.Login(context.Background(), "0EF79C2F-1A34-4B21-9C0D-1BF529332998", "https://netbird.jet-net.net")
	if err != nil || needsBrowser {
		t.Fatal(needsBrowser, err)
	}
	if fs.login.GetSetupKey() != "0EF79C2F-1A34-4B21-9C0D-1BF529332998" || fs.login.GetManagementUrl() != "https://netbird.jet-net.net" {
		t.Fatalf("Login = %+v", fs.login)
	}
	if fs.login.GetHostname() != platform.DeviceName {
		t.Fatalf("Hostname = %q, ожидали %q", fs.login.GetHostname(), platform.DeviceName)
	}
}

func TestLoginWithoutKeySendsDeviceName(t *testing.T) {
	c, fs := newTestClient(t)
	if _, err := c.Login(context.Background(), "", "https://netbird.jet-net.net"); err != nil {
		t.Fatal(err)
	}
	if fs.login.GetSetupKey() != "" || fs.login.GetHostname() != platform.DeviceName {
		t.Fatalf("Login = %+v", fs.login)
	}
}

func TestNetworksAndSelectExit(t *testing.T) {
	c, fs := newTestClient(t)
	nets, err := c.Networks(context.Background())
	if err != nil || len(nets) != 2 || nets[1].ID != "sv-demo-exit" || !nets[1].Selected {
		t.Fatal(nets, err)
	}
	if err := c.SelectExit(context.Background(), "sv-demo-exit"); err != nil {
		t.Fatal(err)
	}
	if got := fs.selected; got == nil || len(got.GetNetworkIDs()) != 1 || got.GetNetworkIDs()[0] != "sv-demo-exit" || !got.GetAppend() {
		t.Fatalf("SelectNetworks = %+v", got)
	}
}

func TestUpDownLogoutStatus(t *testing.T) {
	c, fs := newTestClient(t)
	ctx := context.Background()
	if err := c.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := c.Status(ctx)
	if err != nil || st.GetStatus() != "Connected" {
		t.Fatal(st, err)
	}
	if fs.ups != 1 || fs.downs != 1 || fs.logouts != 1 {
		t.Fatal(fs.ups, fs.downs, fs.logouts)
	}
}

func TestStatusAsksFullPeerStatus(t *testing.T) {
	c, fs := newTestClient(t)
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fs.status == nil || !fs.status.GetGetFullPeerStatus() {
		t.Fatalf("StatusRequest = %+v", fs.status)
	}
}

// Полный статус большой сети не влезает в стандартные 4 МБ gRPC: апстрим поднимает до 16 МБ.
func TestStatusAcceptsLargeFullStatus(t *testing.T) {
	c, fs := newTestClient(t)
	fs.bigPeers = 6 * 1024 // ~6 МБ
	st, err := c.Status(context.Background())
	if err != nil || len(st.GetFullStatus().GetPeers()) != fs.bigPeers {
		t.Fatal(len(st.GetFullStatus().GetPeers()), err)
	}
}

func TestConfigureWithoutInterfaceAndPortLeavesThemUnset(t *testing.T) {
	c, fs := newTestClient(t)
	if err := c.Configure(context.Background(), "https://netbird.jet-net.net", "", 0); err != nil {
		t.Fatal(err)
	}
	r := fs.setConfig
	if r == nil || r.InterfaceName != nil || r.WireguardPort != nil || r.GetManagementUrl() != "https://netbird.jet-net.net" {
		t.Fatalf("SetConfig = %+v", r)
	}
}

func TestConfigureStopsWhenActiveProfileFails(t *testing.T) {
	c, fs := newTestClient(t)
	fs.profileErr = errors.New("нет профиля")
	if err := c.Configure(context.Background(), "https://netbird.jet-net.net", "JetBox", 51830); err == nil {
		t.Fatal("ждали ошибку GetActiveProfile")
	}
	if fs.setConfig != nil {
		t.Fatalf("SetConfig вызван: %+v", fs.setConfig)
	}
}

func TestLoginReportsBrowserNeeded(t *testing.T) {
	c, fs := newTestClient(t)
	fs.needsSSO = true
	needsBrowser, err := c.Login(context.Background(), "0EF79C2F-1A34-4B21-9C0D-1BF529332998", "https://netbird.jet-net.net")
	if err != nil || !needsBrowser {
		t.Fatal(needsBrowser, err)
	}
}

// «Без выхода» — DeselectNetworks ровно со списком выходов: не all и не append.
func TestDeselectExitsSendsOnlyIDs(t *testing.T) {
	c, fs := newTestClient(t)
	if err := c.DeselectExits(context.Background(), []string{"exit-1", "exit-2"}); err != nil {
		t.Fatal(err)
	}
	got := fs.deselected
	if got == nil || got.GetAll() || got.GetAppend() || len(got.GetNetworkIDs()) != 2 ||
		got.GetNetworkIDs()[0] != "exit-1" || got.GetNetworkIDs()[1] != "exit-2" {
		t.Fatalf("DeselectNetworks = %+v", got)
	}
}

// Флаг автоподключения меняется отдельным SetConfig: только DisableAutoConnect и профиль,
// без адреса management, интерфейса и порта — их служба оставит как есть.
func TestSetDisableAutoConnectSendsOnlyTheFlag(t *testing.T) {
	c, fs := newTestClient(t)
	for _, want := range []bool{true, false} {
		if err := c.SetDisableAutoConnect(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		r := fs.setConfig
		if r == nil || r.DisableAutoConnect == nil || *r.DisableAutoConnect != want ||
			r.GetProfileName() != "default" || r.GetUsername() != "ivan" {
			t.Fatalf("SetConfig = %+v", r)
		}
		if r.GetManagementUrl() != "" || r.InterfaceName != nil || r.WireguardPort != nil || r.RosenpassEnabled != nil ||
			r.ServerSSHAllowed != nil || r.NetworkMonitor != nil || r.DisableDns != nil {
			t.Fatalf("SetConfig трогает лишнее: %+v", r)
		}
	}
}

func TestSetDisableAutoConnectProfileError(t *testing.T) {
	c, fs := newTestClient(t)
	fs.profileErr = errors.New("no profile")
	if err := c.SetDisableAutoConnect(context.Background(), true); err == nil || fs.setConfig != nil {
		t.Fatalf("err=%v setConfig=%+v", err, fs.setConfig)
	}
}

func fakeUser(t *testing.T, name string) {
	t.Helper()
	prev := currentUser
	currentUser = func() (string, error) { return name, nil }
	t.Cleanup(func() { currentUser = prev })
}

// Профиль в запросах — по ID, если служба его назвала: одинаковые имена профилей не мешают.
func TestActiveProfilePrefersID(t *testing.T) {
	c, fs := newTestClient(t)
	id, user, err := c.ActiveProfile(context.Background())
	if err != nil || id != "default" || user != "ivan" {
		t.Fatal(id, user, err)
	}
	fs.activeID = "a1b2c3"
	if err := c.SetDisableAutoConnect(context.Background(), true); err != nil || fs.setConfig.GetProfileName() != "a1b2c3" {
		t.Fatalf("SetConfig = %+v %v", fs.setConfig, err)
	}
}

// SSH выключается сначала в активном профиле, затем адресно во всех остальных;
// только этот флаг. Сбой по неактивному — не ошибка активного.
func TestDisableServerSSHAllProfiles(t *testing.T) {
	c, fs := newTestClient(t)
	fs.activeID = "a1"
	fs.profiles = []*proto.Profile{{Id: "default", Name: "default"}, {Id: "a1", Name: "jetbox-1", IsActive: true},
		{Id: "b2", Name: "jetbox-2"}}
	fs.setErr = map[string]error{"b2": errors.New("не принимаю")}
	others, err := c.DisableServerSSH(context.Background())
	if err != nil || others == nil || !strings.Contains(others.Error(), "b2") {
		t.Fatal(others, err)
	}
	var got []string
	for _, r := range fs.setConfigs {
		if r.ServerSSHAllowed == nil || *r.ServerSSHAllowed || r.GetUsername() != "ivan" ||
			r.GetManagementUrl() != "" || r.InterfaceName != nil || r.WireguardPort != nil || r.DisableAutoConnect != nil {
			t.Fatalf("SetConfig = %+v", r)
		}
		got = append(got, r.GetProfileName())
	}
	if strings.Join(got, ",") != "a1,default,b2" || fs.listUser != "ivan" {
		t.Fatalf("профили %v, ListProfiles для %q", got, fs.listUser)
	}

	fs.setConfigs, fs.setErr = nil, map[string]error{"a1": errors.New("служба занята")}
	if _, err := c.DisableServerSSH(context.Background()); err == nil || len(fs.setConfigs) != 1 {
		t.Fatalf("отказ по активному: err=%v вызовов=%d", err, len(fs.setConfigs))
	}
}

// Новый профиль заводится для вызывающего (здесь он же пользователь активного профиля);
// переключение и удаление — с тем же пользователем.
func TestAddSwitchRemoveProfile(t *testing.T) {
	c, fs := newTestClient(t)
	fakeUser(t, "ivan")
	ctx := context.Background()
	id, user, err := c.AddProfile(ctx, "jetbox-1763300000")
	if err != nil || id != "0123abcd" || user != "ivan" {
		t.Fatal(id, user, err)
	}
	if fs.added.GetProfileName() != "jetbox-1763300000" || fs.added.GetUsername() != "ivan" {
		t.Fatalf("AddProfile = %+v", fs.added)
	}
	if err := c.SwitchProfile(ctx, id, user); err != nil {
		t.Fatal(err)
	}
	if fs.switched.GetProfileName() != "0123abcd" || fs.switched.GetUsername() != "ivan" {
		t.Fatalf("SwitchProfile = %+v", fs.switched)
	}
	if err := c.RemoveProfile(ctx, id, user); err != nil {
		t.Fatal(err)
	}
	if fs.removed.GetProfileName() != "0123abcd" || fs.removed.GetUsername() != "ivan" {
		t.Fatalf("RemoveProfile = %+v", fs.removed)
	}
	// Обратно на default: пользователь не нужен, ОС не спрашиваем.
	currentUser = func() (string, error) { return "", errors.New("нельзя") }
	if err := c.SwitchProfile(ctx, "default", ""); err != nil || fs.switched.GetProfileName() != "default" {
		t.Fatalf("SwitchProfile default = %+v %v", fs.switched, err)
	}
}

// Активен default — служба хранит его без пользователя, а AddProfile без него не
// создаст профиль: берём текущего пользователя ОС (как `netbird profile add`).
func TestAddProfileFromDefaultUsesOSUser(t *testing.T) {
	c, fs := newTestClient(t)
	fs.noUser = true
	fakeUser(t, `PC\petr`)
	id, user, err := c.AddProfile(context.Background(), "jetbox-1")
	if err != nil || id != "0123abcd" || user != `PC\petr` || fs.added.GetUsername() != `PC\petr` {
		t.Fatal(id, user, err, fs.added)
	}
}

// Новый профиль настраивается и входит адресно: ProfileName и Username в запросе.
func TestConfigureAndLoginProfile(t *testing.T) {
	c, fs := newTestClient(t)
	ctx := context.Background()
	if err := c.ConfigureProfile(ctx, "0123abcd", "ivan", "https://netbird.jet-net.net", "JetBox", 51830); err != nil {
		t.Fatal(err)
	}
	r := fs.setConfig
	if r.GetProfileName() != "0123abcd" || r.GetUsername() != "ivan" || r.GetManagementUrl() != "https://netbird.jet-net.net" ||
		r.GetInterfaceName() != "JetBox" || r.GetWireguardPort() != 51830 || r.DisableAutoConnect == nil || !*r.DisableAutoConnect {
		t.Fatalf("SetConfig = %+v", r)
	}
	assertSSHFirst(t, fs)
	if _, err := c.LoginProfile(ctx, "0123abcd", "ivan", "0EF79C2F-1A34-4B21-9C0D-1BF529332998", "https://netbird.jet-net.net"); err != nil {
		t.Fatal(err)
	}
	l := fs.login
	if l.GetProfileName() != "0123abcd" || l.GetUsername() != "ivan" || l.GetHostname() != platform.DeviceName ||
		l.GetSetupKey() != "0EF79C2F-1A34-4B21-9C0D-1BF529332998" {
		t.Fatalf("Login = %+v", l)
	}
}

func TestAddProfileWithoutUserFails(t *testing.T) {
	c, fs := newTestClient(t)
	fs.noUser = true
	prev := currentUser
	currentUser = func() (string, error) { return "", errors.New("нет пользователя") }
	t.Cleanup(func() { currentUser = prev })
	if _, _, err := c.AddProfile(context.Background(), "jetbox-1"); err == nil || fs.added != nil {
		t.Fatalf("err=%v added=%+v", err, fs.added)
	}
}

func TestListProfiles(t *testing.T) {
	c, fs := newTestClient(t)
	fs.profiles = []*proto.Profile{{Id: "default", Name: "default"}, {Id: "a1", Name: "jetbox-1", IsActive: true}, {Name: "old"}}
	got, err := c.ListProfiles(context.Background(), "")
	if err != nil || fs.listUser != "ivan" || len(got) != 3 || got[1] != (Profile{ID: "a1", Name: "jetbox-1", Active: true}) || got[2].ID != "old" {
		t.Fatal(got, err, fs.listUser)
	}
	if _, err := c.ListProfiles(context.Background(), "petr"); err != nil || fs.listUser != "petr" {
		t.Fatal(err, fs.listUser)
	}
}

// Новый профиль — на вызывающего: активный профиль другого пользователя ОС не даёт своего
// имени (иначе откат замены удалял бы чужой профиль — служба потребует прав).
func TestAddProfileForCallerNotForeignActive(t *testing.T) {
	c, fs := newTestClient(t) // активный — профиль пользователя ivan
	fakeUser(t, `PC\petr`)
	_, user, err := c.AddProfile(context.Background(), "jetbox-1")
	if err != nil || user != `PC\petr` || fs.added.GetUsername() != `PC\petr` {
		t.Fatal(user, err, fs.added)
	}
	// Свой активный (регистр другой) — написание из активного: тот же каталог профилей.
	fakeUser(t, "IVAN")
	if _, user, err = c.AddProfile(context.Background(), "jetbox-2"); err != nil || user != "ivan" {
		t.Fatal(user, err)
	}
}
