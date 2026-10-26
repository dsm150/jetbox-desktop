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

	profileErr error // GetActiveProfile отвечает этой ошибкой
	needsSSO   bool  // Login просит вход через браузер
	bigPeers   int   // Status отдаёт столько пиров по 1 КБ (проверка лимита сообщения)
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
	return &proto.GetActiveProfileResponse{ProfileName: "default", Username: "ivan"}, nil
}
func (f *fakeServer) SetConfig(_ context.Context, r *proto.SetConfigRequest) (*proto.SetConfigResponse, error) {
	f.setConfig = r
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
